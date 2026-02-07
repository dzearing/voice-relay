//go:build windows

package keyboard

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
	vkControl       = 0x11
	vkV             = 0x56
	keyEventFKeyUp  = 0x0002
)

func keyDown(vk int) {
	procKeyboardEvent.Call(uintptr(vk), 0, 0, 0)
}

func keyUp(vk int) {
	procKeyboardEvent.Call(uintptr(vk), 0, uintptr(keyEventFKeyUp), 0)
}

// Paste sends Ctrl+V to the active window.
func Paste() error {
	keyDown(vkControl)
	keyDown(vkV)
	keyUp(vkV)
	keyUp(vkControl)
	return nil
}

// OpenFile opens a file in the default text editor.
func OpenFile(path string) error {
	cmd := exec.Command("notepad", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

// OpenURL opens a URL in the default browser.
func OpenURL(url string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

// For syscall
var _ unsafe.Pointer
