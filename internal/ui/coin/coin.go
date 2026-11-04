// Package coin draws the companion's icon, a gold coin with a darker rim, at any size: the tray shows
// it, and the Windows build carries it as the program's icon. It is drawn rather than shipped as a
// file, so there is nothing in the repository that can't be read.
package coin

import (
	"image"
	"image/color"
)

var (
	gold = color.NRGBA{0xC9, 0xA2, 0x27, 0xFF}
	rim  = color.NRGBA{0x8A, 0x6A, 0x12, 0xFF}
)

// Image is the coin size pixels square, on a transparent background. The rim is 3px at 32px and
// scales with the size.
func Image(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	c, r := float64(size)/2-0.5, float64(size)/2-1
	inner := r - 3*float64(size)/32
	for y := range size {
		for x := range size {
			dx, dy := float64(x)-c, float64(y)-c
			switch d := dx*dx + dy*dy; {
			case d <= inner*inner:
				img.Set(x, y, gold)
			case d <= r*r:
				img.Set(x, y, rim)
			}
		}
	}
	return img
}
