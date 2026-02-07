package config

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// CurrentConfigVersion is bumped when config changes require a fresh setup.
const CurrentConfigVersion = 1

// Config holds all application configuration.
type Config struct {
	ConfigVersion  int    `yaml:"config_version"`
	Name           string `yaml:"name"`
	CoordinatorURL string `yaml:"coordinator_url"`
	OutputMode     string `yaml:"output_mode"` // "paste" or "type"
	SetupComplete  bool   `yaml:"setup_complete,omitempty"`

	// Coordinator mode
	RunAsCoordinator bool   `yaml:"run_as_coordinator,omitempty"`
	Port             int    `yaml:"port,omitempty"`           // default 53937
	WhisperModel     string `yaml:"whisper_model,omitempty"`  // default "base"
	LLMModel         string `yaml:"llm_model,omitempty"`      // default "qwen3-0.6b"
	LLMEnabled       bool   `yaml:"llm_enabled,omitempty"`    // default true
}

// DefaultPort is the default coordinator port.
const DefaultPort = 53937

// Load reads the config from disk, or creates a default one.
// If the config version is outdated, the config is deleted so setup runs again.
func Load() *Config {
	cfg := &Config{}
	configPath := Path()

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		cfg.setDefaults()
		cfg.Save()
		return cfg
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Printf("Error reading config: %v", err)
		cfg.setDefaults()
		return cfg
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		log.Printf("Error parsing config: %v", err)
	}

	// Reset outdated config
	if cfg.ConfigVersion < CurrentConfigVersion {
		log.Printf("Config version %d is outdated (current: %d), resetting", cfg.ConfigVersion, CurrentConfigVersion)
		os.Remove(configPath)
		cfg = &Config{}
		cfg.setDefaults()
		cfg.Save()
		return cfg
	}

	cfg.applyDefaults()
	return cfg
}

// Save writes the config to disk.
func (c *Config) Save() {
	configPath := Path()
	dir := filepath.Dir(configPath)
	os.MkdirAll(dir, 0755)

	data, _ := yaml.Marshal(c)
	os.WriteFile(configPath, data, 0644)
}

// Path returns the platform-specific config file path.
func Path() string {
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "VoiceRelay", "config.yaml")
	} else if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		return filepath.Join(appData, "VoiceRelay", "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "voice-relay", "config.yaml")
}

// Dir returns the platform-specific config/data directory.
func Dir() string {
	return filepath.Dir(Path())
}

// IsFirstRun returns true if no config file exists yet.
func IsFirstRun() bool {
	_, err := os.Stat(Path())
	return os.IsNotExist(err)
}

// IsDefaultURL returns true if the coordinator URL is still the default localhost value.
func (c *Config) IsDefaultURL() bool {
	return c.CoordinatorURL == "" || c.CoordinatorURL == "ws://localhost:53937/ws"
}

func (c *Config) setDefaults() {
	c.ConfigVersion = CurrentConfigVersion
	c.Name = DefaultName()
	c.CoordinatorURL = "ws://localhost:53937/ws"
	c.OutputMode = "paste"
	c.Port = DefaultPort
	c.WhisperModel = "base"
	c.LLMModel = "qwen3-4b"
	c.LLMEnabled = true
}

func (c *Config) applyDefaults() {
	if c.Name == "" {
		c.Name = DefaultName()
	}
	if c.CoordinatorURL == "" {
		c.CoordinatorURL = "ws://localhost:53937/ws"
	}
	if c.OutputMode == "" {
		c.OutputMode = "paste"
	}
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.WhisperModel == "" {
		c.WhisperModel = "base"
	}
	if c.LLMModel == "" || c.LLMModel == "qwen3-0.6b" {
		c.LLMModel = "qwen3-4b"
	}
}

// DefaultName returns the best available machine name for the current platform.
func DefaultName() string {
	return computerName()
}
