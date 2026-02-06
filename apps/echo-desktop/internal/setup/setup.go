package setup

import (
	"fmt"
	"log"
	"os"

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

	ts := DetectTailscale()

	if runCoordinator {
		// Coordinator mode
		cfg.Port = config.DefaultPort

		statusMsg := fmt.Sprintf("Coordinator will run on port %d.", cfg.Port)
		if ts.Available {
			url := fmt.Sprintf("http://%s:%d", ts.IP, cfg.Port)
			if ts.DNSName != "" {
				url = fmt.Sprintf("http://%s:%d", ts.DNSName, cfg.Port)
			}
			statusMsg += fmt.Sprintf("\n\nTailscale detected!\nYour coordinator URL is:\n%s\n\nShare this with your other devices.", url)
		} else {
			statusMsg += "\n\nTailscale not detected.\nInstall Tailscale for easy access from other devices:\nhttps://tailscale.com/download"
		}

		_ = zenity.Info(statusMsg,
			zenity.Title("Coordinator Setup"),
			zenity.OKLabel("Continue"),
		)
	} else {
		// Client-only mode — ask for coordinator URL
		prefill := "ws://localhost:53937/ws"
		if ts.Available && ts.DNSName != "" {
			prefill = fmt.Sprintf("ws://%s:53937/ws", ts.DNSName)
		} else if ts.Available && ts.IP != "" {
			prefill = fmt.Sprintf("ws://%s:53937/ws", ts.IP)
		}

		url, err := zenity.Entry(
			"Enter your coordinator's WebSocket address:",
			zenity.Title("Coordinator URL"),
			zenity.EntryText(prefill),
		)
		if err != nil {
			log.Printf("Entry dialog cancelled, using default URL")
		} else if url != "" {
			cfg.CoordinatorURL = url
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
