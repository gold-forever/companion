package coin

import (
	"image/color"
	"testing"
)

func TestTheCoinIsGoldInsideADarkerRimOnNothing(t *testing.T) {
	for _, size := range []int{16, 32, 48, 256} {
		img := Image(size)
		if img.Bounds().Dx() != size || img.Bounds().Dy() != size {
			t.Fatalf("%dpx: got %v", size, img.Bounds())
		}
		if got := img.NRGBAAt(size/2, size/2); got != gold {
			t.Errorf("%dpx: the middle is %v, want gold", size, got)
		}
		if got := img.NRGBAAt(size/2, 1); got != rim {
			t.Errorf("%dpx: the top edge is %v, want the rim", size, got)
		}
		if got := img.NRGBAAt(0, 0); got != (color.NRGBA{}) {
			t.Errorf("%dpx: the corner is %v, want transparent", size, got)
		}
	}
}
