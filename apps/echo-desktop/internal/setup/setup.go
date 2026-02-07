package setup

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/ncruces/zenity"

	"github.com/voice-relay/echo-desktop/internal/config"
)

// RunWizard presents a native dialog-based setup wizard for first-run configuration.
func RunWizard(cfg *config.Config) error {
	// Welcome
	_ = zenity.Info("Welcome to Voice Relay!\n\nLet's get you set up.",
		zenity.Title("Voice Relay Setup"),
		zenity.OKLabel("Continue"),
	)

	// Mode selection
	runCoordinator, err := askYesNo(
		"Would you like to run as a coordinator?\n\n"+
			"This machine will handle speech-to-text and serve\n"+
			"the web app to your phone.\n\n"+
			"Choose No if another machine is already running\n"+
			"as the coordinator.",
		"Setup Mode",
	)
	if err != nil {
		return err
	}

	cfg.RunAsCoordinator = runCoordinator

	if runCoordinator {
		// Coordinator mode
		cfg.Port = config.DefaultPort

		ts := DetectTailscale()
		statusMsg := fmt.Sprintf("Coordinator will run on port %d.", cfg.Port)
		if ts.Available {
			statusMsg += "\n\nTailscale detected! Voice Relay will automatically\nset up a secure Funnel URL for your devices."
		} else {
			statusMsg += "\n\nTailscale not detected.\nInstall Tailscale for easy access from other devices:\nhttps://tailscale.com/download"
		}

		_ = zenity.Info(statusMsg,
			zenity.Title("Coordinator Setup"),
			zenity.OKLabel("Continue"),
		)
	} else {
		// Client-only mode — ask for connection code from the coordinator's tray menu
		code, err := zenity.Entry(
			"Enter the connection code from your coordinator:\n\n"+
				"Right-click the Voice Relay icon on the coordinator\n"+
				"machine and look for the code in the menu.\n\n"+
				"You can also enter a full URL if you have one.",
			zenity.Title("Connect to Coordinator"),
			zenity.EntryText(""),
		)
		if err != nil {
			log.Printf("Entry dialog cancelled")
		} else if code != "" {
			wsURL := resolveCoordinatorURL(code)
			if wsURL != "" {
				cfg.CoordinatorURL = wsURL
			} else {
				_ = zenity.Warning(
					fmt.Sprintf("Could not connect with code: %s\n\nYou can edit the config file later.", code),
					zenity.Title("Connection Failed"),
				)
			}
		}
	}

	// Device name
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "echo-client"
	}
	name, err := zenity.Entry(
		"Enter a name for this device:",
		zenity.Title("Device Name"),
		zenity.EntryText(hostname),
	)
	if err != nil {
		log.Printf("Name dialog cancelled, using hostname")
	} else if name != "" {
		cfg.Name = name
	}

	cfg.SetupComplete = true
	cfg.Save()

	_ = zenity.Info("Setup complete!\n\nVoice Relay will now start.",
		zenity.Title("Voice Relay"),
		zenity.OKLabel("Let's Go"),
	)

	return nil
}

// resolveCoordinatorURL takes a user-provided input (connection code, short URL, HTTPS URL, or ws:// URL)
// and resolves it to a WebSocket URL.
func resolveCoordinatorURL(input string) string {
	input = strings.TrimSpace(input)

	// If already a WebSocket URL, use as-is
	if strings.HasPrefix(input, "ws://") || strings.HasPrefix(input, "wss://") {
		return input
	}

	// If it looks like a bare code (no dots, no slashes, no scheme), treat as TinyURL code
	if !strings.Contains(input, ".") && !strings.Contains(input, "/") && !strings.Contains(input, ":") {
		log.Printf("Treating input as TinyURL code: %s", input)
		input = "https://tinyurl.com/" + input
	}

	// Ensure it has a scheme
	if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "https://") {
		input = "https://" + input
	}

	// Try to hit /connect-info on the coordinator
	infoURL := strings.TrimRight(input, "/") + "/connect-info"
	log.Printf("Resolving coordinator URL: %s", infoURL)

	resp, err := http.Get(infoURL)
	if err != nil {
		log.Printf("Failed to reach coordinator: %v", err)
		// Fall back to constructing a WSS URL
		return deriveWSURL(input)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var info struct {
			WebSocketURL string `json:"wsUrl"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&info); err == nil && info.WebSocketURL != "" {
			log.Printf("Resolved WebSocket URL: %s", info.WebSocketURL)
			return info.WebSocketURL
		}
	}

	// Fall back to constructing a WSS URL
	return deriveWSURL(input)
}

// deriveWSURL converts an HTTP(S) URL to a WebSocket URL.
func deriveWSURL(httpURL string) string {
	wsURL := httpURL
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = strings.TrimRight(wsURL, "/")
	if !strings.HasSuffix(wsURL, "/ws") {
		wsURL += "/ws"
	}
	log.Printf("Derived WebSocket URL: %s", wsURL)
	return wsURL
}

func askYesNo(msg, title string) (bool, error) {
	err := zenity.Question(msg,
		zenity.Title(title),
		zenity.OKLabel("Yes"),
		zenity.ExtraButton("No"),
	)
	if err == nil {
		return true, nil
	}
	if err == zenity.ErrExtraButton {
		return false, nil
	}
	// User cancelled (pressed X) - default to "No"
	return false, nil
}
