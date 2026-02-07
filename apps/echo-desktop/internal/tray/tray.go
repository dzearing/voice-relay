package tray

import (
	"fmt"
	"strings"
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
	mStatus   *systray.MenuItem
	connected bool
)

// SetupMenu initializes the systray menu items.
func SetupMenu(cfg *config.Config, cb Callbacks) {
	systray.SetTemplateIcon(icons.TemplateIconDisconnected(), icons.IconDisconnected())
	systray.SetTitle("")
	systray.SetTooltip("Voice Relay")

	mStatus = systray.AddMenuItem("Disconnected", "Connection status")
	mStatus.Disable()

	systray.AddSeparator()

	mName := systray.AddMenuItem(fmt.Sprintf("Device: %s", cfg.Name), "Device name")
	mName.Disable()

	// Connection info and QR code
	var mQR *systray.MenuItem
	if cfg.RunAsCoordinator {
		systray.AddSeparator()
		mCoord := systray.AddMenuItem(fmt.Sprintf("Coordinator: Running on :%d", cfg.Port), "Coordinator status")
		mCoord.Disable()
		mURL := systray.AddMenuItem("URL: detecting...", "Coordinator URL")
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
	} else {
		// Client mode — show QR code option to open coordinator's connect page
		systray.AddSeparator()
		mQR = systray.AddMenuItem("Show QR Code", "Open QR code to connect your phone")
	}

	connectLabel := "Reconnect"
	if !cfg.RunAsCoordinator {
		connectLabel = "Connect..."
	}
	mConnect := systray.AddMenuItem(connectLabel, "Connect to coordinator")

	systray.AddSeparator()

	mConfig := systray.AddMenuItem("Open Config...", "Open configuration file")
	mUpdate := systray.AddMenuItem("Check for Updates", "Check for new version")

	systray.AddSeparator()

	mVersion := systray.AddMenuItem(fmt.Sprintf("Voice Relay v%s", updater.CurrentVersion), "Current version")
	mVersion.Disable()

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Quit Voice Relay")

	go func() {
		// Create a nil channel for QR if not set (select on nil channel blocks forever)
		var qrCh <-chan struct{}
		if mQR != nil {
			qrCh = mQR.ClickedCh
		}

		for {
			select {
			case <-qrCh:
				// Derive URL at click time so it reflects any reconnect changes
				var qrURL string
				if cfg.RunAsCoordinator {
					qrURL = fmt.Sprintf("http://localhost:%d/connect", cfg.Port)
				} else {
					qrURL = wsToHTTP(cfg.CoordinatorURL) + "/connect"
				}
				keyboard.OpenURL(qrURL)
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

// wsToHTTP converts a WebSocket URL to an HTTP URL (strips /ws path suffix).
func wsToHTTP(wsURL string) string {
	u := strings.TrimSuffix(wsURL, "/ws")
	u = strings.Replace(u, "wss://", "https://", 1)
	u = strings.Replace(u, "ws://", "http://", 1)
	return u
}

// UpdateStatus updates the systray icon and status text.
func UpdateStatus(isConnected bool, status string) {
	connected = isConnected
	if mStatus != nil {
		mStatus.SetTitle(status)
	}

	if isConnected {
		systray.SetTemplateIcon(icons.TemplateIconConnected(), icons.IconConnected())
	} else {
		systray.SetTemplateIcon(icons.TemplateIconDisconnected(), icons.IconDisconnected())
	}
}
