package file_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/file"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
)

// Pins the documented contract that stored originals KEEP their image
// metadata by default, found by the 2026-09-04 red-probe round
// (exif_red_test.go asserted the opposite policy and was deleted per the
// maintainer's decision on that round's contract question): preservation
// is deliberate — hosts opt into stripping with file.StripMetadata().
// Property: without the opt-in, ProcessFileField stores the uploaded
// original byte-for-byte, EXIF block and all.
// Surfaces: framework/file/filefield.go::ProcessFileField (store.Save of
// the original), core/upload/serve.go (serves it back unchanged).
func TestProcessFileField_KeepsEXIFByDefault(t *testing.T) {
	store := &captureStorage{}
	withExif := jpegWithEXIF(t, 1)
	if !bytes.Contains(withExif, []byte("Exif\x00\x00")) || !bytes.Contains(withExif, []byte("GPS\x00")) {
		t.Fatal("fixture does not carry an EXIF/GPS segment — setup broken")
	}

	ff, err := file.ProcessFileField(context.Background(), store,
		bytes.NewReader(withExif), "photo.jpg", "posts", "avatar")
	if err != nil {
		t.Fatalf("legitimate JPEG with EXIF rejected by default: %v", err)
	}
	if !bytes.Equal(store.data, withExif) {
		t.Errorf("default must store the original byte-for-byte: stored %d bytes, original %d",
			len(store.data), len(withExif))
	}
	if !bytes.Contains(store.data, []byte("GPS\x00")) {
		t.Errorf("default stripped metadata it was not asked to strip")
	}
	if ff.Size != int64(len(withExif)) {
		t.Errorf("FileField.Size = %d, want the preserved length %d", ff.Size, len(withExif))
	}
}

// Pins that file.StripMetadata() removes EXIF and equivalent segments
// from the stored original, found by the 2026-09-04 red-probe round;
// fixed in framework/file/strip.go by splicing/re-encoding JPEG, PNG,
// and WebP containers before the store, baking EXIF orientation first.
// Property: an upload made with file.StripMetadata() stores no
// privacy-bearing metadata segment (JPEG APPn/COM, PNG tEXt/zTXt/iTXt/
// eXIf, WebP EXIF/XMP) and still displays upright.
// Surfaces: framework/file/strip.go (stripJPEG/stripPNG/stripWebP),
// framework/file/filefield.go::ProcessFileField (the hook), and every
// image type whose originals this path stores.
func TestStripMetadataOption_RemovesSegments(t *testing.T) {
	process := func(t *testing.T, body []byte, filename string) (*file.FileField, *captureStorage) {
		t.Helper()
		store := &captureStorage{}
		ff, err := file.ProcessFileField(context.Background(), store,
			bytes.NewReader(body), filename, "posts", "photo", file.StripMetadata())
		if err != nil {
			t.Fatalf("ProcessFileField with StripMetadata: %v", err)
		}
		if ff == nil || ff.URL == "" {
			t.Fatal("no stored file")
		}
		return ff, store
	}

	t.Run("JPEG APP1 EXIF with GPS", func(t *testing.T) {
		ff, store := process(t, jpegWithEXIF(t, 1), "photo.jpg")
		if bytes.Contains(store.data, []byte("Exif\x00\x00")) || bytes.Contains(store.data, []byte("GPS\x00")) {
			t.Errorf("SECURITY: [exif-strip] stored JPEG original keeps its EXIF/GPS block")
		}
		if _, err := jpeg.Decode(bytes.NewReader(store.data)); err != nil {
			t.Errorf("stripped JPEG no longer decodes: %v", err)
		}
		if ff.Size != int64(len(store.data)) {
			t.Errorf("Size = %d, want stripped length %d", ff.Size, len(store.data))
		}
	})

	t.Run("JPEG COM comment", func(t *testing.T) {
		body := jpegWithSegments(t, solidJPEG(t, 2, 2), comSegment("camera serial 4211"))
		_, store := process(t, body, "photo.jpg")
		if bytes.Contains(store.data, []byte("camera serial")) {
			t.Errorf("SECURITY: [com-strip] stored JPEG original keeps its comment segment")
		}
	})

	t.Run("JPEG orientation baked upright", func(t *testing.T) {
		// 4x2, left half red / right half green, EXIF orientation 6
		// ("rotate 90 CW to display"). Stripping drops the tag, so the
		// rotation must be baked in or the photo renders sideways.
		body := jpegWithSegments(t, halfRedGreenJPEG(t), app1ExifSegment(tiffWithGPSAndOrientation(6)))
		_, store := process(t, body, "photo.jpg")
		if bytes.Contains(store.data, []byte("Exif\x00\x00")) {
			t.Fatalf("EXIF block survived the strip")
		}
		img, err := jpeg.Decode(bytes.NewReader(store.data))
		if err != nil {
			t.Fatalf("stripped JPEG does not decode: %v", err)
		}
		if got := img.Bounds(); got.Dx() != 2 || got.Dy() != 4 {
			t.Fatalf("orientation not baked: bounds %v, want 2x4 (source 4x2 rotated)", got)
		}
		// After a 90° CW bake the red half is the top rows. RGBA()
		// returns 16-bit channels; compare against 8-bit thresholds.
		tr, tg, _, _ := img.At(1, 1).RGBA()
		br, bg, _, _ := img.At(1, 2).RGBA()
		if tr>>8 < 150 || tg>>8 > 80 {
			t.Errorf("top pixel is not red: R=%d G=%d", tr>>8, tg>>8)
		}
		if bg>>8 < 150 || br>>8 > 80 {
			t.Errorf("bottom pixel is not green: R=%d G=%d", br>>8, bg>>8)
		}
	})

	t.Run("PNG tEXt zTXt iTXt eXIf", func(t *testing.T) {
		body := pngWithMetadata(t, 1)
		_, store := process(t, body, "photo.png")
		for _, marker := range []string{"tEXt", "zTXt", "iTXt", "eXIf", "secret camera serial"} {
			if bytes.Contains(store.data, []byte(marker)) {
				t.Errorf("SECURITY: [png-strip] stored PNG keeps %q", marker)
			}
		}
		img, err := png.Decode(bytes.NewReader(store.data))
		if err != nil {
			t.Fatalf("stripped PNG does not decode: %v", err)
		}
		if got := img.Bounds(); got.Dx() != 2 || got.Dy() != 2 {
			t.Errorf("stripped PNG bounds changed: %v", got)
		}
	})

	t.Run("PNG orientation baked upright", func(t *testing.T) {
		// 2x1 red|green with an eXIf orientation of 6: lossless format,
		// so the baked pixels are exactly the rotated originals.
		body := pngWithMetadata(t, 6)
		_, store := process(t, body, "photo.png")
		img, err := png.Decode(bytes.NewReader(store.data))
		if err != nil {
			t.Fatalf("stripped PNG does not decode: %v", err)
		}
		if got := img.Bounds(); got.Dx() != 1 || got.Dy() != 2 {
			t.Fatalf("orientation not baked: bounds %v, want 1x2", got)
		}
		if _, g, _, _ := img.At(0, 0).RGBA(); g>>8 > 100 {
			t.Errorf("top pixel is green, image was not rotated upright")
		}
		if r, _, _, _ := img.At(0, 1).RGBA(); r>>8 > 100 {
			t.Errorf("bottom pixel is red, image was rotated the wrong way")
		}
	})

	t.Run("clean PNG unchanged", func(t *testing.T) {
		clean := solidPNG(t, 2, 2)
		_, store := process(t, clean, "photo.png")
		if !bytes.Equal(store.data, clean) {
			t.Errorf("metadata-free PNG must round-trip byte-identical")
		}
	})

	t.Run("WebP EXIF and XMP chunks", func(t *testing.T) {
		body := webpWithMetadata(t, 1, true)
		_, store := process(t, body, "photo.webp")
		chunks := webpTopChunks(store.data)
		if _, ok := chunks["EXIF"]; ok {
			t.Errorf("SECURITY: [webp-strip] stored WebP keeps its EXIF chunk")
		}
		if _, ok := chunks["XMP "]; ok {
			t.Errorf("SECURITY: [webp-strip] stored WebP keeps its XMP chunk")
		}
		if !bytes.Contains(chunks["VP8L"], []byte("fake-vp8l")) {
			t.Errorf("image data chunk was not preserved verbatim")
		}
		if vp8x := chunks["VP8X"]; vp8x != nil && vp8x[0]&0x0C != 0 {
			t.Errorf("VP8X still advertises dropped EXIF/XMP chunks: flags %#x", vp8x[0])
		}
		if bytes.Contains(store.data, []byte("GPS\x00")) {
			t.Errorf("stored WebP keeps GPS-tagged bytes")
		}
	})

	t.Run("WebP orientation kept minimal", func(t *testing.T) {
		// WebP has no re-encoder at this layer, so a non-trivial
		// orientation is preserved as an orientation-only EXIF chunk.
		body := webpWithMetadata(t, 6, true)
		_, store := process(t, body, "photo.webp")
		exif := webpTopChunks(store.data)["EXIF"]
		if exif == nil {
			t.Fatalf("orientation EXIF chunk dropped entirely; WebP renders sideways")
		}
		if len(exif) != 26 || exif[18] != 6 {
			t.Errorf("EXIF chunk is not the orientation-only payload: %d bytes, value byte %#x", len(exif), exif[18])
		}
		if bytes.Contains(exif, []byte("GPS\x00")) {
			t.Errorf("SECURITY: [webp-strip] orientation EXIF keeps GPS-tagged entries")
		}
		if vp8x := webpTopChunks(store.data)["VP8X"]; vp8x == nil || vp8x[0]&0x08 == 0 {
			t.Errorf("VP8X does not advertise the surviving EXIF chunk")
		}
	})

	t.Run("non-image passthrough", func(t *testing.T) {
		body := []byte("plain text attachment, nothing to strip")
		_, store := process(t, body, "notes.txt")
		if !bytes.Equal(store.data, body) {
			t.Errorf("non-image content must pass through unchanged")
		}
	})

	t.Run("deriver sees stripped bytes", func(t *testing.T) {
		store := &captureStorage{}
		d := &capturingDeriver{}
		ff, err := file.ProcessFileField(context.Background(), store,
			bytes.NewReader(jpegWithEXIF(t, 1)), "photo.jpg", "posts", "photo",
			file.StripMetadata(), file.WithImageDeriver(d))
		if err != nil {
			t.Fatalf("ProcessFileField: %v", err)
		}
		if bytes.Contains(d.data, []byte("Exif\x00\x00")) {
			t.Errorf("deriver received unstripped bytes; renditions derive from the original's metadata")
		}
		if ff.Image == nil {
			t.Errorf("deriver result not attached")
		}
	})
}

// Pins that framework/file's local orientation bake (strip.go's
// applyOrientation, reached through StripMetadata) produces the same
// pixels as framework/image's AutoOrient — the two are leaf-local twins
// by the layering rule, and a drift in either would silently rotate
// originals the wrong way. Found by the 2026-09-04 red-probe round's
// sibling-sweep rule (fix one site, pin its twin).
// Property: for every EXIF orientation 2..8, the stripped-and-baked
// JPEG decodes to the same pixels AutoOrient produces from the original.
// Surfaces: framework/file/strip.go::applyOrientation vs
// framework/image/geometry.go::AutoOrient.
func TestStripOrientationBakeMatchesPipeline(t *testing.T) {
	for orient := 2; orient <= 8; orient++ {
		t.Run(string(rune('0'+orient)), func(t *testing.T) {
			src := jpegWithSegments(t, halfRedGreenJPEG(t), app1ExifSegment(tiffWithGPSAndOrientation(orient)))

			store := &captureStorage{}
			if _, err := file.ProcessFileField(context.Background(), store,
				bytes.NewReader(src), "photo.jpg", "posts", "photo", file.StripMetadata()); err != nil {
				t.Fatalf("ProcessFileField: %v", err)
			}
			stripped, err := jpeg.Decode(bytes.NewReader(store.data))
			if err != nil {
				t.Fatalf("stripped JPEG does not decode: %v", err)
			}

			orig, err := fwimage.DecodeBytes(src)
			if err != nil {
				t.Fatalf("framework/image decode: %v", err)
			}
			want := orig.AutoOrient().GoImage()

			got, wantB := stripped.Bounds(), want.Bounds()
			if got.Dx() != wantB.Dx() || got.Dy() != wantB.Dy() {
				t.Fatalf("bounds differ: stripped %v, AutoOrient %v", got, wantB)
			}
			for y := range wantB.Dy() {
				for x := range wantB.Dx() {
					gr, gg, gb, _ := stripped.At(got.Min.X+x, got.Min.Y+y).RGBA()
					wr, wg, wb, _ := want.At(wantB.Min.X+x, wantB.Min.Y+y).RGBA()
					// The strip path re-encodes once at quality 95, so
					// compare with a small lossy tolerance.
					if diff32(gr, wr) > 4096 || diff32(gg, wg) > 4096 || diff32(gb, wb) > 4096 {
						t.Fatalf("pixel (%d,%d): stripped (%d,%d,%d), AutoOrient (%d,%d,%d)",
							x, y, gr, gg, gb, wr, wg, wb)
					}
				}
			}
		})
	}
}

// diff32 returns the absolute difference of two 16-bit color values
// (the RGBA() word width).
func diff32(a, b uint32) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// capturingDeriver records the bytes ProcessFileField hands it.
type capturingDeriver struct{ data []byte }

func (d *capturingDeriver) DeriveImage(ctx context.Context, store upload.Storage, data []byte, primaryRef string) (*file.ImageDerivatives, error) {
	d.data = append([]byte(nil), data...)
	return &file.ImageDerivatives{BlurHash: "LEHV6nWB2yk8pyo0adR*.7kCMdnj"}, nil
}

// --- fixtures ------------------------------------------------------------

// tiffWithGPSAndOrientation builds a little-endian TIFF whose IFD0 has an
// orientation entry and a GPSInfo pointer entry carrying plainly
// recognizable bytes, mirroring framework/image's insertExifAPP1 shape
// (twin helper; not importable across packages).
func tiffWithGPSAndOrientation(orientation int) []byte {
	buf := []byte{
		'I', 'I', 0x2A, 0x00, // little-endian, magic
		0x08, 0x00, 0x00, 0x00, // IFD0 at offset 8
		0x02, 0x00, // two entries
	}
	buf = append(buf,
		0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, byte(orientation), 0x00, 0x00, 0x00, // orientation
		0x25, 0x88, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 'G', 'P', 'S', 0x00, // GPSInfo tag
		0x00, 0x00, 0x00, 0x00, // next IFD = 0
	)
	return buf
}

func app1ExifSegment(tiff []byte) []byte {
	exif := append([]byte("Exif\x00\x00"), tiff...)
	segLen := len(exif) + 2
	return append([]byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen & 0xFF)}, exif...)
}

func comSegment(s string) []byte {
	segLen := len(s) + 2
	return append([]byte{0xFF, 0xFE, byte(segLen >> 8), byte(segLen & 0xFF)}, []byte(s)...)
}

// jpegWithSegments splices extra marker segments right after the SOI of
// a freshly encoded JPEG.
func jpegWithSegments(t *testing.T, encoded []byte, segs ...[]byte) []byte {
	t.Helper()
	if len(encoded) < 2 || encoded[0] != 0xFF || encoded[1] != 0xD8 {
		t.Fatal("encoded fixture is not a JPEG")
	}
	out := []byte{0xFF, 0xD8}
	for _, seg := range segs {
		out = append(out, seg...)
	}
	return append(out, encoded[2:]...)
}

func jpegWithEXIF(t *testing.T, orientation int) []byte {
	t.Helper()
	return jpegWithSegments(t, solidJPEG(t, 2, 2), app1ExifSegment(tiffWithGPSAndOrientation(orientation)))
}

func solidJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 10, G: 200, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// halfRedGreenJPEG is 4x2 with the left half red and the right half
// green, so a 90° rotation is visible in both dimensions and color.
func halfRedGreenJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := range 2 {
		for x := range 4 {
			if x < 2 {
				img.Set(x, y, color.RGBA{R: 220, G: 20, B: 20, A: 255})
			} else {
				img.Set(x, y, color.RGBA{R: 20, G: 220, B: 20, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 10, G: 200, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func pngChunk(typ string, payload []byte) []byte {
	var b bytes.Buffer
	var lb [4]byte
	binary.BigEndian.PutUint32(lb[:], uint32(len(payload)))
	b.Write(lb[:])
	b.WriteString(typ)
	b.Write(payload)
	var cb [4]byte
	binary.BigEndian.PutUint32(cb[:], crc32.ChecksumIEEE(append([]byte(typ), payload...)))
	b.Write(cb[:])
	return b.Bytes()
}

// pngWithMetadata returns a PNG carrying tEXt/zTXt/iTXt/eXIf chunks.
// With orientation 2..8 the image is 2x1 red|green so the bake is
// visible; otherwise a plain 2x2.
func pngWithMetadata(t *testing.T, orientation int) []byte {
	t.Helper()
	var base []byte
	if orientation >= 2 && orientation <= 8 {
		img := image.NewRGBA(image.Rect(0, 0, 2, 1))
		img.Set(0, 0, color.RGBA{R: 220, G: 20, B: 20, A: 255})
		img.Set(1, 0, color.RGBA{R: 20, G: 220, B: 20, A: 255})
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatalf("encode png: %v", err)
		}
		base = buf.Bytes()
	} else {
		base = solidPNG(t, 2, 2)
	}
	// Splice metadata chunks before IEND (the final 12 bytes).
	sig := base[:8]
	iend := base[len(base)-12:]
	mid := base[8 : len(base)-12]
	out := append([]byte{}, sig...)
	out = append(out, mid...)
	out = append(out, pngChunk("tEXt", []byte("Comment\x00secret camera serial"))...)
	out = append(out, pngChunk("zTXt", []byte("Comment\x00\x00\x00compressed-secret"))...)
	out = append(out, pngChunk("iTXt", []byte("Keyword\x00\x00\x00\x00int'l secret"))...)
	out = append(out, pngChunk("eXIf", tiffWithGPSAndOrientation(orientation))...)
	return append(out, iend...)
}

type webpChunk struct {
	typ     string
	payload []byte
}

func buildWebP(t *testing.T, chunks ...webpChunk) []byte {
	t.Helper()
	var body bytes.Buffer
	body.WriteString("WEBP")
	for _, c := range chunks {
		body.WriteString(c.typ)
		var lb [4]byte
		binary.LittleEndian.PutUint32(lb[:], uint32(len(c.payload)))
		body.Write(lb[:])
		body.Write(c.payload)
		if len(c.payload)%2 == 1 {
			body.WriteByte(0)
		}
	}
	out := bytes.NewBufferString("RIFF")
	var lb [4]byte
	binary.LittleEndian.PutUint32(lb[:], uint32(body.Len()))
	out.Write(lb[:])
	out.Write(body.Bytes())
	return out.Bytes()
}

// webpWithMetadata builds a WebP container with a VP8X header
// (EXIF|XMP flags set), a stand-in VP8L image chunk, an EXIF chunk, and
// an XMP chunk. The image payload is fake bytes: nothing in the strip
// path decodes WebP pixels, the container walk is the surface under
// test, and ProcessFileField's sniffer only reads the RIFF header.
func webpWithMetadata(t *testing.T, orientation int, withXMP bool) []byte {
	t.Helper()
	chunks := []webpChunk{
		{typ: "VP8X", payload: []byte{0x0C, 0, 0, 0, 4, 0, 0, 2, 0, 0}}, // EXIF|XMP flags, 4x2 canvas
		{typ: "VP8L", payload: []byte("fake-vp8l-data")},
		{typ: "EXIF", payload: tiffWithGPSAndOrientation(orientation)},
	}
	if withXMP {
		chunks = append(chunks, webpChunk{typ: "XMP ", payload: []byte("<x:xmpmeta>secret xmp</x:xmpmeta>")})
	}
	return buildWebP(t, chunks...)
}

// webpTopChunks returns the top-level chunks of a WebP container.
func webpTopChunks(data []byte) map[string][]byte {
	m := map[string][]byte{}
	if len(data) < 12 || string(data[:4]) != "RIFF" {
		return m
	}
	riffEnd := 8 + int(uint32(binary.LittleEndian.Uint32(data[4:8])))
	pos := 12
	for pos+8 <= riffEnd && pos <= len(data) {
		ct := string(data[pos : pos+4])
		cl := int(uint32(binary.LittleEndian.Uint32(data[pos+4 : pos+8])))
		if pos+8+cl > len(data) {
			break
		}
		m[ct] = data[pos+8 : pos+8+cl]
		pos += 8 + cl
		if cl%2 == 1 {
			pos++
		}
	}
	return m
}
