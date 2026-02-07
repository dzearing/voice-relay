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

//go:embed icon_connected_template_16.png
var connectedTemplate16 []byte

//go:embed icon_connected_template_22.png
var connectedTemplate22 []byte

//go:embed icon_connected_template_32.png
var connectedTemplate32 []byte

//go:embed icon_disconnected_template_16.png
var disconnectedTemplate16 []byte

//go:embed icon_disconnected_template_22.png
var disconnectedTemplate22 []byte

//go:embed icon_disconnected_template_32.png
var disconnectedTemplate32 []byte

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

// TemplateIconConnected returns the connected template icon (for macOS dark/light mode).
func TemplateIconConnected() []byte {
	return connectedTemplateIcon(platformIconSize())
}

// TemplateIconDisconnected returns the disconnected template icon (for macOS dark/light mode).
func TemplateIconDisconnected() []byte {
	return disconnectedTemplateIcon(platformIconSize())
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

func connectedTemplateIcon(size int) []byte {
	switch size {
	case 16:
		return connectedTemplate16
	case 32:
		return connectedTemplate32
	default:
		return connectedTemplate22
	}
}

func disconnectedTemplateIcon(size int) []byte {
	switch size {
	case 16:
		return disconnectedTemplate16
	case 32:
		return disconnectedTemplate32
	default:
		return disconnectedTemplate22
	}
}
