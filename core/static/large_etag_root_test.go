package static

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A large file on disk is hashed in the background, and the goroutine
// used to open the file through the request's *os.Root, which the
// handler closes as soon as the response is done. A quick answer (a
// HEAD, a 304, a short range) closed it first, the open failed, and a
// file only ever reached that way never got an ETag: every revalidation
// a CDN or browser sends is exactly such a request.
func TestLargeDiskFileETagLandsAfterQuickAnswers(t *testing.T) {
	dir := t.TempDir()
	size := int64(largeFileETagThreshold + 1)
	f, err := os.Create(filepath.Join(dir, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f.Close()
	want, _ := hashETag(io.LimitReader(zeroReader{}, size))

	for _, tc := range []struct {
		name    string
		method  string
		headers map[string]string
	}{
		{"HEAD", http.MethodHead, nil},
		{"304", http.MethodGet, map[string]string{"If-Modified-Since": time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)}},
		{"range", http.MethodGet, map[string]string{"Range": "bytes=0-9"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := Handler(Config{FS: os.DirFS(dir)})
			// One quick answer starts the hash. Polling with more
			// requests would start a fresh one each time and let a
			// lucky schedule hide the race, so the check waits instead.
			serveWith(h, tc.method, "/big.bin", tc.headers)
			time.Sleep(time.Second)
			if etag := serveWith(h, http.MethodHead, "/big.bin", nil).Header().Get("ETag"); etag != want {
				t.Fatalf("ETag a second after one %s request: %q, want the content hash %s", tc.name, etag, want)
			}
		})
	}
}
