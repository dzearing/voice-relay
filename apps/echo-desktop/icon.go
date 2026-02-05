package main

// Generated icons for system tray
// Green microphone for connected, gray for disconnected

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

func init() {
	// Generate icons programmatically
	iconConnected = generateIcon(color.RGBA{34, 197, 94, 255})    // Green
	iconDisconnected = generateIcon(color.RGBA{128, 128, 128, 255}) // Gray
}

func generateIcon(c color.RGBA) []byte {
	const size = 22
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// Draw a simple microphone shape
	centerX, centerY := size/2, size/2

	// Mic body (oval)
	for y := 4; y <= 12; y++ {
		for x := 7; x <= 14; x++ {
			dx := float64(x - centerX)
			dy := float64(y - 8)
			if dx*dx/16 + dy*dy/16 <= 1 {
				img.Set(x, y, c)
			}
		}
	}

	// Mic stand
	for y := 13; y <= 16; y++ {
		img.Set(centerX-1, y, c)
		img.Set(centerX, y, c)
		img.Set(centerX+1, y, c)
	}

	// Mic base arc
	for x := 5; x <= 16; x++ {
		dx := float64(x - centerX)
		y := int(14 + dx*dx/20)
		if y >= 14 && y <= 17 {
			img.Set(x, y, c)
		}
	}

	// Base line
	for x := 6; x <= 15; x++ {
		img.Set(x, 18, c)
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}
