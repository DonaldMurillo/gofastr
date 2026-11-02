package file_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/file"
)

// StripMetadata drops whatever follows a JPEG's EOI, but a PNG or WebP
// with no metadata chunk was returned byte-for-byte, so bytes after the
// PNG IEND chunk or past the WebP RIFF size survived into the stored
// original. Readers ignore them and phone and editing tools park data
// there (a location, a second image), so it is metadata the caller asked
// to lose. Property: with StripMetadata, nothing past the image's own end
// marker is stored, for every format the strip path handles.
func TestStripMetadataDropsTrailers(t *testing.T) {
	const secret = "lat=51.5007 lon=-0.1246 serial=4211"
	strip := func(t *testing.T, body []byte, name string) []byte {
		t.Helper()
		store := &captureStorage{}
		if _, err := file.ProcessFileField(context.Background(), store,
			bytes.NewReader(body), name, "posts", "photo", file.StripMetadata()); err != nil {
			t.Fatalf("ProcessFileField with StripMetadata: %v", err)
		}
		return store.data
	}

	t.Run("PNG after IEND", func(t *testing.T) {
		var enc bytes.Buffer
		if err := png.Encode(&enc, image.NewGray(image.Rect(0, 0, 2, 2))); err != nil {
			t.Fatal(err)
		}
		stored := strip(t, append(enc.Bytes(), secret...), "photo.png")
		if bytes.Contains(stored, []byte(secret)) {
			t.Error("stored PNG keeps the bytes after IEND")
		}
		if _, err := png.Decode(bytes.NewReader(stored)); err != nil {
			t.Errorf("stripped PNG no longer decodes: %v", err)
		}
	})

	t.Run("WebP past the RIFF size", func(t *testing.T) {
		body := buildWebP(t, webpChunk{typ: "VP8L", payload: []byte("fake-vp8l-data")})
		want := append([]byte(nil), body...)
		stored := strip(t, append(body, secret...), "photo.webp")
		if bytes.Contains(stored, []byte(secret)) {
			t.Error("stored WebP keeps the bytes past its RIFF size")
		}
		if !bytes.Equal(stored, want) {
			t.Errorf("stored WebP = %q, want the container unchanged without the trailer", stored)
		}
	})
}
