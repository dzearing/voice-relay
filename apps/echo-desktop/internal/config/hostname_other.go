//go:build !darwin

package config

import "os"

// computerName returns the system hostname on non-macOS platforms.
func computerName() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "echo-client"
	}
	return hostname
}
