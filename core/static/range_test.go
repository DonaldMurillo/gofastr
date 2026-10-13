package static

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"testing/fstest"
	"time"
)

// rangeBody is 1000 patterned bytes, so a wrong offset shows up in the body.
func rangeBody() []byte {
	b := make([]byte, 1000)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

// rangeHandlers serves rangeBody as clip.mp4 from a disk FS (the *os.Root
// branch) and from an in-memory FS (the plain fs.FS branch).
func rangeHandlers(t *testing.T) map[string]http.Handler {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), rangeBody(), 0o600); err != nil {
		t.Fatal(err)
	}
	mem := fstest.MapFS{"clip.mp4": &fstest.MapFile{Data: rangeBody(), ModTime: time.Unix(1_700_000_000, 0)}}
	return map[string]http.Handler{
		"disk":   Handler(Config{FS: os.DirFS(dir)}),
		"memory": Handler(Config{FS: mem}),
	}
}

func serveWith(h http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRangeAnswers206(t *testing.T) {
	body := rangeBody()
	for name, h := range rangeHandlers(t) {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				rng, contentRange string
				want              []byte
			}{
				{"bytes=0-99", "bytes 0-99/1000", body[:100]},
				{"bytes=900-", "bytes 900-999/1000", body[900:]},
				{"bytes=-10", "bytes 990-999/1000", body[990:]},
			} {
				rec := serveWith(h, http.MethodGet, "/clip.mp4", map[string]string{"Range": tc.rng})
				if rec.Code != http.StatusPartialContent {
					t.Fatalf("Range %s: status %d, want 206", tc.rng, rec.Code)
				}
				if got := rec.Header().Get("Content-Range"); got != tc.contentRange {
					t.Errorf("Range %s: Content-Range %q, want %q", tc.rng, got, tc.contentRange)
				}
				if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(tc.want)) {
					t.Errorf("Range %s: Content-Length %q, want %d", tc.rng, got, len(tc.want))
				}
				if !bytes.Equal(rec.Body.Bytes(), tc.want) {
					t.Errorf("Range %s: body is not the requested slice", tc.rng)
				}
				if got := rec.Header().Get("Content-Type"); got != "video/mp4" {
					t.Errorf("Range %s: Content-Type %q, want video/mp4", tc.rng, got)
				}
				if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Errorf("Range %s: nosniff missing on a 206", tc.rng)
				}
			}
		})
	}
}

func TestFullGETAdvertisesRanges(t *testing.T) {
	for name, h := range rangeHandlers(t) {
		t.Run(name, func(t *testing.T) {
			rec := serveWith(h, http.MethodGet, "/clip.mp4", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
				t.Errorf("Accept-Ranges %q, want bytes", got)
			}
			if got := rec.Header().Get("Content-Length"); got != "1000" {
				t.Errorf("Content-Length %q, want 1000", got)
			}
			if !bytes.Equal(rec.Body.Bytes(), rangeBody()) {
				t.Error("full body differs from the file")
			}
		})
	}
}

func TestUnsatisfiableRangeIs416(t *testing.T) {
	for name, h := range rangeHandlers(t) {
		t.Run(name, func(t *testing.T) {
			rec := serveWith(h, http.MethodGet, "/clip.mp4", map[string]string{"Range": "bytes=5000-6000"})
			if rec.Code != http.StatusRequestedRangeNotSatisfiable {
				t.Fatalf("status %d, want 416", rec.Code)
			}
			if got := rec.Header().Get("Content-Range"); got != "bytes */1000" {
				t.Errorf("Content-Range %q, want bytes */1000", got)
			}
		})
	}
}

func TestMultiRangeIsMultipart(t *testing.T) {
	for name, h := range rangeHandlers(t) {
		t.Run(name, func(t *testing.T) {
			rec := serveWith(h, http.MethodGet, "/clip.mp4", map[string]string{"Range": "bytes=0-9,100-109"})
			if rec.Code != http.StatusPartialContent {
				t.Fatalf("status %d, want 206", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !regexp.MustCompile(`^multipart/byteranges; boundary=`).MatchString(ct) {
				t.Errorf("Content-Type %q, want multipart/byteranges", ct)
			}
		})
	}
}

// If-Range resumes a download only while the file is unchanged: a stale
// validator gets the whole file, never a slice of a different revision.
func TestIfRangeResumesOnlyUnchanged(t *testing.T) {
	for name, h := range rangeHandlers(t) {
		t.Run(name, func(t *testing.T) {
			etag := serveWith(h, http.MethodGet, "/clip.mp4", nil).Header().Get("ETag")
			if etag == "" {
				t.Fatal("no ETag on the full response")
			}
			rec := serveWith(h, http.MethodGet, "/clip.mp4", map[string]string{"Range": "bytes=500-", "If-Range": etag})
			if rec.Code != http.StatusPartialContent {
				t.Errorf("current If-Range: status %d, want 206", rec.Code)
			}
			rec = serveWith(h, http.MethodGet, "/clip.mp4", map[string]string{"Range": "bytes=500-", "If-Range": `"stale"`})
			if rec.Code != http.StatusOK || rec.Body.Len() != 1000 {
				t.Errorf("stale If-Range: status %d with %d bytes, want 200 with 1000", rec.Code, rec.Body.Len())
			}
		})
	}
}

func TestHeadRangeHasNoBody(t *testing.T) {
	for name, h := range rangeHandlers(t) {
		t.Run(name, func(t *testing.T) {
			rec := serveWith(h, http.MethodHead, "/clip.mp4", nil)
			if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
				t.Errorf("HEAD: status %d with %d body bytes, want 200 and none", rec.Code, rec.Body.Len())
			}
			if got := rec.Header().Get("Content-Length"); got != "1000" {
				t.Errorf("HEAD: Content-Length %q, want 1000", got)
			}
		})
	}
}

// A conditional GET that matches still answers 304 before any range logic.
func TestRangeWithMatchingETagIs304(t *testing.T) {
	for name, h := range rangeHandlers(t) {
		t.Run(name, func(t *testing.T) {
			etag := serveWith(h, http.MethodGet, "/clip.mp4", nil).Header().Get("ETag")
			rec := serveWith(h, http.MethodGet, "/clip.mp4", map[string]string{"Range": "bytes=0-9", "If-None-Match": etag})
			if rec.Code != http.StatusNotModified {
				t.Errorf("status %d, want 304", rec.Code)
			}
		})
	}
}

// fs.FS promises no seeking. A handle that cannot seek keeps the full-body
// answer and must not advertise ranges it cannot serve.
func TestNonSeekableIgnoresRange(t *testing.T) {
	base := fstest.MapFS{"clip.mp4": &fstest.MapFile{Data: rangeBody()}}
	h := Handler(Config{FS: nonSeekableFS{inner: base}})
	rec := serveWith(h, http.MethodGet, "/clip.mp4", map[string]string{"Range": "bytes=0-99"})
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), rangeBody()) {
		t.Fatalf("status %d with %d bytes, want 200 with the full 1000", rec.Code, rec.Body.Len())
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "" {
		t.Errorf("Accept-Ranges %q on a non-seekable handle, want none", got)
	}
}

// The 404 page is not the resource the client asked for, so a Range or
// a precondition aimed at the missing URL must not slice or 412 it.
func TestNotFoundPageIgnoresRange(t *testing.T) {
	fsys := fstest.MapFS{"404.html": &fstest.MapFile{Data: []byte("<h1>gone</h1>")}}
	h := Handler(Config{FS: fsys, NotFoundFile: "404.html"})
	for _, hdr := range []map[string]string{
		{"Range": "bytes=0-3"},
		{"If-Match": `"nope"`},
		{"If-Unmodified-Since": "Mon, 02 Jan 2006 15:04:05 GMT"},
	} {
		rec := serveWith(h, http.MethodGet, "/missing.mp4", hdr)
		if rec.Code != http.StatusNotFound || rec.Body.String() != "<h1>gone</h1>" {
			t.Errorf("%v: status %d body %q, want 404 with the full page", hdr, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Accept-Ranges"); got != "" {
			t.Errorf("%v: Accept-Ranges %q on the 404 page, want none", hdr, got)
		}
	}
}

// bigFile is a seekable synthetic file of any size that counts the bytes
// read from it, so a test can tell whether the ETag hashed the file.
type bigFile struct {
	size, off int64
	modTime   time.Time
	read      *int64
}

type bigInfo struct {
	size    int64
	modTime time.Time
}

func (i bigInfo) Name() string       { return "big.mp4" }
func (i bigInfo) Size() int64        { return i.size }
func (i bigInfo) Mode() fs.FileMode  { return 0o644 }
func (i bigInfo) ModTime() time.Time { return i.modTime }
func (i bigInfo) IsDir() bool        { return false }
func (i bigInfo) Sys() any           { return nil }

func (f *bigFile) Stat() (fs.FileInfo, error) { return bigInfo{f.size, f.modTime}, nil }
func (f *bigFile) Close() error               { return nil }
func (f *bigFile) Read(p []byte) (int, error) {
	if f.off >= f.size {
		return 0, io.EOF
	}
	n := int64(len(p))
	if rest := f.size - f.off; n > rest {
		n = rest
	}
	clear(p[:n])
	f.off += n
	*f.read += n
	return int(n), nil
}

func (f *bigFile) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.off = off
	case io.SeekCurrent:
		f.off += off
	case io.SeekEnd:
		f.off = f.size + off
	}
	return f.off, nil
}

type bigFS struct {
	size    int64
	modTime time.Time
	read    *int64
}

func (b bigFS) Open(name string) (fs.File, error) {
	if name != "big.mp4" {
		return nil, fs.ErrNotExist
	}
	return &bigFile{size: b.size, modTime: b.modTime, read: b.read}, nil
}

// Hashing a multi-GB video for its ETag reads the whole file before the
// first byte goes out. Above the threshold a file with a real modtime gets
// a validator from its size and modtime instead, and the validator stays
// strong so If-Range can still resume a download.
func TestLargeFileETagSkipsHash(t *testing.T) {
	var read int64
	mod := time.Unix(1_700_000_000, 123)
	h := Handler(Config{FS: bigFS{size: 4 << 30, modTime: mod, read: &read}})

	rec := serveWith(h, http.MethodGet, "/big.mp4", map[string]string{"Range": "bytes=0-99"})
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status %d, want 206", rec.Code)
	}
	if read > 100 {
		t.Errorf("read %d bytes to answer a 100-byte range; the ETag hashed the file", read)
	}
	etag := rec.Header().Get("ETag")
	want := `"` + strconv.FormatInt(4<<30, 16) + "-" + strconv.FormatInt(mod.UnixNano(), 16) + `"`
	if etag != want {
		t.Errorf("ETag %s, want %s", etag, want)
	}

	rec = serveWith(h, http.MethodGet, "/big.mp4", map[string]string{"Range": "bytes=10-19", "If-Range": etag})
	if rec.Code != http.StatusPartialContent {
		t.Errorf("If-Range with the size-modtime ETag: status %d, want 206", rec.Code)
	}
}

// A file with no usable modtime (embed.FS reports zero; reproducible
// builds stamp the epoch) has nothing but its content to tell revisions
// apart, so it keeps the content hash at any size.
func TestLargeFileWithoutModTimeHashes(t *testing.T) {
	for _, mod := range []time.Time{{}, time.Unix(0, 0), time.Unix(1, 0)} {
		var read int64
		size := int64(largeFileETagThreshold + 1)
		h := Handler(Config{FS: bigFS{size: size, modTime: mod, read: &read}})
		rec := serveWith(h, http.MethodHead, "/big.mp4", nil)
		if !regexp.MustCompile(`^"[0-9a-f]{32}"$`).MatchString(rec.Header().Get("ETag")) {
			t.Errorf("modtime %v: ETag %s, want the content hash", mod, rec.Header().Get("ETag"))
		}
		if read != size {
			t.Errorf("modtime %v: read %d bytes, want the whole file (%d)", mod, read, size)
		}
	}
}

// At or below the threshold the content hash stays, so a redeploy that
// resets modtimes keeps validating unchanged files.
func TestSmallFileKeepsContentHash(t *testing.T) {
	for name, h := range rangeHandlers(t) {
		t.Run(name, func(t *testing.T) {
			etag := serveWith(h, http.MethodGet, "/clip.mp4", nil).Header().Get("ETag")
			if !regexp.MustCompile(`^"[0-9a-f]{32}"$`).MatchString(etag) {
				t.Errorf("ETag %s, want the content hash", etag)
			}
		})
	}
}
