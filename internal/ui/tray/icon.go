package tray

import (
	"bytes"
	"encoding/binary"
	"image/png"

	"github.com/gold-forever/companion/internal/ui/coin"
)

const iconSize = 32

func pngIcon() []byte {
	img := coin.Image(iconSize)
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
