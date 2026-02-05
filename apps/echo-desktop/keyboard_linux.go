//go:build linux

package main

import (
	"os/exec"
)

func paste() error {
	// Use xdotool to send Ctrl+V
	return exec.Command("xdotool", "key", "ctrl+v").Run()
}

func openFile(path string) error {
	return exec.Command("xdg-open", path).Start()
}
