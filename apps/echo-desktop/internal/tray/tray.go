package tray

import (
	"fmt"
	"time"

	"github.com/getlantern/systray"

	"github.com/voice-relay/echo-desktop/internal/config"
	"github.com/voice-relay/echo-desktop/internal/coordinator"
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

	// Coordinator mode info — show URL after coordinator starts
	var mQR *systray.MenuItem
	var mURL *systray.MenuItem
	if cfg.RunAsCoordinator {
		systray.AddSeparator()
		mCoord := systray.AddMenuItem(fmt.Sprintf("Coordinator: Running on :%d", cfg.Port), "Coordinator status")
		mCoord.Disable()
		mURL = systray.AddMenuItem("URL: detecting...", "Coordinator URL")
		mURL.Disable()
		mQR = systray.AddMenuItem("Show QR Code", "Open QR code to connect your phone")

		// Update display once coordinator detects connection code
		go func() {
			for i := 0; i < 30; i++ {
				time.Sleep(1 * time.Second)
				if code := coordinator.GetConnectionCode(); code != "" {
					mURL.SetTitle(fmt.Sprintf("Code: %s", code))
					return
				}
				if url := coordinator.GetExternalURL(); url != "" {
					mURL.SetTitle(fmt.Sprintf("URL: %s", url))
					return
				}
			}
			mURL.SetTitle("localhost only")
		}()
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
		// Create a nil channel for QR if not in coordinator mode (select on nil channel blocks forever)
		var qrCh <-chan struct{}
		if mQR != nil {
			qrCh = mQR.ClickedCh
		}

		for {
			select {
			case <-qrCh:
				keyboard.OpenURL(fmt.Sprintf("http://localhost:%d/connect", cfg.Port))
			case <-mConnect.ClickedCh:
				if cb.OnReconnect != nil {
					cb.OnReconnect()
				}
			case <-mConfig.ClickedCh:
				cfg.Save()
				keyboard.OpenFile(config.Path())
			case <-mUpdate.ClickedCh:
				go updater.CheckForUpdatesInteractive(func() {
					if cb.OnQuit != nil {
						cb.OnQuit()
					}
					systray.Quit()
				})
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
