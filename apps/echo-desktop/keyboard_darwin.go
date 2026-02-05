//go:build darwin

package main

import (
	"os/exec"
)

func paste() error {
	// Use osascript to send Cmd+V
	script := `tell application "System Events" to keystroke "v" using command down`
	return exec.Command("osascript", "-e", script).Run()
}

func openFile(path string) error {
	return exec.Command("open", "-t", path).Run()
}
