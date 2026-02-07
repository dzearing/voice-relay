//go:build !windows

package main

import "os/exec"

func hideWindow(cmd *exec.Cmd) {
	// No special handling needed on non-Windows platforms.
}
