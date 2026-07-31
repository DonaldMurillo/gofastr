package static_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/static"
)

// TestStatic_ServesLargeFileUntruncated verifies that files larger than
// 32MB are served in full. Previously serveFile wrapped the file in
// io.LimitReader(f, 32<<20) and never compared against stat.Size(), so a
// >32MB file was served truncated as 200 with a Content-Length and ETag
// that were self-consistent with the (truncated) body — a silent data
// corruption.
//
// The body is verified by streaming it through a SHA-256 hasher so the
// 40MB content is never held in RAM.
func TestStatic_ServesLargeFileUntruncated(t *testing.T) {
	const size = 40 << 20 // 40 MiB > 32 MiB cap

	dir := t.TempDir()
	fpath := dir + "/big.bin"
	fout, err := os.Create(fpath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// 4 KiB patterned block, written size/4096 times. Patterned so a
	// truncation is detectable by hash, not held in RAM.
	block := make([]byte, 4096)
	for i := range block {
		block[i] = byte(i)
	}
	written := sha256.New()
	mw := io.MultiWriter(fout, written)
	for remaining := size; remaining > 0; {
		n := len(block)
		if remaining < n {
			n = remaining
		}
		if _, err := mw.Write(block[:n]); err != nil {
			fout.Close()
			t.Fatalf("write: %v", err)
		}
		remaining -= n
	}
	fout.Close()
	wantHash := hex.EncodeToString(written.Sum(nil))

	h := static.Handler(static.Config{FS: os.DirFS(dir)})
	req := httptest.NewRequest(http.MethodGet, "/big.bin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("SECURITY: [static] 40MiB file returned %d, want 200. Truncation served the wrong status or body.", rr.Code)
	}
	if cl := rr.Header().Get("Content-Length"); cl != "41943040" {
		t.Errorf("SECURITY: [static] Content-Length = %q, want 41943040. A truncated body with a self-consistent length is silent corruption.", cl)
	}
	got := sha256.Sum256(rr.Body.Bytes())
	if hex.EncodeToString(got[:]) != wantHash {
		t.Errorf("SECURITY: [static] 40MiB body hash mismatch (truncated). got %x…, want %s", got[:4], wantHash)
	}
}

// errorReadFile is an fs.File whose Read returns a non-ENOENT error,
// simulating an I/O fault mid-serve (e.g. a permission/decryption/read
// failure on the backing store).
type errorReadFile struct{}

func (errorReadFile) Stat() (fs.FileInfo, error) { return errFileInfo{}, nil }
func (errorReadFile) Read(p []byte) (int, error) { return 0, errors.New("simulated read failure") }
func (errorReadFile) Seek(off int64, whence int) (int64, error) {
	return 0, errors.New("simulated read failure")
}
func (errorReadFile) Close() error { return nil }

// errFileInfo backs errorReadFile with a plausible non-directory file.
type errFileInfo struct{}

func (errFileInfo) Name() string       { return "boom.bin" }
func (errFileInfo) Size() int64        { return 100 }
func (errFileInfo) Mode() fs.FileMode  { return 0o644 }
func (errFileInfo) ModTime() time.Time { return time.Time{} }
func (errFileInfo) IsDir() bool        { return false }
func (errFileInfo) Sys() any           { return nil }

type errorReadFS struct{}

func (errorReadFS) Open(name string) (fs.File, error) {
	if name == "boom.bin" {
		return errorReadFile{}, nil
	}
	return nil, fs.ErrNotExist
}

// TestStatic_ReadErrorMapsTo500 verifies that an I/O error during serving
// that is NOT fs.ErrNotExist yields a 500, not a 404. Previously every
// IO error (Open/Stat/Read) returned false from serveFile and the handler
// fell through to http.NotFound — masking server-side faults as 404s.
func TestStatic_ReadErrorMapsTo500(t *testing.T) {
	h := static.Handler(static.Config{FS: errorReadFS{}})
	req := httptest.NewRequest(http.MethodGet, "/boom.bin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("SECURITY: [static] non-ENOENT read error returned %d, want 500. IO faults must not be masked as 404.", rr.Code)
	}
}
