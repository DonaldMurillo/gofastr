package file

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
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

func TestStripPNGPreservesOrientedNRGBA64(t *testing.T) {
	src := image.NewNRGBA64(image.Rect(0, 0, 3, 2))
	src.SetNRGBA64(0, 0, color.NRGBA64{R: 0x1234, G: 0x5678, B: 0x9abc, A: 0xdef0})
	src.SetNRGBA64(2, 0, color.NRGBA64{R: 0xa1b2, G: 0xc3d4, B: 0xe5f6, A: 0xffff})
	src.SetNRGBA64(1, 1, color.NRGBA64{R: 0x1357, G: 0x2468, B: 0x9ace, A: 0})
	var enc bytes.Buffer
	if err := png.Encode(&enc, src); err != nil {
		t.Fatal(err)
	}
	const ihdrEnd = 8 + 12 + 13
	raw := enc.Bytes()
	data := appendPNGChunk(bytes.Clone(raw[:ihdrEnd]), "eXIf", minimalOrientationTIFF(6))
	data = append(data, raw[ihdrEnd:]...)
	out, err := stripPNG(data)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := decoded.(*image.NRGBA64)
	if !ok {
		t.Fatalf("oriented PNG decoded as %T, want *image.NRGBA64", decoded)
	}
	if want := image.Rect(0, 0, 2, 3); got.Bounds() != want {
		t.Fatalf("bounds = %v, want %v", got.Bounds(), want)
	}
	for y := range 2 {
		for x := range 3 {
			if p, want := got.NRGBA64At(1-y, x), src.NRGBA64At(x, y); p != want {
				t.Errorf("oriented pixel for (%d, %d) = %#v, want %#v", x, y, p, want)
			}
		}
	}
}

func TestStripMetadataRefusesMissingEndMarkers(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 4, 4))
	var jpg, pngFile bytes.Buffer
	if err := jpeg.Encode(&jpg, img, nil); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&pngFile, img); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		body []byte
		want string
	}{
		{"truncated.jpg", bytes.TrimSuffix(jpg.Bytes(), []byte{0xff, 0xd9}), "missing EOI"},
		{"truncated.png", bytes.TrimSuffix(pngFile.Bytes(), appendPNGChunk(nil, "IEND", nil)), "missing IEND"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemStore()
			_, err := ProcessFileField(context.Background(), store, bytes.NewReader(tc.body), tc.name, "posts", "photo", StripMetadata())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ProcessFileField error = %v, want %q", err, tc.want)
			}
			if len(store.saved) != 0 {
				t.Errorf("stored %d object(s), want 0", len(store.saved))
			}
		})
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
