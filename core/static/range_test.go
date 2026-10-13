package static

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync"
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

// RFC 9110 §14.2 defines Range for GET only; a HEAD ignores it and
// describes the full file.
func TestHeadIgnoresRange(t *testing.T) {
	for name, h := range rangeHandlers(t) {
		t.Run(name, func(t *testing.T) {
			for _, hdr := range []map[string]string{nil, {"Range": "bytes=0-99"}} {
				rec := serveWith(h, http.MethodHead, "/clip.mp4", hdr)
				if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
					t.Errorf("HEAD %v: status %d with %d body bytes, want 200 and none", hdr, rec.Code, rec.Body.Len())
				}
				if got := rec.Header().Get("Content-Length"); got != "1000" {
					t.Errorf("HEAD %v: Content-Length %q, want 1000", hdr, got)
				}
				if got := rec.Header().Get("Content-Range"); got != "" {
					t.Errorf("HEAD %v: Content-Range %q, want none", hdr, got)
				}
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

// bigFile is a seekable synthetic file of zeros that counts the bytes
// read from it. A handle that reads before it ever seeks is the
// background hash (ServeContent seeks first), and it waits on hold.
type bigFile struct {
	size, off int64
	modTime   time.Time
	read      int64
	hold      chan struct{}
	seeked    bool
	// laterMod, when set, becomes the modtime once the last byte is read:
	// a file rewritten while it was being hashed.
	laterMod time.Time
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
	if f.hold != nil && !f.seeked {
		<-f.hold
	}
	if f.off >= f.size {
		if !f.laterMod.IsZero() {
			f.modTime = f.laterMod
		}
		return 0, io.EOF
	}
	n := min(int64(len(p)), f.size-f.off)
	clear(p[:n])
	f.off += n
	f.read += n
	return int(n), nil
}

func (f *bigFile) Seek(off int64, whence int) (int64, error) {
	f.seeked = true
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
	hold    chan struct{}
	mu      *sync.Mutex
	opened  *[]*bigFile
}

func newBigFS(size int64, mod time.Time) bigFS {
	return bigFS{size: size, modTime: mod, hold: make(chan struct{}), mu: &sync.Mutex{}, opened: &[]*bigFile{}}
}

func (b bigFS) Open(name string) (fs.File, error) {
	if name != "big.mp4" {
		return nil, fs.ErrNotExist
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	f := &bigFile{size: b.size, modTime: b.modTime, hold: b.hold}
	*b.opened = append(*b.opened, f)
	return f, nil
}

// Hashing a multi-GB video for its ETag read the whole file before the
// first byte went out. A large file now answers at once with no ETag and
// is hashed off the request path; later requests carry the content hash.
func TestLargeFileHashesOffRequest(t *testing.T) {
	size := int64(largeFileETagThreshold + 1)
	fsys := newBigFS(size, time.Unix(1_700_000_000, 0))
	h := Handler(Config{FS: fsys})

	rec := serveWith(h, http.MethodGet, "/big.mp4", map[string]string{"Range": "bytes=0-99"})
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status %d, want 206", rec.Code)
	}
	fsys.mu.Lock()
	first := (*fsys.opened)[0]
	fsys.mu.Unlock()
	if got := first.read; got > 100 {
		t.Errorf("request read %d bytes to answer a 100-byte range; the ETag hashed the file inline", got)
	}
	if got := rec.Header().Get("ETag"); got != "" {
		t.Errorf("ETag %s before the background hash finished, want none", got)
	}
	// With no ETag yet, an If-Range ETag cannot be confirmed: whole file.
	rec = serveWith(h, http.MethodGet, "/big.mp4", map[string]string{"Range": "bytes=0-9", "If-Range": `"guess"`})
	if rec.Code != http.StatusOK {
		t.Errorf("If-Range while hashing: status %d, want 200", rec.Code)
	}

	close(fsys.hold)
	want, _ := hashETag(io.LimitReader(zeroReader{}, size))
	var etag string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if etag = serveWith(h, http.MethodHead, "/big.mp4", nil).Header().Get("ETag"); etag != "" {
			break
		}
	}
	if etag != want {
		t.Fatalf("ETag after the background hash: %q, want the content hash %s", etag, want)
	}
	rec = serveWith(h, http.MethodGet, "/big.mp4", map[string]string{"Range": "bytes=10-19", "If-Range": etag})
	if rec.Code != http.StatusPartialContent {
		t.Errorf("If-Range with the content hash: status %d, want 206", rec.Code)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

// waitIdle waits for every background hash on c to finish.
func waitIdle(t *testing.T, c *digestCache) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		busy := false
		c.inflight.Range(func(any, any) bool { busy = true; return false })
		if !busy {
			return
		}
	}
	t.Fatal("background hash still running after 10s")
}

// A file replaced between the request's open and the background open must
// not lend its digest to the revision the request saw.
func TestBackgroundHashSkipsReplacedFile(t *testing.T) {
	mod := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		name            string
		bgMod, laterMod time.Time
		want            bool
	}{
		{"same revision is stored", mod, time.Time{}, true},
		{"replaced before the hash", mod.Add(time.Second), time.Time{}, false},
		{"replaced during the hash", mod, mod.Add(time.Second), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newDigestCache()
			key := digestKey{name: "big.mp4", modTime: mod.UnixNano(), size: 1 << 10}
			f := &bigFile{size: 1 << 10, modTime: tc.bgMod, laterMod: tc.laterMod}
			c.hashInBackground(key, func() (fs.File, error) { return f, nil })
			waitIdle(t, c)
			if _, ok := c.done.Load(key); ok != tc.want {
				t.Errorf("digest stored = %v, want %v", ok, tc.want)
			}
			// A file already replaced is not worth reading: for a
			// multi-GB video that is the whole cost this hash avoids.
			if tc.bgMod != mod && f.read != 0 {
				t.Errorf("read %d bytes of a file already replaced, want 0", f.read)
			}
		})
	}
}

// Every slot busy means no new goroutine: the request moves on and a
// later one retries.
func TestBackgroundHashRespectsSlots(t *testing.T) {
	c := newDigestCache()
	for range backgroundHashSlots {
		c.slots <- struct{}{}
	}
	opened := false
	c.hashInBackground(digestKey{name: "x"}, func() (fs.File, error) {
		opened = true
		return nil, fs.ErrNotExist
	})
	time.Sleep(20 * time.Millisecond)
	if opened {
		t.Error("hash started with every slot busy")
	}
	if _, ok := c.inflight.Load(digestKey{name: "x"}); ok {
		t.Error("key left in flight with no hash running, so no later request retries it")
	}
}

// conditionalHandlers serve the same file, with a sub-second modtime, from
// the *os.Root branch, an in-memory FS, and a non-seekable FS.
func conditionalHandlers(t *testing.T) map[string]http.Handler {
	t.Helper()
	mod := time.Unix(1_700_000_000, 500_000_000)
	dir := t.TempDir()
	p := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(p, rangeBody(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
	mem := fstest.MapFS{"clip.mp4": &fstest.MapFile{Data: rangeBody(), ModTime: mod}}
	return map[string]http.Handler{
		"disk":         Handler(Config{FS: os.DirFS(dir)}),
		"memory":       Handler(Config{FS: mem}),
		"non-seekable": Handler(Config{FS: nonSeekableFS{inner: mem}}),
	}
}

// RFC 9110 §13.2.2: If-Match and If-Unmodified-Since are evaluated before
// If-None-Match, so a failed If-Match is a 412 even when If-None-Match
// matches. Both branches answer alike.
func TestPreconditionOrder(t *testing.T) {
	for name, h := range conditionalHandlers(t) {
		t.Run(name, func(t *testing.T) {
			full := serveWith(h, http.MethodGet, "/clip.mp4", nil)
			etag, lastMod := full.Header().Get("ETag"), full.Header().Get("Last-Modified")
			for _, tc := range []struct {
				name string
				hdr  map[string]string
				want int
			}{
				{"If-Match fails before If-None-Match", map[string]string{"If-Match": `"other"`, "If-None-Match": etag}, http.StatusPreconditionFailed},
				{"weak If-Match never matches", map[string]string{"If-Match": "W/" + etag}, http.StatusPreconditionFailed},
				{"If-Match current", map[string]string{"If-Match": etag}, http.StatusOK},
				{"If-Unmodified-Since past", map[string]string{"If-Unmodified-Since": "Mon, 02 Jan 2006 15:04:05 GMT"}, http.StatusPreconditionFailed},
				{"If-Unmodified-Since echoed", map[string]string{"If-Unmodified-Since": lastMod}, http.StatusOK},
				{"weak If-None-Match", map[string]string{"If-None-Match": "W/" + etag}, http.StatusNotModified},
				{"If-Modified-Since echoed", map[string]string{"If-Modified-Since": lastMod}, http.StatusNotModified},
			} {
				if got := serveWith(h, http.MethodGet, "/clip.mp4", tc.hdr).Code; got != tc.want {
					t.Errorf("%s: status %d, want %d", tc.name, got, tc.want)
				}
			}
		})
	}
}

// rewindOnlyFS hands out files whose Seek supports only SeekStart, which
// is all the handler needed before ServeContent; ServeContent also seeks
// to the end.
type rewindOnlyFS struct{ inner fs.FS }

type rewindOnlyFile struct{ fs.File }

func (f rewindOnlyFile) Seek(off int64, whence int) (int64, error) {
	if whence != io.SeekStart {
		return 0, errors.New("rewind only")
	}
	return f.File.(io.Seeker).Seek(off, whence)
}

func (r rewindOnlyFS) Open(name string) (fs.File, error) {
	f, err := r.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return rewindOnlyFile{f}, nil
}

// fs.FS promises no seeking, and ServeContent needs Seek to the end as
// well as a rewind. A handle that cannot do both keeps the full-body
// answer and must not advertise ranges it cannot serve.
func TestUnseekableHandleServesFull(t *testing.T) {
	mem := fstest.MapFS{"clip.mp4": &fstest.MapFile{Data: rangeBody()}}
	for name, fsys := range map[string]fs.FS{
		"no Seek":     nonSeekableFS{inner: mem},
		"rewind only": rewindOnlyFS{inner: mem},
	} {
		t.Run(name, func(t *testing.T) {
			rec := serveWith(Handler(Config{FS: fsys}), http.MethodGet, "/clip.mp4", map[string]string{"Range": "bytes=0-99"})
			if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), rangeBody()) {
				t.Fatalf("status %d with %d bytes, want 200 with the full 1000", rec.Code, rec.Body.Len())
			}
			if got := rec.Header().Get("Accept-Ranges"); got != "" {
				t.Errorf("Accept-Ranges %q, want none", got)
			}
		})
	}
}

// An in-place rewrite of the same size with the modtime put back (cp -p,
// rsync -t) must not keep the old digest: the cached ETag would validate
// the new bytes, and If-Range would splice a resumed download.
func TestRewriteKeepingModTimeNewETag(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("fileRevision is wired for linux and darwin")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(p, bytes.Repeat([]byte("A"), 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	mod := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{FS: os.DirFS(dir)})
	before := serveWith(h, http.MethodGet, "/clip.mp4", nil).Header().Get("ETag")

	if err := os.WriteFile(p, bytes.Repeat([]byte("B"), 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
	rec := serveWith(h, http.MethodGet, "/clip.mp4", map[string]string{"Range": "bytes=500-", "If-Range": before})
	if after := rec.Header().Get("ETag"); after == before {
		t.Errorf("ETag %s survived a same-size rewrite with the modtime put back", after)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("If-Range with the old ETag: status %d, want 200 (whole new file)", rec.Code)
	}
}
