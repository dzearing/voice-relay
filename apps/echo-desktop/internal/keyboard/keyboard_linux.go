//go:build linux

package keyboard

import (
	"os/exec"
)

// Paste sends Ctrl+V to the active window.
func Paste() error {
	return exec.Command("xdotool", "key", "ctrl+v").Run()
}

// OpenFile opens a file in the default application.
func OpenFile(path string) error {
	return exec.Command("xdg-open", path).Start()
}

// OpenURL opens a URL in the default browser.
func OpenURL(url string) error {
	return exec.Command("xdg-open", url).Start()
}

// HasAccessibility always returns true on Linux (no special permission needed).
func HasAccessibility() bool { return true }

// OpenAccessibilitySettings is a no-op on Linux.
func OpenAccessibilitySettings() {}

// CheckAccessibility is a no-op on Linux.
func CheckAccessibility() {}
