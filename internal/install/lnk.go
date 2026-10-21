package install

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// A Windows shortcut (.lnk) in its smallest form that Explorer opens: a header, the target as a local
// path in LinkInfo (ANSI and Unicode), and the working folder. The format is Microsoft's
// [MS-SHLLINK]; writing it here avoids starting PowerShell or a COM object to make one.

var linkCLSID = [16]byte{0x01, 0x14, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}

const (
	hasLinkInfo   = 0x02
	hasWorkingDir = 0x10
	isUnicode     = 0x80

	linkInfoHeaderSize = 0x24 // with the Unicode offsets
	volumeIDSize       = 0x11 // the fixed part and an empty label
	driveFixed         = 3
	showNormal         = 1
)

func shellLink(target, workingDir string) []byte {
	var b []byte
	u32 := func(v uint32) { b = binary.LittleEndian.AppendUint32(b, v) }

	// ShellLinkHeader: 76 bytes.
	u32(0x4C)
	b = append(b, linkCLSID[:]...)
	u32(hasLinkInfo | hasWorkingDir | isUnicode)
	u32(0)                             // file attributes
	b = append(b, make([]byte, 24)...) // creation, access and write times
	u32(0)                             // file size
	u32(0)                             // icon index
	u32(showNormal)
	b = append(b, make([]byte, 12)...) // hot key (2) and reserved (2, 4, 4)

	// LinkInfo: the header, a VolumeID, then the paths.
	ansi := append([]byte(asciiOnly(target)), 0)
	wide := append(utf16Bytes(target), 0, 0)
	volumeAt := uint32(linkInfoHeaderSize)
	pathAt := volumeAt + volumeIDSize
	suffixAt := pathAt + uint32(len(ansi))
	widePathAt := suffixAt + 1
	wideSuffixAt := widePathAt + uint32(len(wide))
	size := wideSuffixAt + 2
	u32(size)
	u32(linkInfoHeaderSize)
	u32(1) // VolumeIDAndLocalBasePath
	u32(volumeAt)
	u32(pathAt)
	u32(0) // no network share
	u32(suffixAt)
	u32(widePathAt)
	u32(wideSuffixAt)
	u32(volumeIDSize)
	u32(driveFixed)
	u32(0)    // drive serial number
	u32(0x10) // the label follows at once
	b = append(b, 0)
	b = append(b, ansi...)
	b = append(b, 0) // empty common path suffix
	b = append(b, wide...)
	b = append(b, 0, 0)

	// StringData: the working folder, counted in UTF-16 units, then the terminal block.
	dir := utf16Bytes(workingDir)
	b = binary.LittleEndian.AppendUint16(b, uint16(len(dir)/2))
	b = append(b, dir...)
	u32(0)
	return b
}

func utf16Bytes(s string) []byte {
	var out []byte
	for _, u := range utf16.Encode([]rune(s)) {
		out = binary.LittleEndian.AppendUint16(out, u)
	}
	return out
}

// asciiOnly is the ANSI copy of a path; Windows reads the Unicode copy, which keeps every letter.
func asciiOnly(s string) string {
	return strings.Map(func(r rune) rune {
		if r > 0x7E {
			return '?'
		}
		return r
	}, s)
}
