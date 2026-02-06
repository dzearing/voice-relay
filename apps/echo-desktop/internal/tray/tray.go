package tray

import (
	"fmt"

	"github.com/getlantern/systray"

	"github.com/voice-relay/echo-desktop/internal/config"
	"github.com/voice-relay/echo-desktop/internal/icons"
	"github.com/voice-relay/echo-desktop/internal/keyboard"
	"github.com/voice-relay/echo-desktop/internal/updater"
)

// Callbacks holds function references for tray actions.
type Callbacks struct {
	OnReconnect func()
	OnQuit      func()
}

var (
	mStatus *systray.MenuItem
	connected bool
)

// SetupMenu initializes the systray menu items.
func SetupMenu(cfg *config.Config, cb Callbacks) {
	systray.SetIcon(icons.IconDisconnected())
	systray.SetTitle("")
	systray.SetTooltip("Voice Relay")

	mStatus = systray.AddMenuItem("Disconnected", "Connection status")
	mStatus.Disable()

	systray.AddSeparator()

	mName := systray.AddMenuItem(fmt.Sprintf("Device: %s", cfg.Name), "Device name")
	mName.Disable()

	// Coordinator mode info
	if cfg.RunAsCoordinator {
		systray.AddSeparator()
		mCoord := systray.AddMenuItem(fmt.Sprintf("Coordinator: Running on :%d", cfg.Port), "Coordinator status")
		mCoord.Disable()
	}

	mConnect := systray.AddMenuItem("Reconnect", "Reconnect to coordinator")

	systray.AddSeparator()

	mConfig := systray.AddMenuItem("Open Config...", "Open configuration file")
	mUpdate := systray.AddMenuItem("Check for Updates", "Check for new version")

	systray.AddSeparator()

	mVersion := systray.AddMenuItem(fmt.Sprintf("Voice Relay v%s", updater.CurrentVersion), "Current version")
	mVersion.Disable()

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Quit Voice Relay")

	go func() {
		for {
			select {
			case <-mConnect.ClickedCh:
				if cb.OnReconnect != nil {
					cb.OnReconnect()
				}
			case <-mConfig.ClickedCh:
				cfg.Save()
				keyboard.OpenFile(config.Path())
			case <-mUpdate.ClickedCh:
				go updater.CheckForUpdates()
			case <-mQuit.ClickedCh:
				if cb.OnQuit != nil {
					cb.OnQuit()
				}
				systray.Quit()
			}
		}
	}()
}

// UpdateStatus updates the systray icon and status text.
func UpdateStatus(isConnected bool, status string) {
	connected = isConnected
	if mStatus != nil {
		mStatus.SetTitle(status)
	}

	if isConnected {
		systray.SetIcon(icons.IconConnected())
	} else {
		systray.SetIcon(icons.IconDisconnected())
	}
}
