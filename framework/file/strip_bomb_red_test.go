//go:build red

package file_test

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: header pixel dimensions of an untrusted image must pass the
// framework's 64 MP guard (framework/image DefaultMaxPixels, enforced
// DecodeConfig-first in framework/image/image.go::decodeBytes) BEFORE any
// pixel decode runs — including the decodes StripMetadata performs.
// Surfaces: framework/file/strip.go::stripPNG (any tEXt/zTXt/iTXt/eXIf
// chunk → png.Decode with no cap), framework/file/strip.go::stripJPEG
// (EXIF orientation 2..8 → jpeg.Decode with no cap); both reached from
// framework/crud/crud_upload.go::saveFilePart whenever StripUploadMetadata
// is set (documented option, docs/content/uploads.md).
// Finding: StripMetadata's strip path decodes the full raster from the
// sniffed bytes with no pixel guard, so a ~300 KB PNG (or a few-KB JPEG)
// whose header declares 8192×8193 — one pixel over the cap the image
// pipeline enforces — makes an unauthenticated multipart upload allocate
// and re-encode a ~256 MiB raster per request.
// Fix direction: run DecodeConfig + the DefaultMaxPixels check before
// png.Decode/jpeg.Decode in stripPNG/stripJPEG and fail the upload (the
// strip path is already fail-closed on structural errors).

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/file"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
)

// bombDims are one row past the 8192×8192 square DefaultMaxPixels allows:
// 8192×8193 = 67,117,056 px > 67,108,864 px. Sized to stay well inside a
// test process (RGBA raster ≈ 256 MiB), unlike a 65535×65535 header
// which would fatal-OOM the binary rather than prove the finding.
const (
	bombW = 8192
	bombH = 8193
)

// TestStripMetadataRedPngBombRefused: a PNG whose IHDR declares over-cap
// dimensions and whose IDAT is a complete zlib stream must be refused by
// the strip path on its header alone.
func TestStripMetadataRedPngBombRefused(t *testing.T) {
	body := pngBomb(t, bombW, bombH)

	// Setup proof: the header says what we claim and is over the cap the
	// image pipeline enforces, so the strip path must share it.
	cfg, err := png.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("setup broken: fixture header does not parse: %v", err)
	}
	if cfg.Width != bombW || cfg.Height != bombH {
		t.Fatalf("setup broken: IHDR says %dx%d, want %dx%d", cfg.Width, cfg.Height, bombW, bombH)
	}
	if int64(cfg.Width)*int64(cfg.Height) <= fwimage.DefaultMaxPixels {
		t.Fatalf("setup broken: %dx%d is not over the %d-px cap", bombW, bombH, fwimage.DefaultMaxPixels)
	}
	// Setup proof: the IDAT is a complete zlib stream of exactly the
	// expected byte count, so a refusal demanded below can only be a
	// pixel-cap refusal, never a truncated-input error.
	if err := idatInflatesTo(t, body, bombH*(1+bombW*4)); err != nil {
		t.Fatalf("setup broken: IDAT not a complete stream: %v", err)
	}

	store := &captureStorage{}
	if _, err := file.ProcessFileField(context.Background(), store,
		bytes.NewReader(body), "bomb.png", "posts", "photo", file.StripMetadata()); err == nil {
		t.Errorf("SECURITY: [strip-png-decode-bomb] StripMetadata accepted and decoded a PNG whose header declares %d×%d = %d px, over the framework's %d-px DefaultMaxPixels cap, with no DecodeConfig guard: png.Decode materialized the full ~%d MiB raster and the upload then succeeded. Attack: an unauthenticated multipart upload to any StripUploadMetadata route costs the attacker ~300 KB of wire and the server ~256 MiB of decode plus re-encode per request — a few concurrent requests exhaust memory.",
			bombW, bombH, int64(bombW)*int64(bombH), fwimage.DefaultMaxPixels, (int64(bombW)*int64(bombH)*4)>>20)
	}
}

// TestStripMetadataRedJpegOrientBombRefused: the orientation-bake path
// (stripJPEG decode → rotate → re-encode) must refuse an over-cap frame
// on its SOF dimensions alone.
func TestStripMetadataRedJpegOrientBombRefused(t *testing.T) {
	// Uniform gray frame: jpeg.Encode compresses it to a few hundred KB
	// regardless of dimensions, so the fixture is cheap to ship while
	// forcing any decoder to materialize a 67 MP raster.
	gray := image.NewGray(image.Rect(0, 0, bombW, bombH))
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, gray, &jpeg.Options{Quality: 1}); err != nil {
		t.Fatalf("setup broken: encoding gray frame: %v", err)
	}
	// APP1 orientation 6 routes stripJPEG through jpeg.Decode for the
	// bake — the unguarded pixel decode under test.
	body := jpegWithSegments(t, enc.Bytes(), app1ExifSegment(tiffWithGPSAndOrientation(6)))

	cfg, err := jpeg.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("setup broken: fixture header does not parse: %v", err)
	}
	if cfg.Width != bombW || cfg.Height != bombH {
		t.Fatalf("setup broken: SOF says %dx%d, want %dx%d", cfg.Width, cfg.Height, bombW, bombH)
	}
	if int64(cfg.Width)*int64(cfg.Height) <= fwimage.DefaultMaxPixels {
		t.Fatalf("setup broken: %dx%d is not over the %d-px cap", bombW, bombH, fwimage.DefaultMaxPixels)
	}
	// Setup proof: the frame is a real, complete JPEG, so the refusal
	// demanded below can only be a pixel-cap refusal.
	if _, err := jpeg.Decode(bytes.NewReader(body)); err != nil {
		t.Fatalf("setup broken: bomb JPEG does not decode cleanly: %v", err)
	}

	store := &captureStorage{}
	if _, err := file.ProcessFileField(context.Background(), store,
		bytes.NewReader(body), "bomb.jpg", "posts", "photo", file.StripMetadata()); err == nil {
		t.Errorf("SECURITY: [strip-jpeg-decode-bomb] StripMetadata's orientation bake decoded a JPEG whose SOF declares %d×%d = %d px, over the framework's %d-px DefaultMaxPixels cap, with no DecodeConfig guard: jpeg.Decode materialized the raster, rotate90 allocated a ~%d MiB RGBA copy, and the upload then succeeded. Attack: an unauthenticated multipart upload with an orientation-6 EXIF segment costs the attacker a few hundred KB of wire and the server a full decode + rotation + quality-95 re-encode per request.",
			bombW, bombH, int64(bombW)*int64(bombH), fwimage.DefaultMaxPixels, (int64(bombW)*int64(bombH)*4)>>20)
	}
}

// pngBomb builds a valid PNG whose IHDR declares w×h (color type 6,
// 8-bit) and whose single IDAT is a complete zlib stream covering the
// full filter stream of that frame, compressed from all-zero bytes
// (filter None, transparent black pixels). Decoding it is structurally
// unremarkable — the cost is purely the w×h allocation the header
// forces — and the compressed payload stays a few hundred KB.
func pngBomb(t *testing.T, w, h int) []byte {
	t.Helper()
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:8], uint32(h))
	ihdr[8] = 8 // bit depth
	ihdr[9] = 6 // color type: truecolor with alpha
	// ihdr[10:13] left zero: deflate, adaptive filtering, no interlace

	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	row := make([]byte, 1+w*4) // filter byte + RGBA, all zeros
	for range h {
		if _, err := zw.Write(row); err != nil {
			t.Fatalf("setup broken: compressing bomb rows: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("setup broken: closing zlib stream: %v", err)
	}

	out := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}
	out = append(out, pngChunk("IHDR", ihdr)...)
	// Any metadata chunk flips stripPNG's hasMeta and forces the
	// unguarded png.Decode; tEXt is the cheapest.
	out = append(out, pngChunk("tEXt", []byte("Comment\x00bomb"))...)
	out = append(out, pngChunk("IDAT", zbuf.Bytes())...)
	return append(out, pngChunk("IEND", nil)...)
}

// idatInflatesTo concatenates the PNG's IDAT chunks and reports whether
// they form one valid, complete zlib stream inflating to exactly want
// bytes. It pins that a later decode refusal is policy, not truncation.
func idatInflatesTo(t *testing.T, data []byte, want int) error {
	t.Helper()
	var idat []byte
	pos := 8
	for pos+8 <= len(data) {
		length := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		typ := string(data[pos+4 : pos+8])
		if typ == "IEND" {
			break
		}
		if typ == "IDAT" {
			idat = append(idat, data[pos+8:pos+8+length]...)
		}
		pos += 12 + length
	}
	zr, err := zlib.NewReader(bytes.NewReader(idat))
	if err != nil {
		return fmt.Errorf("zlib header: %w", err)
	}
	defer zr.Close()
	n, err := io.Copy(io.Discard, zr)
	if err != nil {
		return fmt.Errorf("inflating IDAT: %w", err)
	}
	if int(n) != want {
		return fmt.Errorf("inflated %d bytes, want %d", n, want)
	}
	return nil
}
