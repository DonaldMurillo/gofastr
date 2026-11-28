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
	ico, err := buildWindowsICO(in.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	size := binary.LittleEndian.Uint32(ico[6+8:])
	offset := binary.LittleEndian.Uint32(ico[6+12:])
	frame, err := png.Decode(bytes.NewReader(ico[offset : offset+size]))
	if err != nil {
		t.Fatal(err)
	}
	if r, _, b, _ := frame.At(0, 0).RGBA(); r > b {
		t.Fatal("the icon's corner is the source's red edge: a non-square icon was stretched, not center-cropped")
	}
}
