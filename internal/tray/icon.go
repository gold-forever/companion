package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
)

// The icon is drawn here rather than shipped as a file, so there is nothing in the repository that
// can't be read: a gold coin with a darker rim.
const iconSize = 32

func pngIcon() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, iconSize, iconSize))
	gold := color.NRGBA{0xC9, 0xA2, 0x27, 0xFF}
	rim := color.NRGBA{0x8A, 0x6A, 0x12, 0xFF}
	const c, r = iconSize/2 - 0.5, iconSize/2 - 1
	for y := range iconSize {
		for x := range iconSize {
			dx, dy := float64(x)-c, float64(y)-c
			switch d := dx*dx + dy*dy; {
			case d <= (r-3)*(r-3):
				img.Set(x, y, gold)
			case d <= r*r:
				img.Set(x, y, rim)
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// icoIcon wraps the PNG in an ICO, which Windows' tray needs and which may hold a PNG since Vista.
func icoIcon() []byte {
	p := pngIcon()
	var b []byte
	b = binary.LittleEndian.AppendUint16(b, 0) // reserved
	b = binary.LittleEndian.AppendUint16(b, 1) // an icon
	b = binary.LittleEndian.AppendUint16(b, 1) // one image
	b = append(b, iconSize, iconSize, 0, 0)    // width, height, no palette, reserved
	b = binary.LittleEndian.AppendUint16(b, 1) // colour planes
	b = binary.LittleEndian.AppendUint16(b, 32)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(p)))
	b = binary.LittleEndian.AppendUint32(b, 22) // the image follows the 6-byte header and 16-byte entry
	return append(b, p...)
}
