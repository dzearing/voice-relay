package config

import (
	"os"
	"os/exec"
	"strings"
)

// computerName returns the user-friendly macOS Computer Name via scutil,
// falling back to os.Hostname() if unavailable.
func computerName() string {
	out, err := exec.Command("scutil", "--get", "ComputerName").Output()
	if err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			return name
		}
	}
	hostname, err := os.Hostname()
	if err != nil {
		return "echo-client"
	}
	return hostname
}
