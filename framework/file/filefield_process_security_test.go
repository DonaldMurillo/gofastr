package file_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
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
// active-content token, <script, <svg, <iframe, <html, <!doctype, <object,
// <embed, <base, is rejected no matter where in the body it sits and no
// matter what bytes lead the file. The earlier scan only looked inside the
// 512-byte sniff window and skipped the token scan entirely for confirmed-
// inert binaries (raster/PDF/font magic), so a payload past the window or
// hidden behind valid image magic slipped through (the GIFAR polyglot
// class). HARD tokens now scan the whole body; SOFT tokens (<img, <?xml,
// <style, <link, javascript:) keep the old windowed, binary-skipped
// behavior, they genuinely appear in image metadata, and their
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
// in confirmed-inert binaries, such as raster images, PDF, fonts, are NOT
// rejected. Those tokens genuinely appear in EXIF/XMP/comment metadata and
// PDF/font streams, so the scan checks the soft set only in the 512-byte
// sniff window and skips it entirely for confirmed-inert binaries.
//
// Rationale (why this was weakened): this test used to assert that
// GIF89a/PNG/JPEG/PDF magic plus "<script>alert(1)</script> javascript:
// <img src=x>" was ACCEPTED. That is no longer the correct behavior:
// <script is now a HARD token, scanned across the WHOLE body regardless of
// magic bytes (see TestProcessFileField_HardTokenRejected), so a confirmed
// binary carrying <script is rejected, closing the GIFAR polyglot hole
// where valid raster magic switched the token scan off. The property this
// test was protecting. "don't false-positive on a token in image
// metadata", survives for the SOFT set, which is why it now uses soft
// tokens only. A plain raster with no token is included as a baseline.
func TestProcessFileField_AcceptsBinaryWithToken(t *testing.T) {
	t.Parallel()
	// Soft tokens only, these are the ones the binary skip still covers.
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
		// A plain raster with no token at all, baseline that a clean
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

// deriveError is the stub deriver's failure, standing in for
// imagefield.DeriveImage's designed reject path ("surfacing it fails the
// upload, which is the point") when non-decodable bytes land on an Image
// field, and for a mid-variant encode failure.
type deriveError struct{}

func (deriveError) Error() string { return "stub deriver: decoding upload failed" }

// deleteLedgerStorage records every Save and Delete and reports Exists
// truthfully, so a test can assert whether bytes written before a failure
// were compensated by a delete.
type deleteLedgerStorage struct {
	saved   []string
	deleted []string
}

func (s *deleteLedgerStorage) Save(_ context.Context, key string, _ io.Reader) error {
	s.saved = append(s.saved, key)
	return nil
}

func (s *deleteLedgerStorage) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return nil
}

func (s *deleteLedgerStorage) Get(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (s *deleteLedgerStorage) Exists(_ context.Context, key string) (bool, error) {
	for _, k := range s.deleted {
		if k == key {
			return false, nil
		}
	}
	for _, k := range s.saved {
		if k == key {
			return true, nil
		}
	}
	return false, nil
}

var _ upload.Storage = (*deleteLedgerStorage)(nil)

// failingDeriver rejects the upload after ProcessFileField has already
// saved the primary (filefield.go:288 saves, :302-306 derives).
type failingDeriver struct{}

func (failingDeriver) DeriveImage(context.Context, upload.Storage, []byte, string) (*file.ImageDerivatives, error) {
	return nil, deriveError{}
}

// partialDeriver saves one rendition through the store and THEN fails,
// mirroring imagefield.DeriveImage's incremental variant loop
// (imagefield.go:160-168 saves renditions one at a time inside ProcessTo;
// a failure on variant k leaves variants 1..k-1 plus the primary stored).
type partialDeriver struct{}

func (partialDeriver) DeriveImage(ctx context.Context, store upload.Storage, _ []byte, primaryRef string) (*file.ImageDerivatives, error) {
	if err := store.Save(ctx, primaryRef+"_sm.webp", bytes.NewReader([]byte("rendition"))); err != nil {
		return nil, err
	}
	return nil, deriveError{}
}

// okDeriver succeeds so the success path can be pinned as the control.
type okDeriver struct{}

func (okDeriver) DeriveImage(context.Context, upload.Storage, []byte, string) (*file.ImageDerivatives, error) {
	return &file.ImageDerivatives{}, nil
}

// Property (CHAIN8-R4): an upload ProcessFileField REJECTS must not
// remain in storage. The primary is saved at filefield.go:288 BEFORE the
// deriver runs at :302-306, and every error return after the save reaches
// no compensating store.Delete, so rejected bytes persist forever,
// referenced by no row and included in no error response. An attacker
// repeats ~32 MiB inert non-image posts against an Image field: each
// request fails (decode rejection is the designed path) AND stores the
// full object — unbounded disk consumption invisible to any DB-driven
// cleanup.
func TestProcessFileFieldDeriveFailureOrphans(t *testing.T) {
	// Inert binary payload (raster magic + zeros) that passes
	// rejectUnsafeContent; rejection then comes only from the deriver.
	payload := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x00}, 512)...)

	remaining := func(s *deleteLedgerStorage) []string {
		var out []string
		for _, k := range s.saved {
			deleted := false
			for _, d := range s.deleted {
				if d == k {
					deleted = true
				}
			}
			if !deleted {
				out = append(out, k)
			}
		}
		return out
	}

	t.Run("derive failure must not orphan the saved primary", func(t *testing.T) {
		store := &deleteLedgerStorage{}
		_, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(payload), "photo.png", "users", "avatar", file.WithImageDeriver(failingDeriver{}))
		if err == nil {
			t.Fatal("control: a failing deriver must still fail the upload")
		}
		if orphans := remaining(store); len(orphans) > 0 {
			t.Fatalf("SECURITY: [filefield-orphan] upload was rejected (err=%v) but its bytes persist in storage with no compensating delete: keys %v remain (saved=%v deleted=%v). Attack: repeat inert non-image posts against an Image field — every request fails AND stores the full object; unbounded storage growth invisible to row-driven cleanup (store.Save at filefield.go:288 precedes the deriver at :302-306 with no store.Delete on any later error path).", err, orphans, store.saved, store.deleted)
		}
	})

	t.Run("mid-variant derive failure must not orphan primary and saved renditions", func(t *testing.T) {
		store := &deleteLedgerStorage{}
		_, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(payload), "photo.png", "users", "avatar", file.WithImageDeriver(partialDeriver{}))
		if err == nil {
			t.Fatal("control: a deriver failing after saving a rendition must still fail the upload")
		}
		if orphans := remaining(store); len(orphans) > 0 {
			t.Fatalf("SECURITY: [filefield-orphan] upload rejected mid-derivation (err=%v) but the primary AND already-saved rendition persist: keys %v remain (saved=%v deleted=%v). Attack: each rejected request leaves a full-size object plus partial renditions on disk, referenced by no row (variant loop saves incrementally, imagefield.go:160-168; no store.Delete on the error path).", err, orphans, store.saved, store.deleted)
		}
	})

	// Control: compensation must fire only on failure — a successful
	// process keeps exactly the stored references and deletes nothing.
	t.Run("control success keeps stored bytes", func(t *testing.T) {
		store := &deleteLedgerStorage{}
		ff, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(payload), "photo.png", "users", "avatar", file.WithImageDeriver(okDeriver{}))
		if err != nil {
			t.Fatalf("control: successful process failed: %v", err)
		}
		if len(store.saved) != 1 || store.saved[0] != ff.StorageRef || len(store.deleted) != 0 {
			t.Fatalf("control: success path must keep exactly the primary (saved=%v deleted=%v storageRef=%q)", store.saved, store.deleted, ff.StorageRef)
		}
	})
}

// failAfterReader yields ok bytes, then a read error: a client that
// hangs up (or a proxy that resets) partway through the body.
type failAfterReader struct {
	data []byte
	off  int
	done bool
}

func (r *failAfterReader) Read(p []byte) (int, error) {
	if r.off < len(r.data) {
		n := copy(p, r.data[r.off:])
		r.off += n
		return n, nil
	}
	if !r.done {
		r.done = true
		return 0, errors.New("connection reset by peer")
	}
	return 0, errors.New("connection reset by peer")
}

// TestProcessFileFieldReadErrorStoresNothing: a body whose read fails
// mid-stream must surface the error and never call store.Save. A
// partial object persisted under a generated key is unreferenced by
// any row and invisible to row-driven cleanup — the same orphan shape
// TestProcessFileFieldDeriveFailureOrphans pins for derive failures.
func TestProcessFileFieldReadErrorStoresNothing(t *testing.T) {
	store := &captureStorage{}
	body := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x00}, 512)...)
	r := &failAfterReader{data: body}

	if _, err := file.ProcessFileField(context.Background(), store, r, "half.png", "posts", "attachment"); err == nil {
		t.Fatal("a mid-body read error must fail the upload")
	}
	if store.key != "" {
		t.Fatalf("partial upload was saved to storage key %q after a read error; nothing may persist", store.key)
	}
}

// TestProcessFileFieldCapBoundary: MaxProcessFileSize is a boundary,
// not an off-by-one. Exactly-at-cap bodies are accepted (the cap is
// inclusive) and one byte past is refused — without buffering the rest
// of the stream (pinned elsewhere as "without buffering the rest of
// the body"; here only the accept/reject edge is asserted).
func TestProcessFileFieldCapBoundary(t *testing.T) {
	atCap := &captureStorage{}
	body := bytes.Repeat([]byte{0x61}, int(file.MaxProcessFileSize))
	ff, err := file.ProcessFileField(context.Background(), atCap, bytes.NewReader(body), "big.txt", "posts", "attachment")
	if err != nil {
		t.Fatalf("exactly-at-cap upload rejected: %v", err)
	}
	if ff.Size != file.MaxProcessFileSize {
		t.Fatalf("at-cap Size = %d, want %d", ff.Size, file.MaxProcessFileSize)
	}

	overCap := &captureStorage{}
	body = append(body, 0x61)
	if _, err := file.ProcessFileField(context.Background(), overCap, bytes.NewReader(body), "big2.txt", "posts", "attachment"); err == nil {
		t.Fatal("one-past-cap upload accepted")
	}
	if overCap.key != "" {
		t.Fatalf("over-cap upload was stored under %q before rejection", overCap.key)
	}
}

// TestGenerateFilePathNeverEscapesUploads: the storage key built from a
// client-controlled multipart filename must always stay a relative
// path under the uploads/ root — no traversal segment, no absolute
// path, no backslash, no control byte. This is the key-construction
// half of the traversal property whose validation half
// TestFileFieldRejectsFilenameTraversal pins.
func TestGenerateFilePathNeverEscapesUploads(t *testing.T) {
	hostile := []string{
		"../../../etc/passwd",
		"..\\..\\..\\windows\\system32\\evil.dll",
		"con\r\ntrol\x00name.png",
		"..",
		".",
		"",
		"/absolute/path.png",
		"C:\\absolute\\win.png",
		"inner..dots.png",
	}
	for _, name := range hostile {
		key := file.GenerateFilePath("posts", "attachment", name)
		if key == "" {
			t.Fatalf("GenerateFilePath(%q) returned an empty key", name)
		}
		if strings.Contains(key, "..") {
			t.Errorf("key for %q contains a dot-pair segment: %q", name, key)
		}
		if strings.HasPrefix(key, "/") || strings.HasSuffix(key, "/") {
			t.Errorf("key for %q is not a safe relative path: %q", name, key)
		}
		if strings.Contains(key, "\\") {
			t.Errorf("key for %q contains a backslash: %q", name, key)
		}
		for i := range len(key) {
			if key[i] < 0x20 || key[i] == 0x7F {
				t.Errorf("key for %q contains control byte 0x%02X: %q", name, key[i], key)
				break
			}
		}
	}
}
