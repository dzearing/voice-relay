package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/getlantern/systray"

	"github.com/voice-relay/echo-desktop/internal/client"
	"github.com/voice-relay/echo-desktop/internal/config"
	"github.com/voice-relay/echo-desktop/internal/coordinator"
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

func onReady() {
	// Start coordinator if configured
	if cfg.RunAsCoordinator {
		// Set the URL before starting so the client knows where to connect
		cfg.CoordinatorURL = fmt.Sprintf("ws://localhost:%d/ws", cfg.Port)
		go initCoordinator()
	}

	// Create echo client
	echoClient = client.New(cfg.Name, cfg.CoordinatorURL, tray.UpdateStatus)

	// Setup systray menu
	tray.SetupMenu(cfg, tray.Callbacks{
		OnReconnect: func() { echoClient.TriggerReconnect() },
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
