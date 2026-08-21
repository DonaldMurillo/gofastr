package static

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// nonSeekableFS wraps an fs.FS so its files expose ONLY the fs.File
// interface (Stat/Read/Close), no io.Seeker. fs.FS makes no seek
// promise, so this is a conforming filesystem, not a hostile one.
type nonSeekableFS struct{ inner fs.FS }

type nonSeekableFile struct{ inner fs.File }

func (f nonSeekableFile) Stat() (fs.FileInfo, error) { return f.inner.Stat() }
func (f nonSeekableFile) Read(p []byte) (int, error) { return f.inner.Read(p) }
func (f nonSeekableFile) Close() error               { return f.inner.Close() }

func (n nonSeekableFS) Open(name string) (fs.File, error) {
	f, err := n.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return nonSeekableFile{inner: f}, nil
}

// Hashing for the ETag consumes the file. When the handle cannot seek
// back, the body must still be served in full. An empty body under a
// correct Content-Length is a corrupt response, not a cache miss.
func TestServesFullBodyFromNonSeekableFS(t *testing.T) {
	const body = "complete body"
	base := fstest.MapFS{"app.js": &fstest.MapFile{Data: []byte(body)}}
	h := Handler(Config{FS: nonSeekableFS{inner: base}, Prefix: "/static"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))

	res := rec.Result()
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if string(got) != body {
		t.Fatalf("body = %q, want %q (Content-Length %q)",
			got, body, res.Header.Get("Content-Length"))
	}
}

// The second request hits the digest cache, so no hashing read happens
// and the seek is never needed. This must keep working too.
func TestNonSeekableFSSecondRequestOK(t *testing.T) {
	const body = "cached body"
	base := fstest.MapFS{"app.js": &fstest.MapFile{Data: []byte(body)}}
	h := Handler(Config{FS: nonSeekableFS{inner: base}, Prefix: "/static"})

	for i := range 2 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))
		res := rec.Result()
		got, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if string(got) != body {
			t.Fatalf("request %d: body = %q, want %q", i+1, got, body)
		}
	}
}
