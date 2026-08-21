package upload

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postNamedFile(t *testing.T, h http.HandlerFunc, name, content string) Metadata {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write([]byte(content))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload %q = %d: %s", name, rec.Code, rec.Body.String())
	}
	var meta Metadata
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	return meta
}

// TestSameFilenameDoesNotClobber pins the storage-key contract: two
// uploads of the same client filename must land at DIFFERENT keys, and
// the first upload's bytes must survive the second. Keying objects on
// the sanitized client filename (with LocalStorage.Save opening
// O_TRUNC) meant user B's report.txt silently overwrote user A's:
// cross-user data loss with no error anywhere. The auto-CRUD path
// already solves this with a timestamp + crypto/rand suffix
// (file.GenerateFilePath); the standalone handler must use the same
// unique-name generation, not a second scheme.
func TestSameFilenameDoesNotClobber(t *testing.T) {
	dir := tmpDir(t)
	store := NewLocalStorage(dir)
	h := Handler(Config{Storage: store})

	m1 := postNamedFile(t, h, "report.txt", "USER-A-SECRET-CONTENT")
	m2 := postNamedFile(t, h, "report.txt", "USER-B-CONTENT")

	if m1.Key == m2.Key {
		t.Fatalf("two uploads of %q produced the same key %q — second silently overwrites first (O_TRUNC clobber)", "report.txt", m1.Key)
	}

	got, err := store.Get(t.Context(), m1.Key)
	if err != nil {
		t.Fatalf("get first upload: %v", err)
	}
	data, err := io.ReadAll(got)
	if err != nil {
		t.Fatalf("read first upload: %v", err)
	}
	_ = got.Close()
	if string(data) != "USER-A-SECRET-CONTENT" {
		t.Fatalf("first upload's bytes were clobbered: got %q", string(data))
	}

	// The key keeps the sanitized base name (minus extension) so storage
	// listings stay readable, and carries the extension through.
	if !strings.HasPrefix(m1.Key, "report_") || !strings.HasSuffix(m1.Key, ".txt") {
		t.Errorf("key %q lost the sanitized name/extension", m1.Key)
	}
}
