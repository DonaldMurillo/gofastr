package imagefield_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"path"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/imagefield"
)

// sidewaysJPEG is a 64x32 (landscape) JPEG whose EXIF orientation 6
// says "rotate 90 CW to display": a phone photo taken in portrait.
func sidewaysJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{uint8(x * 4), 80, 160, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	enc := buf.Bytes()
	tiff := []byte{
		'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00, // little-endian TIFF, IFD0 at 8
		0x01, 0x00, // one entry
		0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00, // Orientation = 6
		0x00, 0x00, 0x00, 0x00, // no next IFD
	}
	exif := append([]byte("Exif\x00\x00"), tiff...)
	segLen := len(exif) + 2
	out := append([]byte{0xFF, 0xD8, 0xFF, 0xE1, byte(segLen >> 8), byte(segLen)}, exif...)
	return append(out, enc[2:]...)
}

// TestRenditionsFollowOrientation: an EXIF-aware viewer shows the
// original upright, so its renditions and placeholder must be upright
// too. They came out sideways because the decoded source was never
// auto-oriented.
func TestRenditionsFollowOrientation(t *testing.T) {
	t.Parallel()
	d, err := imagefield.New(imagefield.Config{
		Variants:    []fwimage.Variant{{Width: 16, Format: fwimage.FormatJPEG, Suffix: "sm"}},
		Placeholder: &fwimage.PlaceholderOptions{Width: 8},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	store := newMemStore()
	got, err := d.DeriveImage(context.Background(), store, sidewaysJPEG(t), "uploads/posts/photo/p_1_ab.jpg")
	if err != nil {
		t.Fatalf("DeriveImage: %v", err)
	}
	v := got.Variants[0]
	if v.Height <= v.Width {
		t.Errorf("rendition is %dx%d, want portrait (source 64x32 with orientation 6)", v.Width, v.Height)
	}
	stored, err := jpeg.DecodeConfig(bytes.NewReader(store.objects[v.StorageRef]))
	if err != nil {
		t.Fatalf("stored rendition does not decode: %v", err)
	}
	if stored.Height <= stored.Width {
		t.Errorf("stored rendition is %dx%d, want portrait", stored.Width, stored.Height)
	}
	_, b64, ok := strings.Cut(got.Placeholder, ";base64,")
	if !ok {
		t.Fatalf("placeholder %.40q is not a base64 data URL", got.Placeholder)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("placeholder base64: %v", err)
	}
	ph, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("placeholder does not decode: %v", err)
	}
	if ph.Height <= ph.Width {
		t.Errorf("placeholder is %dx%d, want portrait", ph.Width, ph.Height)
	}
}

// TestRenditionNamesFitFilenameLimit: the original's key is sized to
// upload.MaxFilenameBytes, so appending "-<suffix>.<ext>" pushed every
// rendition name past it and local storage failed the upload.
func TestRenditionNamesFitFilenameLimit(t *testing.T) {
	t.Parallel()
	if fwimage.MaxVariantNameBytes != upload.MaxFilenameBytes {
		t.Fatalf("image.MaxVariantNameBytes = %d, upload.MaxFilenameBytes = %d: keep them equal",
			fwimage.MaxVariantNameBytes, upload.MaxFilenameBytes)
	}
	key := "uploads/posts/photo/" + upload.UniqueFilename(strings.Repeat("a", 400)+".png")
	if n := len(path.Base(key)); n < upload.MaxFilenameBytes-4 {
		t.Fatalf("setup broken: original name is %d bytes, want near %d", n, upload.MaxFilenameBytes)
	}
	d, err := imagefield.New(landscapeConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := d.DeriveImage(context.Background(), newMemStore(), testPNG(t, 800, 600), key)
	if err != nil {
		t.Fatalf("DeriveImage: %v", err)
	}
	token := strings.TrimSuffix(path.Base(key), ".png")
	token = token[strings.LastIndexByte(token, '_'):]
	for _, v := range got.Variants {
		name := path.Base(v.StorageRef)
		if len(name) > upload.MaxFilenameBytes {
			t.Errorf("rendition name is %d bytes, over upload.MaxFilenameBytes=%d", len(name), upload.MaxFilenameBytes)
		}
		if !strings.Contains(name, token+"-") {
			t.Errorf("rendition %q lost the original's random token %q", name, token)
		}
	}
}
