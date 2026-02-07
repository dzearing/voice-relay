//go:build darwin

package keyboard

import (
	"fmt"
	"os/exec"
	"strings"
)

// Paste sends Cmd+V to the active application.
// Uses AppleScript via System Events, which requires Accessibility permission.
// If permission is missing, macOS will prompt the user to grant it.
func Paste() error {
	script := `tell application "System Events" to keystroke "v" using command down`
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("paste failed: %w (output: %s)", err, string(out))
	}
	return nil
}

// HasAccessibility returns true if this process has Accessibility permission on macOS.
func HasAccessibility() bool {
	script := `use framework "ApplicationServices"
return (current application's AXIsProcessTrusted()) as boolean`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// OpenAccessibilitySettings opens System Settings to the Accessibility pane.
func OpenAccessibilitySettings() {
	// AppleScript is the most reliable way to open the right pane across macOS versions
	script := `tell application "System Settings"
		activate
		delay 0.5
		reveal anchor "Privacy_Accessibility" of pane id "com.apple.settings.PrivacySecurity"
	end tell`
	err := exec.Command("osascript", "-e", script).Run()
	if err != nil {
		// Fallback: try the URL scheme (works on older macOS)
		exec.Command("open", "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility").Start()
	}
}

// CheckAccessibility is a no-op on macOS — use HasAccessibility + prompt flow instead.
func CheckAccessibility() {}

// OpenFile opens a file in the default text editor.
func OpenFile(path string) error {
	return exec.Command("open", "-t", path).Run()
}

// OpenURL opens a URL in the default browser.
func OpenURL(url string) error {
	return exec.Command("open", url).Start()
}
