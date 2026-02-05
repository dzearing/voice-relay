//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procKeyboardEvent = user32.NewProc("keybd_event")
)

const (
	VK_CONTROL = 0x11
	VK_V       = 0x56
	KEYEVENTF_KEYUP = 0x0002
)

func keyDown(vk int) {
	procKeyboardEvent.Call(uintptr(vk), 0, 0, 0)
}

func keyUp(vk int) {
	procKeyboardEvent.Call(uintptr(vk), 0, uintptr(KEYEVENTF_KEYUP), 0)
}

func paste() error {
	// Send Ctrl+V
	keyDown(VK_CONTROL)
	keyDown(VK_V)
	keyUp(VK_V)
	keyUp(VK_CONTROL)
	return nil
}

func openFile(path string) error {
	return exec.Command("notepad", path).Start()
}

// Prevent console window from showing
func init() {
	// This is handled by -H windowsgui ldflags
}

// For syscall
var _ unsafe.Pointer
