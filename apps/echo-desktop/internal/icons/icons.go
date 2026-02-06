package icons

import (
	_ "embed"
	"runtime"
)

//go:embed icon_connected_16.png
var connectedIcon16 []byte

//go:embed icon_connected_22.png
var connectedIcon22 []byte

//go:embed icon_connected_32.png
var connectedIcon32 []byte

//go:embed icon_disconnected_16.png
var disconnectedIcon16 []byte

//go:embed icon_disconnected_22.png
var disconnectedIcon22 []byte

//go:embed icon_disconnected_32.png
var disconnectedIcon32 []byte

//go:embed icon_connected.ico
var connectedIconICO []byte

//go:embed icon_disconnected.ico
var disconnectedIconICO []byte

// IconConnected returns the connected icon in the appropriate format for the platform.
func IconConnected() []byte {
	if runtime.GOOS == "windows" {
		return connectedIconICO
	}
	return connectedIcon(platformIconSize())
}

// IconDisconnected returns the disconnected icon in the appropriate format for the platform.
func IconDisconnected() []byte {
	if runtime.GOOS == "windows" {
		return disconnectedIconICO
	}
	return disconnectedIcon(platformIconSize())
}

func platformIconSize() int {
	switch runtime.GOOS {
	case "darwin":
		return 22 // macOS menu bar
	default:
		return 22 // Linux panel
	}
}

func connectedIcon(size int) []byte {
	switch size {
	case 16:
		return connectedIcon16
	case 32:
		return connectedIcon32
	default:
		return connectedIcon22
	}
}

func disconnectedIcon(size int) []byte {
	switch size {
	case 16:
		return disconnectedIcon16
	case 32:
		return disconnectedIcon32
	default:
		return disconnectedIcon22
	}
}
