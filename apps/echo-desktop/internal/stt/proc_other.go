//go:build !windows

package stt

import "os/exec"

func setSysProcAttr(cmd *exec.Cmd) {
	// No special handling needed on non-Windows platforms
}
