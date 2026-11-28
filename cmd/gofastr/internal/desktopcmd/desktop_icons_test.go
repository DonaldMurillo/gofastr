package desktopcmd

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestNonSquareIconIsCroppedNotStretched(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 300, 100))
	for y := range 100 {
		for x := range 300 {
			c := color.NRGBA{R: 255, A: 255}
			if x >= 100 && x < 200 {
				c = color.NRGBA{B: 255, A: 255}
			}
			src.Set(x, y, c)
		}
	}
	var in bytes.Buffer
	if err := png.Encode(&in, src); err != nil {
		t.Fatal(err)
	}
	cornerIsCropped := func(t *testing.T, frame []byte) {
		t.Helper()
		img, err := png.Decode(bytes.NewReader(frame))
		if err != nil {
			t.Fatal(err)
		}
		if r, _, b, _ := img.At(0, 0).RGBA(); r > b {
			t.Fatal("the icon's corner is the source's red edge: a non-square icon was stretched, not center-cropped")
		}
	}
	t.Run("ico", func(t *testing.T) {
		ico, err := buildWindowsICO(in.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		size := binary.LittleEndian.Uint32(ico[6+8:])
		offset := binary.LittleEndian.Uint32(ico[6+12:])
		cornerIsCropped(t, ico[offset:offset+size])
	})
	t.Run("icns", func(t *testing.T) {
		icns, err := buildICNS(in.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		// An 8-byte file header, then the first entry's type and length,
		// a length that counts those 8 bytes too.
		size := binary.BigEndian.Uint32(icns[12:16])
		cornerIsCropped(t, icns[16:8+size])
	})
}
