//go:build linux

package static

import (
	"bytes"
	"io/fs"
	"net/http"
	"syscall"
	"testing"
	"testing/fstest"
	"time"
)

func TestRewriteKeepingRevisionNewETag(t *testing.T) {
	f := &fstest.MapFile{
		Data:    bytes.Repeat([]byte("A"), 1000),
		ModTime: time.Unix(1_700_000_000, 0),
		Sys:     &syscall.Stat_t{Ino: 42, Ctim: syscall.NsecToTimespec(time.Now().UnixNano())},
	}
	h := Handler(Config{FS: fstest.MapFS{"clip.mp4": f}})
	before := serveWith(h, http.MethodGet, "/clip.mp4", nil).Header().Get("ETag")
	if before == "" {
		t.Fatal("initial response has no ETag")
	}
	f.Data = bytes.Repeat([]byte("B"), len(f.Data))
	rec := serveWith(h, http.MethodGet, "/clip.mp4", map[string]string{"Range": "bytes=500-", "If-Range": before})
	if after := rec.Header().Get("ETag"); after == "" || after == before {
		t.Errorf("ETag = %q, want a new content hash", after)
	}
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), f.Data) {
		t.Errorf("If-Range with old ETag: status %d, %d bytes, want 200 and all %d new bytes", rec.Code, rec.Body.Len(), len(f.Data))
	}
}

func TestLargeFileFutureCTimeGetsETag(t *testing.T) {
	f := &fstest.MapFile{
		Data: make([]byte, largeFileETagThreshold+1),
		Sys:  &syscall.Stat_t{Ino: 42, Ctim: syscall.NsecToTimespec(time.Now().Add(time.Hour).UnixNano())},
	}
	fsys := fstest.MapFS{"big.bin": f}
	c := newDigestCache()
	get := func() string {
		t.Helper()
		file, err := fsys.Open("big.bin")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		st, err := file.Stat()
		if err != nil {
			t.Fatal(err)
		}
		etag, _, err := fileETag(c, file, st, "big.bin", func() (fs.File, error) { return fsys.Open("big.bin") })
		if err != nil {
			t.Fatal(err)
		}
		return etag
	}
	get()
	waitIdle(t, c)
	want, err := hashETag(bytes.NewReader(f.Data))
	if err != nil {
		t.Fatal(err)
	}
	if got := get(); got != want {
		t.Fatalf("ETag = %q, want %s for future ctime", got, want)
	}
}
