//go:build darwin

package keyboard

import (
	"os/exec"
)

// Paste sends Cmd+V to the active application.
func Paste() error {
	script := `tell application "System Events" to keystroke "v" using command down`
	return exec.Command("osascript", "-e", script).Run()
}

// OpenFile opens a file in the default text editor.
func OpenFile(path string) error {
	return exec.Command("open", "-t", path).Run()
}
