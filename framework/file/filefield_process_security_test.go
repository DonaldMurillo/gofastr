package file_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

type captureStorage struct {
	key  string
	data []byte
}

func (s *captureStorage) Save(ctx context.Context, key string, r io.Reader) error {
	s.key = key
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.data = data
	return nil
}

func (s *captureStorage) Delete(ctx context.Context, key string) error { return nil }

func (s *captureStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (s *captureStorage) Exists(ctx context.Context, key string) (bool, error) { return false, nil }

var _ upload.Storage = (*captureStorage)(nil)

func TestProcessFileField_RejectsSVGByDefault(t *testing.T) {
	store := &captureStorage{}
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`

	if _, err := file.ProcessFileField(context.Background(), store, bytes.NewReader([]byte(svg)), "payload.svg", "posts", "attachment"); err == nil {
		t.Fatal("SECURITY: [filefield] ProcessFileField accepted SVG content by default. Attack: stored active content can trigger script execution in downstream renderers.")
	}
}

func TestProcessFileField_RejectsExecutableContentByDefault(t *testing.T) {
	store := &captureStorage{}
	exe := append([]byte("MZ"), bytes.Repeat([]byte{0x00}, 512)...)

	if _, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(exe), "payload.exe", "posts", "attachment"); err == nil {
		t.Fatal("SECURITY: [filefield] ProcessFileField accepted executable content by default. Attack: framework stores dangerous binary payloads without an allowlist.")
	}
}

func TestProcessFileField_RejectsHTMLByDefault(t *testing.T) {
	store := &captureStorage{}
	html := []byte("<html><body><script>alert(1)</script></body></html>")

	if _, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(html), "payload.html", "posts", "attachment"); err == nil {
		t.Fatal("SECURITY: [filefield] ProcessFileField accepted HTML content by default. Attack: stored active content can execute in downstream renderers.")
	}
}

func TestProcessFileField_RejectsOversizeInputByDefault(t *testing.T) {
	store := &captureStorage{}
	huge := bytes.Repeat([]byte("A"), 33<<20)

	if _, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(huge), "large.bin", "posts", "attachment"); err == nil {
		t.Fatal("SECURITY: [filefield] ProcessFileField accepted a 33 MiB upload without a size limit. Attack: attacker can force unbounded in-memory buffering.")
	}
}

// TestProcessFileField_RejectsHiddenActiveContent covers active-markup
// shapes that the leading-token + DetectContentType heuristic misses:
// DOCTYPE-prefixed SVG, BOM-prefixed script, midstream tags, and bare
// HTML elements that browsers still execute when the file is served.
func TestProcessFileField_RejectsHiddenActiveContent(t *testing.T) {
	cases := map[string][]byte{
		// Finding 1: DOCTYPE svg prefix makes "<svg" no longer the leading token.
		"doctype-svg": []byte(`<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd"><svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`),
		// Finding 2: UTF-8 BOM before <script> defeats the prefix check.
		"bom-script": append([]byte{0xEF, 0xBB, 0xBF}, []byte("<script>alert(1)</script>")...),
		// Finding 2: bare HTML element DetectContentType reports as text/plain.
		"img-onerror": []byte("<img src=x onerror=alert(1)>"),
		// Finding 4: dangerous tag is not the leading token.
		"midstream-svg": []byte("x\n<svg onload=alert(1)>"),
		// Finding 4: UTF-16 BOM before <svg>.
		"utf16-svg": append([]byte{0xFF, 0xFE}, []byte("<svg onload=alert(1)>")...),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			store := &captureStorage{}
			if _, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(body), "x.svg", "posts", "attachment"); err == nil {
				t.Fatalf("SECURITY: [filefield] ProcessFileField accepted active content %q. Attack: stored markup executes script in a downstream renderer.", name)
			}
		})
	}
}

// TestProcessFileField_HardTokenRejected pins the polyglot defense. A HARD
// active-content token — <script, <svg, <iframe, <html, <!doctype, <object,
// <embed, <base — is rejected no matter where in the body it sits and no
// matter what bytes lead the file. The earlier scan only looked inside the
// 512-byte sniff window and skipped the token scan entirely for confirmed-
// inert binaries (raster/PDF/font magic), so a payload past the window or
// hidden behind valid image magic slipped through (the GIFAR polyglot
// class). HARD tokens now scan the whole body; SOFT tokens (<img, <?xml,
// <style, <link, javascript:) keep the old windowed, binary-skipped
// behavior — they genuinely appear in image metadata — and their
// acceptance is pinned by TestProcessFileField_AcceptsBinaryWithToken.
func TestProcessFileField_HardTokenRejected(t *testing.T) {
	t.Parallel()
	cases := map[string][]byte{
		// Shape 1: a hard token buried PAST the 512-byte sniff window.
		// The window saw only inert padding, so the old scan missed it.
		"past-sniff-window": append(bytes.Repeat([]byte("A"), 600), []byte("<script>alert(1)</script>")...),
		// Shape 2: a hard token behind valid raster magic (GIFAR). The
		// magic made the file sniff as a confirmed-inert binary, so the
		// old scan skipped it outright.
		"behind-raster-magic": []byte("GIF89a<svg onload=alert(1)>"),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &captureStorage{}
			if _, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(body), "payload.bin", "posts", "attachment"); err == nil {
				t.Fatalf("SECURITY: [filefield] ProcessFileField accepted a HARD active-content token (%s). Attack: a polyglot stores markup that executes when a host serves the bucket by extension-derived Content-Type.", name)
			}
		})
	}
}

// TestProcessFileField_AcceptsBinaryWithToken pins the property that SOFT
// active-content tokens (<img, <?xml, <style, <link, javascript:) embedded
// in confirmed-inert binaries — raster images, PDF, fonts — are NOT
// rejected. Those tokens genuinely appear in EXIF/XMP/comment metadata and
// PDF/font streams, so the scan checks the soft set only in the 512-byte
// sniff window and skips it entirely for confirmed-inert binaries.
//
// Rationale (why this was weakened): this test used to assert that
// GIF89a/PNG/JPEG/PDF magic plus "<script>alert(1)</script> javascript:
// <img src=x>" was ACCEPTED. That is no longer the correct behavior:
// <script is now a HARD token, scanned across the WHOLE body regardless of
// magic bytes (see TestProcessFileField_HardTokenRejected), so a confirmed
// binary carrying <script is rejected — closing the GIFAR polyglot hole
// where valid raster magic switched the token scan off. The property this
// test was protecting — "don't false-positive on a token in image
// metadata" — survives for the SOFT set, which is why it now uses soft
// tokens only. A plain raster with no token is included as a baseline.
func TestProcessFileField_AcceptsBinaryWithToken(t *testing.T) {
	t.Parallel()
	// Soft tokens only — these are the ones the binary skip still covers.
	token := []byte(`<?xml version="1.0"?> <style>.x{}</style> <link rel="x"> <img src="x"> javascript:foo()`)
	cases := map[string][]byte{
		// PNG magic + IDAT-ish bytes, with an embedded soft token (e.g. a
		// tEXt chunk or zTXt comment carrying user text / XMP).
		"png": append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0x0D, 'I', 'H', 'D', 'R'}, token...),
		// JPEG SOI + JFIF, token embedded in an EXIF/comment segment.
		"jpeg": append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}, token...),
		// PDF header, token in a stream/metadata.
		"pdf": append([]byte("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n"), token...),
		// GIF, token in a comment extension.
		"gif": append([]byte("GIF89a"), token...),
		// A plain raster with no token at all — baseline that a clean
		// image passes the sniff and the binary skip unchanged.
		"plain-png": append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0x0D, 'I', 'H', 'D', 'R'}, bytes.Repeat([]byte{0x00}, 64)...),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &captureStorage{}
			if _, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(body), "asset."+name, "posts", "attachment"); err != nil {
				t.Fatalf("SECURITY/correctness: [filefield] ProcessFileField rejected legitimate %s binary that merely contains a SOFT ASCII token: %v", name, err)
			}
		})
	}
}
