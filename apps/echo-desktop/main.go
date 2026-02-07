package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/getlantern/systray"
	"github.com/ncruces/zenity"

	"github.com/voice-relay/echo-desktop/internal/client"
	"github.com/voice-relay/echo-desktop/internal/config"
	"github.com/voice-relay/echo-desktop/internal/coordinator"
	"github.com/voice-relay/echo-desktop/internal/keyboard"
	"github.com/voice-relay/echo-desktop/internal/llm"
	"github.com/voice-relay/echo-desktop/internal/setup"
	"github.com/voice-relay/echo-desktop/internal/stt"
	"github.com/voice-relay/echo-desktop/internal/tray"
	"github.com/voice-relay/echo-desktop/internal/updater"
)

var (
	cfg        *config.Config
	echoClient *client.Client
	sttEngine  *stt.Engine
	llmEngine  *llm.Engine
)

func main() {
	// --force: kill any existing VoiceRelay instances before starting
	for _, arg := range os.Args[1:] {
		if arg == "--force" {
			killExisting()
			break
		}
	}

	// Setup file logging
	logPath := filepath.Join(config.Dir(), "voicerelay.log")
	os.MkdirAll(config.Dir(), 0755)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, logFile))
		defer logFile.Close()
	}
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	cfg = config.Load()

	// First-run setup wizard
	if !cfg.SetupComplete {
		log.Println("Running setup wizard...")
		if err := setup.RunWizard(cfg); err != nil {
			log.Printf("Setup wizard error: %v", err)
		}
	}

	systray.Run(onReady, onExit)
}

func ensureAccessibility() {
	if keyboard.HasAccessibility() {
		return
	}

	log.Println("Accessibility permission not granted, prompting user")

	for {
		err := zenity.Question(
			"Voice Relay needs Accessibility permission to paste\n"+
				"text into your apps.\n\n"+
				"Click \"Open Settings\" to go there directly, or navigate to:\n"+
				"System Settings → Privacy & Security → Accessibility\n\n"+
				"Then toggle Voice Relay on in the list.",
			zenity.Title("Accessibility Permission Required"),
			zenity.OKLabel("Open Settings"),
			zenity.ExtraButton("I've Enabled It"),
		)

		if err == nil {
			// User clicked "Open Settings"
			keyboard.OpenAccessibilitySettings()
			continue
		}

		if err == zenity.ErrExtraButton {
			// User clicked "I've Enabled It" — check and break or retry
			time.Sleep(500 * time.Millisecond)
			if keyboard.HasAccessibility() {
				log.Println("Accessibility permission granted")
				return
			}
			// Still not granted — loop back
			_ = zenity.Warning(
				"Accessibility permission is still not enabled.\n\n"+
					"Make sure Voice Relay is toggled on in the list.\n"+
					"You may need to unlock Settings first (click the lock icon).",
				zenity.Title("Permission Not Detected"),
				zenity.OKLabel("Try Again"),
			)
			continue
		}

		// User closed the dialog (pressed X) — continue without permission
		log.Println("User skipped Accessibility permission prompt")
		return
	}
}

func onReady() {
	// Check Accessibility permission (macOS only — needed for paste injection)
	ensureAccessibility()

	// Start coordinator if configured
	if cfg.RunAsCoordinator {
		// Set the URL before starting so the client knows where to connect
		cfg.CoordinatorURL = fmt.Sprintf("ws://localhost:%d/ws", cfg.Port)

		// Auto-start Tailscale Funnel and detect the URL
		ts := setup.DetectTailscale()
		if ts.Available {
			funnelURL, err := setup.EnsureFunnel(cfg.Port)
			if err != nil {
				log.Printf("Tailscale Funnel not available: %v", err)
			} else if funnelURL != "" {
				coordinator.SetExternalURL(funnelURL)

				// Create a short URL for easy sharing
				shortURL := setup.ShortenURL(funnelURL)
				if shortURL != "" {
					coordinator.SetShortURL(shortURL)
				}

				log.Printf("Coordinator accessible at: %s", funnelURL)
			}
		} else {
			log.Printf("Tailscale not available, coordinator only accessible on localhost")
		}

		go initCoordinator()
	}

	// Create echo client
	echoClient = client.New(cfg.Name, cfg.CoordinatorURL, tray.UpdateStatus)

	// Setup systray menu
	tray.SetupMenu(cfg, tray.Callbacks{
		OnReconnect: handleReconnect,
		OnQuit:      func() { echoClient.Close() },
	})

	// Check for updates in background
	go updater.CheckForUpdates()

	// Start echo client connection manager (with small delay if coordinator is starting)
	go func() {
		if cfg.RunAsCoordinator {
			time.Sleep(500 * time.Millisecond)
		}
		echoClient.Run()
	}()

	// Handle OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		systray.Quit()
	}()
}

func handleReconnect() {
	// Coordinator mode: just reconnect to localhost, no dialog needed
	if cfg.RunAsCoordinator {
		echoClient.TriggerReconnect()
		return
	}

	// Client mode: show a connect dialog with retry loop
	lastCode := ""
	errMsg := ""

	for {
		prompt := "Enter the connection code from your coordinator,\nor a full URL."
		if errMsg != "" {
			prompt = errMsg + "\n\n" + prompt
		}

		code, err := zenity.Entry(
			prompt,
			zenity.Title("Connect to Coordinator"),
			zenity.OKLabel("Connect"),
			zenity.EntryText(lastCode),
		)
		if err != nil {
			return // user cancelled
		}
		code = strings.TrimSpace(code)
		if code == "" {
			return
		}
		lastCode = code

		// Show progress while resolving
		dlg, dlgErr := zenity.Progress(
			zenity.Title("Connect to Coordinator"),
			zenity.Pulsate(),
			zenity.NoCancel(),
		)
		if dlgErr == nil {
			dlg.Text("Connecting...")
		}

		wsURL, resolveErr := setup.ResolveCoordinatorURL(code)

		if dlgErr == nil {
			dlg.Close()
		}

		if resolveErr != nil {
			log.Printf("Connection failed: %v", resolveErr)
			errMsg = fmt.Sprintf("Connection failed: %v", resolveErr)
			continue // retry with error shown
		}

		// Success — update config and connect
		cfg.CoordinatorURL = wsURL
		cfg.Save()
		echoClient.CoordinatorURL = wsURL
		echoClient.TriggerReconnect()

		zenity.Info(
			"Connected successfully!\n\nVoice Relay is now connected to the coordinator.",
			zenity.Title("Connect to Coordinator"),
			zenity.OKLabel("OK"),
		)
		return
	}
}

func initCoordinator() {
	dataDir := config.Dir()
	modelsDir := filepath.Join(dataDir, "models")
	binDir := filepath.Join(dataDir, "bin")

	// Initialize STT engine
	modelPath, err := stt.EnsureModel(modelsDir, cfg.WhisperModel)
	if err != nil {
		log.Printf("STT model not available: %v", err)
	} else {
		serverPath, err := stt.EnsureServer(binDir)
		if err != nil {
			log.Printf("whisper-server not available: %v", err)
		} else {
			engine, err := stt.NewEngine(modelPath, serverPath, 8178)
			if err != nil {
				log.Printf("Failed to initialize STT engine: %v", err)
			} else {
				sttEngine = engine
				coordinator.SetSTTFunc(func(audioData []byte, filename string) (string, error) {
					return sttEngine.Transcribe(audioData, filename)
				})
			}
		}
	}

	// Initialize LLM engine
	if cfg.LLMEnabled {
		llmModelPath, err := llm.EnsureModel(modelsDir, cfg.LLMModel)
		if err != nil {
			log.Printf("LLM model not available: %v", err)
		} else {
			llmServerPath, err := llm.EnsureServer(binDir)
			if err != nil {
				log.Printf("llama-server not available: %v", err)
			} else {
				engine, err := llm.NewEngine(llmModelPath, llmServerPath, 8179)
				if err != nil {
					log.Printf("Failed to initialize LLM engine: %v", err)
				} else {
					llmEngine = engine
					coordinator.SetLLMFunc(func(rawText string) (string, error) {
						return llmEngine.CleanupText(rawText)
					})
				}
			}
		}
	}

	// Start coordinator HTTP server (blocks)
	if err := coordinator.Start(cfg.Port); err != nil {
		log.Printf("Coordinator failed to start: %v", err)
	}
}

func onExit() {
	if echoClient != nil {
		echoClient.Close()
	}
	if sttEngine != nil {
		sttEngine.Close()
	}
	if llmEngine != nil {
		llmEngine.Close()
	}
}

// killExisting terminates other running VoiceRelay processes (not ourselves).
func killExisting() {
	myPID := os.Getpid()
	exeName := filepath.Base(os.Args[0])

	switch runtime.GOOS {
	case "windows":
		// WMIC lists PIDs for processes matching our executable name.
		cmd := exec.Command("wmic", "process", "where",
			fmt.Sprintf("Name='%s'", exeName), "get", "ProcessId", "/format:list")
		hideWindow(cmd)
		out, err := cmd.Output()
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "ProcessId=") {
				continue
			}
			pidStr := strings.TrimPrefix(line, "ProcessId=")
			pid, err := strconv.Atoi(strings.TrimSpace(pidStr))
			if err != nil || pid == myPID {
				continue
			}
			log.Printf("Killing existing VoiceRelay process (PID %d)", pid)
			p, err := os.FindProcess(pid)
			if err == nil {
				p.Kill()
			}
		}
	default:
		// pgrep for macOS/Linux
		pgrepCmd := exec.Command("pgrep", "-f", exeName)
		hideWindow(pgrepCmd)
		out, _ := pgrepCmd.Output()
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			pid, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || pid == myPID {
				continue
			}
			log.Printf("Killing existing VoiceRelay process (PID %d)", pid)
			p, err := os.FindProcess(pid)
			if err == nil {
				p.Kill()
			}
		}
	}

	// Brief pause to let killed processes release resources
	time.Sleep(500 * time.Millisecond)
}
