package static

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

type invalidFS struct{ fstest.MapFS }

func (v invalidFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return v.MapFS.Open(name)
}

func TestSweepInvalidPathIsAMiss(t *testing.T) {
	fsys := invalidFS{fstest.MapFS{"index.html": {Data: []byte("shell")}}}
	for _, spa := range []bool{false, true} {
		h := Handler(Config{FS: fsys, SPA: spa})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = "/\xff"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		want := http.StatusNotFound
		if spa {
			want = http.StatusOK
		}
		if rec.Code != want {
			t.Errorf("SPA=%v: /\\xff answered %d, want %d", spa, rec.Code, want)
		}
	}
}

func TestSweepFallbackOpenRefusesSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "leak.txt")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{FS: os.DirFS(root), resolvedRoot: resolved, digests: newDigestCache()}.defaults()
	rec := httptest.NewRecorder()
	if !serveFile(rec, httptest.NewRequest(http.MethodGet, "/leak.txt", nil), cfg, "leak.txt") || rec.Code != http.StatusNotFound {
		t.Fatalf("a symlink out of the root on the fallback open answered %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatal("the escaped file's bytes were served")
	}
}

type panicFS struct{ fs.FS }

func (p panicFS) Open(name string) (fs.File, error) {
	if name == "big.bin" {
		if f, err := p.FS.Open(name); err == nil {
			return panicFile{f}, nil
		}
	}
	return p.FS.Open(name)
}

type panicFile struct{ fs.File }

func (panicFile) Read([]byte) (int, error) { panic("read") }

type closePanicFile struct{ fs.File }

func (closePanicFile) Close() error { panic("close") }

func TestSweepBackgroundHashPanicCostsOnlyTheETag(t *testing.T) {
	c := newDigestCache()
	data := bytes.Repeat([]byte("x"), 8)
	base := fstest.MapFS{"big.bin": {Data: data}}
	st, err := fs.Stat(base, "big.bin")
	if err != nil {
		t.Fatal(err)
	}
	key := keyOf("big.bin", st)
	c.hashInBackground(key, func() (fs.File, error) { return panicFS{base}.Open("big.bin") })
	c.hashInBackground(digestKey{name: "boom"}, func() (fs.File, error) { panic("open") })
	closeKey := digestKey{name: "close"}
	c.hashInBackground(closeKey, func() (fs.File, error) {
		f, err := base.Open("big.bin")
		return closePanicFile{f}, err
	})
	deadline := time.Now().Add(2 * time.Second)
	for len(c.slots) > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(c.slots) != 0 {
		t.Fatal("a panicking hash kept its slot")
	}
	if _, ok := c.done.Load(key); ok {
		t.Fatal("a panicking read stored a digest")
	}
	for _, k := range []digestKey{key, closeKey} {
		for time.Now().Before(deadline) {
			if _, running := c.inflight.Load(k); !running {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if _, running := c.inflight.Load(k); running {
			t.Fatalf("a panicking hash kept its inflight key %q", k.name)
		}
	}
	c.hashInBackground(key, func() (fs.File, error) { return base.Open("big.bin") })
	for time.Now().Before(deadline) {
		if _, ok := c.done.Load(key); ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the same revision could not be hashed after a panic")
}
