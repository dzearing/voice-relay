//go:build darwin

package keyboard

/*
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices
#include <CoreGraphics/CoreGraphics.h>
#include <ApplicationServices/ApplicationServices.h>

// simulatePaste sends Cmd+V via CGEvent (runs in-process, so the
// Accessibility permission granted to VoiceRelay applies directly).
static int simulatePaste() {
	CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
	if (!source) return -1;

	// macOS virtual key code for 'v' is 9
	CGEventRef keyDown = CGEventCreateKeyboardEvent(source, (CGKeyCode)9, true);
	CGEventRef keyUp   = CGEventCreateKeyboardEvent(source, (CGKeyCode)9, false);

	if (!keyDown || !keyUp) {
		if (keyDown) CFRelease(keyDown);
		if (keyUp)   CFRelease(keyUp);
		CFRelease(source);
		return -2;
	}

	CGEventSetFlags(keyDown, kCGEventFlagMaskCommand);
	CGEventSetFlags(keyUp,   kCGEventFlagMaskCommand);

	CGEventPost(kCGHIDEventTap, keyDown);
	CGEventPost(kCGHIDEventTap, keyUp);

	CFRelease(keyDown);
	CFRelease(keyUp);
	CFRelease(source);
	return 0;
}

// isAccessibilityTrusted wraps AXIsProcessTrusted (checks this process directly).
static int isAccessibilityTrusted() {
	return AXIsProcessTrusted() ? 1 : 0;
}
*/
import "C"

import (
	"fmt"
	"os/exec"
)

// Paste sends Cmd+V to the active application via CGEvent.
// Requires Accessibility permission for this process.
func Paste() error {
	rc := C.simulatePaste()
	if rc != 0 {
		return fmt.Errorf("CGEvent paste failed (rc=%d)", rc)
	}
	return nil
}

// HasAccessibility returns true if this process has Accessibility permission on macOS.
func HasAccessibility() bool {
	return C.isAccessibilityTrusted() != 0
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
