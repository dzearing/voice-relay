//go:build !windows

package setup

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
