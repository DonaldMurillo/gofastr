package upload

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// streamedUpload builds a multipart body with one "file" part of size
// bytes without holding the part in memory.
func streamedUpload(t *testing.T, size int64) (io.Reader, string) {
	t.Helper()
	var head bytes.Buffer
	w := multipart.NewWriter(&head)
	if _, err := w.CreateFormFile("file", "big.bin"); err != nil {
		t.Fatal(err)
	}
	tail := "\r\n--" + w.Boundary() + "--\r\n"
	fill := io.LimitReader(repeatByte('A'), size)
	return io.MultiReader(&head, fill, strings.NewReader(tail)), w.FormDataContentType()
}

type repeatByte byte

func (b repeatByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}

// Config.MaxSize 0 meant "no limit": the handler skipped MaxBytesReader
// and ParseMultipartForm spooled whatever the client sent to the temp
// directory before anything checked it. A zero value now means
// DefaultMaxSize; a larger limit is an explicit MaxSize.
func TestUploadZeroMaxSizeHasDefaultCap(t *testing.T) {
	h := Handler(Config{Storage: nopStorage{}})
	body, ct := streamedUpload(t, DefaultMaxSize+(1<<20))
	counted := &countingReader{r: body}
	req := httptest.NewRequest(http.MethodPost, "/upload", counted)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	h(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 for a body past DefaultMaxSize with MaxSize 0", rec.Code)
	}
	if int64(counted.n) > DefaultMaxSize+64<<10 {
		t.Fatalf("handler read %d bytes; the default cap must stop it near %d", counted.n, DefaultMaxSize)
	}
}
