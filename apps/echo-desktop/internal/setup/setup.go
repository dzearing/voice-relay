package setup

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

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
			wsURL, err := ResolveCoordinatorURL(code)
			if err != nil {
				_ = zenity.Warning(
					fmt.Sprintf("Could not connect: %v\n\nYou can edit the config file later.", err),
					zenity.Title("Connection Failed"),
				)
			} else if wsURL != "" {
				cfg.CoordinatorURL = wsURL
			}
		}
	}

	// Device name
	hostname := config.DefaultName()
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

// ResolveCoordinatorURL takes a user-provided input (connection code, short URL, HTTPS URL, or ws:// URL)
// and resolves it to a WebSocket URL. Returns the URL and an error message if resolution failed.
func ResolveCoordinatorURL(input string) (string, error) {
	input = strings.TrimSpace(input)

	// If already a WebSocket URL, use as-is
	if strings.HasPrefix(input, "ws://") || strings.HasPrefix(input, "wss://") {
		return input, nil
	}

	// If it looks like a bare code (no dots, no slashes, no scheme), treat as short URL code
	if !strings.Contains(input, ".") && !strings.Contains(input, "/") && !strings.Contains(input, ":") {
		log.Printf("Treating input as short URL code: %s", input)
		// Try is.gd first (current provider), then tinyurl (legacy)
		resolved, err := resolveShortURL("https://is.gd/" + input)
		if err != nil {
			resolved, err = resolveShortURL("https://tinyurl.com/" + input)
		}
		if err != nil {
			return "", fmt.Errorf("could not resolve code '%s': %v", input, err)
		}
		input = resolved
	}

	// Ensure it has a scheme
	if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "https://") {
		input = "https://" + input
	}

	// Try to hit /connect-info on the coordinator
	infoURL := strings.TrimRight(input, "/") + "/connect-info"
	log.Printf("Resolving coordinator URL: %s", infoURL)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(infoURL)
	if err != nil {
		log.Printf("Failed to reach coordinator: %v", err)
		return "", fmt.Errorf("could not reach coordinator at %s", strings.TrimRight(input, "/"))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var info struct {
			WebSocketURL string `json:"wsUrl"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&info); err == nil && info.WebSocketURL != "" {
			log.Printf("Resolved WebSocket URL: %s", info.WebSocketURL)
			return info.WebSocketURL, nil
		}
	}

	// Fall back to constructing a WSS URL from the base URL
	return deriveWSURL(input), nil
}

// resolveShortURL follows redirects on a short URL and returns the final destination URL.
func resolveShortURL(shortURL string) (string, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // don't follow redirects, capture the Location header
		},
	}

	resp, err := client.Get(shortURL)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		if loc != "" {
			log.Printf("Short URL resolved: %s -> %s", shortURL, loc)
			return loc, nil
		}
	}

	// If no redirect, the short URL might directly serve content (broken)
	return "", fmt.Errorf("short URL did not redirect (status %d)", resp.StatusCode)
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
