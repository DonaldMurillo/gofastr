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
