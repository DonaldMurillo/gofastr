package upload

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// nopStorage is a Storage that consumes the reader and discards it.
type nopStorage struct{}

func (nopStorage) Save(_ context.Context, _ string, r io.Reader) error {
	_, err := io.Copy(io.Discard, r)
	return err
}
func (nopStorage) Delete(_ context.Context, _ string) error               { return nil }
func (nopStorage) Get(_ context.Context, _ string) (io.ReadCloser, error) { return nil, nil }
func (nopStorage) Exists(_ context.Context, _ string) (bool, error)       { return false, nil }

// multipartBody builds a multipart/form-data body with a single "file"
// part of the given size and returns the body plus its Content-Type.
func multipartBody(t *testing.T, fileSize int) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "big.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(bytes.Repeat([]byte("A"), fileSize)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

// TestHandler_BodyBoundedBeforeBuffer verifies the request body is
// capped at MaxSize before the whole multipart form is buffered/spilled
// to disk. Attack: POST a body far larger than MaxSize to force Go to
// spool gigabytes to os.TempDir before the 413 fires (resource DoS).
func TestHandler_BodyBoundedBeforeBuffer(t *testing.T) {
	const maxSize = 1024 // 1 KiB
	h := Handler(Config{MaxSize: maxSize, Storage: nopStorage{}})

	// File part is ~64 KiB, 64x the limit.
	body, ct := multipartBody(t, 64*1024)

	// Count how many body bytes the handler actually consumes. A
	// MaxBytesReader-wrapped body stops reading shortly past maxSize;
	// an unbounded ParseMultipartForm drains the whole thing.
	counted := &countingReader{r: body}
	req := httptest.NewRequest(http.MethodPost, "/upload", counted)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	h(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413; an oversized body must be rejected", rec.Code)
	}
	// The handler must not have drained the entire 64 KiB body. Allow
	// generous slack for multipart framing + the MaxBytesReader overshoot.
	if counted.n > maxSize+16*1024 {
		t.Errorf("SECURITY: [upload] handler consumed %d body bytes for a %d-byte limit. Attack: oversized multipart body spilled to disk before size check (DoS).", counted.n, maxSize)
	}
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// recordingStorage records the keys it was asked to save.
type recordingStorage struct{ saved []string }

func (s *recordingStorage) Save(_ context.Context, key string, r io.Reader) error {
	_, _ = io.Copy(io.Discard, r)
	s.saved = append(s.saved, key)
	return nil
}
func (s *recordingStorage) Delete(_ context.Context, _ string) error               { return nil }
func (s *recordingStorage) Get(_ context.Context, _ string) (io.ReadCloser, error) { return nil, nil }
func (s *recordingStorage) Exists(_ context.Context, _ string) (bool, error)       { return false, nil }

// TestHandler_RemovesSpilledTempFiles verifies the handler removes the
// multipart temp files it spills to disk before returning. Attack:
// repeatedly upload a part larger than the in-memory threshold so each
// request leaves an abandoned temp file, exhausting disk/inodes.
func TestHandler_RemovesSpilledTempFiles(t *testing.T) {
	// Point os.TempDir() at an isolated directory so we can count the
	// multipart spill files deterministically. multipart.Form spills to
	// os.CreateTemp("", ...), which honours $TMPDIR.
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	countTmp := func() int {
		entries, err := os.ReadDir(tmp)
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}

	// A small MaxSize forces the size-reject path. On the unfixed
	// handler, ParseMultipartForm(cfg.MaxSize) uses MaxSize as the
	// in-memory threshold, so a 256 KiB part spills to a temp file
	// *during parse* and is then abandoned when the 413 reject fires.
	const maxSize = 1024
	h := Handler(Config{MaxSize: maxSize, Storage: &recordingStorage{}})

	body, ct := multipartBody(t, 256*1024)
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", ct)

	before := countTmp()
	rec := httptest.NewRecorder()
	h(rec, req)

	// Either the body is bounded (rejected with 413) or parsed; on every
	// path the spilled temp file must not survive the handler return.
	after := countTmp()
	if after > before {
		t.Errorf("SECURITY: [upload] handler left %d spilled multipart temp file(s) on disk after returning. Attack: sustained uploads accumulate abandoned temp files, exhausting disk/inodes (DoS).", after-before)
	}
}
