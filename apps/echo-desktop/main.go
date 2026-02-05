package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/atotto/clipboard"
	"github.com/getlantern/systray"
	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Name           string `yaml:"name"`
	CoordinatorURL string `yaml:"coordinator_url"`
	OutputMode     string `yaml:"output_mode"` // "paste" or "type"
}

type Message struct {
	Type    string `json:"type"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
}

var (
	config     Config
	conn       *websocket.Conn
	connected  bool
	mStatus    *systray.MenuItem
	mConnect   *systray.MenuItem
	mQuit      *systray.MenuItem
	reconnect  = make(chan bool, 1)
	lastText   string
	lastTextAt time.Time
)

func main() {
	loadConfig()
	systray.Run(onReady, onExit)
}

func getConfigPath() string {
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "VoiceRelayEcho", "config.yaml")
	} else if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		return filepath.Join(appData, "VoiceRelayEcho", "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "voice-relay-echo", "config.yaml")
}

func loadConfig() {
	configPath := getConfigPath()

	// Create default config if not exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		config = Config{
			Name:           getDefaultName(),
			CoordinatorURL: "ws://localhost:53937/ws",
			OutputMode:     "paste",
		}
		saveConfig()
		return
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Printf("Error reading config: %v", err)
		config = Config{
			Name:           getDefaultName(),
			CoordinatorURL: "ws://localhost:53937/ws",
			OutputMode:     "paste",
		}
		return
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		log.Printf("Error parsing config: %v", err)
	}

	if config.Name == "" {
		config.Name = getDefaultName()
	}
	if config.CoordinatorURL == "" {
		config.CoordinatorURL = "ws://localhost:53937/ws"
	}
	if config.OutputMode == "" {
		config.OutputMode = "paste"
	}
}

func saveConfig() {
	configPath := getConfigPath()
	dir := filepath.Dir(configPath)
	os.MkdirAll(dir, 0755)

	data, _ := yaml.Marshal(&config)
	os.WriteFile(configPath, data, 0644)
}

func getDefaultName() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "echo-client"
	}
	return hostname
}

func onReady() {
	// Set icon based on platform
	systray.SetIcon(getIcon())
	systray.SetTitle("") // Empty for Mac menu bar
	systray.SetTooltip("Voice Relay Echo")

	// Menu items
	mStatus = systray.AddMenuItem("Disconnected", "Connection status")
	mStatus.Disable()

	systray.AddSeparator()

	mName := systray.AddMenuItem(fmt.Sprintf("Device: %s", config.Name), "Device name")
	mName.Disable()

	mConnect = systray.AddMenuItem("Reconnect", "Reconnect to coordinator")

	systray.AddSeparator()

	mConfig := systray.AddMenuItem("Open Config...", "Open configuration file")

	systray.AddSeparator()

	mQuit = systray.AddMenuItem("Quit", "Quit Voice Relay Echo")

	// Handle menu clicks
	go func() {
		for {
			select {
			case <-mConnect.ClickedCh:
				reconnect <- true
			case <-mConfig.ClickedCh:
				openConfigFile()
			case <-mQuit.ClickedCh:
				systray.Quit()
			}
		}
	}()

	// Start connection manager
	go connectionManager()

	// Handle OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		systray.Quit()
	}()
}

func onExit() {
	if conn != nil {
		conn.Close()
	}
}

func connectionManager() {
	for {
		connect()

		// Wait for reconnect signal or timeout
		select {
		case <-reconnect:
			log.Println("Manual reconnect requested")
		case <-time.After(5 * time.Second):
			// Auto reconnect after delay
		}
	}
}

func connect() {
	updateStatus(false, "Connecting...")

	var err error
	conn, _, err = websocket.DefaultDialer.Dial(config.CoordinatorURL, nil)
	if err != nil {
		log.Printf("Connection failed: %v", err)
		updateStatus(false, "Connection failed")
		return
	}

	// Register
	msg := Message{Type: "register", Name: config.Name}
	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("Registration failed: %v", err)
		conn.Close()
		updateStatus(false, "Registration failed")
		return
	}

	updateStatus(true, fmt.Sprintf("Connected as %s", config.Name))
	log.Printf("Connected as %s", config.Name)

	// Message loop
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("Read error: %v", err)
			break
		}

		switch msg.Type {
		case "registered":
			log.Printf("Registered as: %s", msg.Name)
		case "text":
			handleText(msg.Content)
		}
	}

	conn.Close()
	updateStatus(false, "Disconnected")
}

func handleText(text string) {
	if text == "" {
		return
	}

	log.Printf("Received: %s", text)

	// Debounce duplicate messages
	if text == lastText && time.Since(lastTextAt) < 2*time.Second {
		log.Println("Ignoring duplicate message")
		return
	}
	lastText = text
	lastTextAt = time.Now()

	// Copy to clipboard
	if err := clipboard.WriteAll(text); err != nil {
		log.Printf("Clipboard error: %v", err)
	}

	// Always use paste mode (type mode removed - requires complex native libs)
	time.Sleep(100 * time.Millisecond) // Small delay for clipboard
	if err := paste(); err != nil {
		log.Printf("Paste error: %v", err)
	}
}

// paste() is defined in keyboard_darwin.go and keyboard_windows.go

func updateStatus(isConnected bool, status string) {
	connected = isConnected
	if mStatus != nil {
		mStatus.SetTitle(status)
	}

	// Update icon based on connection status
	systray.SetIcon(getIcon())
}

func openConfigFile() {
	configPath := getConfigPath()

	// Ensure config exists
	saveConfig()

	if err := openFile(configPath); err != nil {
		log.Printf("Error opening config: %v", err)
	}
}

func getIcon() []byte {
	if connected {
		return iconConnected
	}
	return iconDisconnected
}

// Icons are generated in icon.go
var iconConnected []byte
var iconDisconnected []byte
