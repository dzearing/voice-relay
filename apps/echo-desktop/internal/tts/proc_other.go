//go:build !windows

package tts

import "os/exec"

func setSysProcAttr(cmd *exec.Cmd) {
	// No special handling needed on non-Windows platforms
}
