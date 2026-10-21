package tray

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"
)

func TestTheIconIsAPNGAndTheWindowsIconWrapsIt(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(pngIcon()))
	if err != nil || img.Bounds().Dx() != iconSize {
		t.Fatalf("not a %dpx PNG: %v", iconSize, err)
	}
	ico := icoIcon()
	if binary.LittleEndian.Uint16(ico[2:]) != 1 || binary.LittleEndian.Uint16(ico[4:]) != 1 {
		t.Fatal("an ICO header with one image")
	}
	size, offset := binary.LittleEndian.Uint32(ico[14:]), binary.LittleEndian.Uint32(ico[18:])
	if int(offset+size) != len(ico) || !bytes.Equal(ico[offset:], pngIcon()) {
		t.Error("the ICO entry must point at the PNG it carries")
	}
}
