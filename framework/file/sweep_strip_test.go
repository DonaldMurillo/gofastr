package file

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestStripJPEGWalk(t *testing.T) {
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, image.NewGray(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	out, err := stripJPEG(append(enc.Bytes(), "TRAILER"...))
	if err != nil || bytes.Contains(out, []byte("TRAILER")) {
		t.Errorf("JPEG trailer after EOI kept (err %v)", err)
	}
	synthetic := []byte("\xff\xd8\xff\xff\xfe\x00\x03x\xff\xda\x00\x02ab\xff\xd0cd\xff\x00\xff\xd9")
	out, err = stripJPEG(synthetic)
	if err != nil {
		t.Fatalf("fill byte or restart marker refused: %v", err)
	}
	if want := "\xff\xd8\xff\xda\x00\x02ab\xff\xd0cd\xff\x00\xff\xd9"; string(out) != want {
		t.Errorf("stripJPEG = %q, want %q", out, want)
	}
	if _, err := stripJPEG([]byte("\xff\xd8\xff\xe1\x00\x01xx\xff\xd9")); err == nil {
		t.Error("a segment length under 2 was accepted")
	}
}

func TestSpliceAPNGDropsTextAndTrailer(t *testing.T) {
	data := []byte("\x89PNG\r\n\x1a\n")
	data = appendPNGChunk(data, "acTL", []byte{0, 0, 0, 1, 0, 0, 0, 0})
	data = appendPNGChunk(data, "iTXt", []byte("secret-itxt"))
	data = appendPNGChunk(data, "IDAT", []byte("frame"))
	data = appendPNGChunk(data, "IEND", nil)
	data = append(data, "TRAILER-TRAILER-TRAILER!"...)
	out := spliceAPNG(data, 0)
	for _, bad := range []string{"secret-itxt", "TRAILER-"} {
		if bytes.Contains(out, []byte(bad)) {
			t.Errorf("spliced APNG keeps %q", bad)
		}
	}
	if !bytes.Contains(out, []byte("frame")) {
		t.Error("spliced APNG lost its frame")
	}
}

func TestStripPNGReadsPrefixedEXIFOrientation(t *testing.T) {
	var enc bytes.Buffer
	if err := png.Encode(&enc, image.NewGray(image.Rect(0, 0, 3, 1))); err != nil {
		t.Fatal(err)
	}
	raw := enc.Bytes()
	ihdrEnd := 8 + 12 + 13
	data := append([]byte{}, raw[:ihdrEnd]...)
	data = appendPNGChunk(data, "eXIf", append([]byte("Exif\x00\x00"), minimalOrientationTIFF(6)...))
	data = append(data, raw[ihdrEnd:]...)
	out, err := stripPNG(data)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width != 1 || cfg.Height != 3 {
		t.Fatalf("stripped PNG is %dx%d (err %v), want 1x3 after orientation 6", cfg.Width, cfg.Height, err)
	}
}

func TestStripPixelCapRefusesEmptyDimensions(t *testing.T) {
	for _, d := range [][2]int{{0, 5}, {5, 0}} {
		if checkStripPixelCap(d[0], d[1]) == nil {
			t.Errorf("checkStripPixelCap(%d, %d) accepted", d[0], d[1])
		}
	}
}

func riffChunk(typ string, payload []byte) []byte {
	b := append([]byte(typ), byte(len(payload)), byte(len(payload)>>8), 0, 0)
	b = append(b, payload...)
	if len(payload)%2 == 1 {
		b = append(b, 0)
	}
	return b
}

func riff(chunks ...[]byte) []byte {
	body := []byte("WEBP")
	for _, c := range chunks {
		body = append(body, c...)
	}
	n := len(body)
	return append([]byte{'R', 'I', 'F', 'F', byte(n), byte(n >> 8), 0, 0}, body...)
}

func TestStripWebPContainer(t *testing.T) {
	short := riff(riffChunk("VP8L", []byte("abcd")))
	short[4] = 200
	if _, err := stripWebP(short); err == nil {
		t.Error("a RIFF size past the file was accepted")
	}
	in := riff(riffChunk("VP8X", make([]byte, 10)), riffChunk("VP8L", []byte("odd")), riffChunk("EXIF", minimalOrientationTIFF(6)))
	out, err := stripWebP(in)
	if err != nil {
		t.Fatal(err)
	}
	if out[12+8]&webpVP8XEXIFFlag == 0 {
		t.Error("VP8X does not advertise the kept orientation EXIF")
	}
	if !bytes.Contains(out, []byte("odd\x00EXIF")) {
		t.Errorf("odd chunk not padded before the next chunk: %q", out)
	}
}

func TestOrientationSevenIsTransverse(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 2, 2))
	src.Pix = []byte{'A', 'B', 'C', 'D'}
	got := applyOrientation(src, 7)
	want := map[[2]int]byte{{0, 0}: 'D', {1, 0}: 'B', {0, 1}: 'C', {1, 1}: 'A'}
	for p, v := range want {
		if r, _, _, _ := got.At(p[0], p[1]).RGBA(); byte(r>>8) != v {
			t.Errorf("pixel %v = %q, want %q", p, byte(r>>8), v)
		}
	}
}
