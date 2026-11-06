// Package appicon draws the Print Agent app icon, for the agent's status page
// and the cloud dashboard.
package appicon

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// PNG draws the app icon: a white printer on teal (safe as a maskable icon).
func PNG(size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	teal := color.RGBA{0x0f, 0x76, 0x6e, 0xff}
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	fill := func(x0, y0, x1, y1 float64, c color.RGBA) {
		for y := int(y0 * float64(size)); y < int(y1*float64(size)); y++ {
			for x := int(x0 * float64(size)); x < int(x1*float64(size)); x++ {
				img.SetRGBA(x, y, c)
			}
		}
	}
	fill(0, 0, 1, 1, teal)
	fill(0.34, 0.24, 0.66, 0.36, white) // paper going in
	fill(0.24, 0.38, 0.76, 0.62, white) // printer body
	fill(0.32, 0.54, 0.68, 0.57, teal)  // paper slot
	fill(0.34, 0.57, 0.66, 0.76, white) // printed page
	fill(0.39, 0.62, 0.61, 0.64, teal)  // text lines
	fill(0.39, 0.67, 0.55, 0.69, teal)
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
