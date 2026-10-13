package static

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// generateETag returns a double-quoted ETag string derived from the SHA-256
// hash of data, truncated to 32 hex characters for brevity.
func generateETag(data []byte) string {
	h := sha256.Sum256(data)
	return `"` + hex.EncodeToString(h[:16]) + `"`
}

// digestKey identifies a file revision within ONE filesystem. The cache
// holding it is per-handler (Config.digests), never process-wide: an
// embed.FS reports the zero time.Time as the modtime of every file, so a
// shared cache would collapse to (name, size) across every embed in the
// process. Two handlers backed by different filesystems, a site embed
// and an admin embed both serving "app.css", would then hand the second
// one the first one's ETag, and answer 304 Not Modified for content the
// client has never seen.
//
// Within one filesystem the key also carries the inode and change time
// where the platform reports them (Linux, macOS; see fileRevision). Name,
// size and modtime alone repeat after an in-place rewrite of the same
// size whose modtime is put back (cp -p, rsync -t, a pinned build stamp),
// and the cached digest would then validate the new bytes: a stale 304,
// or an If-Range that splices a resumed download from two revisions. An
// atomic replace changes the inode; any write changes the ctime, which no
// tool can set back. embed.FS and fstest.MapFS report neither and cannot
// change under a running handler.
type digestKey struct {
	name    string
	modTime int64
	size    int64
	ino     uint64
	ctime   int64
}

func keyOf(name string, stat fs.FileInfo) digestKey {
	ino, ctime := fileRevision(stat)
	return digestKey{name: name, modTime: stat.ModTime().UnixNano(), size: stat.Size(), ino: ino, ctime: ctime}
}

// digestCache memoises one handler's content digests (see digestKey for
// why it is per-handler) and tracks the large files being hashed in the
// background.
type digestCache struct {
	done     sync.Map      // digestKey → quoted ETag
	inflight sync.Map      // digestKey → struct{}: a background hash is running
	slots    chan struct{} // bounds concurrent background hashes
}

func newDigestCache() *digestCache {
	return &digestCache{slots: make(chan struct{}, backgroundHashSlots)}
}

// largeFileETagThreshold is the size above which fileETag hashes a file
// off the request path. Hashing reads the whole file, and for a multi-GB
// video that is seconds of disk before the first byte goes out. Until the
// background hash lands, the file is served with no ETag: Last-Modified
// still revalidates it, and an If-Range carrying an ETag gets the whole
// file rather than a range.
//
// The ETag stays a content hash at every size. A size-and-modtime ETag
// would skip the read, but it repeats across releases of a same-sized
// file whenever the build pins modtimes (reproducible builds stamp a
// fixed date), survives a same-size rewrite inside one modtime tick, and
// differs between replicas that unpacked the same file at different
// times. As a strong validator the first two let If-Range splice a
// resumed download from two revisions.
const largeFileETagThreshold = 16 << 20

// backgroundHashSlots caps how many large files one handler hashes at
// once. A request that finds every slot busy serves without an ETag and
// leaves the hash to a later request.
const backgroundHashSlots = 2

// fileETag returns a double-quoted content-hash ETag for the file. On a
// cache miss it streams the file through SHA-256, the content is never
// held in memory, which matters for large files. A read error is returned
// so the caller can map it to 500 rather than masking it as 404.
// The bool reports whether f was READ to compute the digest. On a cache
// hit nothing is read and the handle is still at offset 0; on a miss the
// handle is exhausted and the caller must rewind (or reopen) before
// serving the body.
// A file above largeFileETagThreshold that misses the cache is not read:
// fileETag returns "" and hashes a fresh handle from reopen in the
// background, so a later request gets the ETag. A nil cache computes the
// digest inline without memoising it.
func fileETag(cache *digestCache, f fs.File, stat fs.FileInfo, name string, reopen func() (fs.File, error)) (string, bool, error) {
	key := keyOf(name, stat)
	if cache != nil {
		if v, ok := cache.done.Load(key); ok {
			return v.(string), false, nil
		}
		if stat.Size() > largeFileETagThreshold {
			cache.hashInBackground(key, reopen)
			return "", false, nil
		}
	}
	etag, err := hashETag(f)
	if err != nil {
		return "", true, err
	}
	if cache != nil {
		cache.done.Store(key, etag)
	}
	return etag, true, nil
}

func hashETag(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return `"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`, nil
}

// hashInBackground digests the file named by key on its own goroutine,
// at most once at a time per key and backgroundHashSlots at a time per
// handler. The digest is stored only when the handle it read still has
// the revision the key was built from, before and after the read,
// so a file replaced mid-hash never lends its digest to the old revision.
func (c *digestCache) hashInBackground(key digestKey, open func() (fs.File, error)) {
	if _, running := c.inflight.LoadOrStore(key, struct{}{}); running {
		return
	}
	select {
	case c.slots <- struct{}{}:
	default:
		c.inflight.Delete(key)
		return
	}
	go func() {
		defer func() {
			<-c.slots
			c.inflight.Delete(key)
		}()
		// No request owns this goroutine, so no recovery middleware sits
		// above it. A custom fs.File that panics must cost only its ETag.
		defer func() { _ = recover() }()
		f, err := open()
		if err != nil {
			return
		}
		defer f.Close()
		if !sameRevision(f, key) {
			return
		}
		etag, err := hashETag(f)
		if err != nil || !sameRevision(f, key) {
			return
		}
		c.done.Store(key, etag)
	}()
}

func sameRevision(f fs.File, key digestKey) bool {
	st, err := f.Stat()
	return err == nil && keyOf(key.name, st) == key
}

// setCacheHeaders sets Cache-Control, ETag, and Last-Modified headers on w.
func setCacheHeaders(w http.ResponseWriter, etag string, modTime time.Time, maxAge time.Duration) {
	// Cache-Control
	if maxAge > 0 {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(maxAge.Seconds())))
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}

	// ETag
	if etag != "" {
		w.Header().Set("ETag", etag)
	}

	// Last-Modified
	if !modTime.IsZero() {
		w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
	}
}

// preconditionStatus evaluates the request's preconditions in the order
// RFC 9110 §13.2.2 sets and returns 412, 304, or 0 to serve the file.
// If-Match and If-Unmodified-Since come first, so a failed If-Match is a
// 412 even when If-None-Match also matches. If-Match compares strongly
// and If-None-Match weakly. An empty etag (a large file whose background
// hash has not landed) matches only "*". The handler serves only GET and
// HEAD, so a matching If-None-Match is always a 304.
func preconditionStatus(r *http.Request, etag string, modTime time.Time) int {
	if im := r.Header.Get("If-Match"); im != "" {
		if !etagListMatch(im, etag, false) {
			return http.StatusPreconditionFailed
		}
	} else if ius := r.Header.Get("If-Unmodified-Since"); ius != "" && !modTime.IsZero() {
		if t, err := http.ParseTime(ius); err == nil && modTime.Truncate(time.Second).After(t) {
			return http.StatusPreconditionFailed
		}
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		if etagListMatch(inm, etag, true) {
			return http.StatusNotModified
		}
		return 0
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" && !modTime.IsZero() {
		// Last-Modified goes out at one-second precision, so compare at
		// that precision: a disk modtime carries nanoseconds the client
		// never saw.
		if t, err := http.ParseTime(ims); err == nil && !modTime.Truncate(time.Second).After(t) {
			return http.StatusNotModified
		}
	}
	return 0
}

// etagListMatch reports whether a comma-separated If-Match/If-None-Match
// list names etag. "*" matches any current file. A weak comparison
// ignores the W/ prefix on either side; a strong one refuses weak tags.
func etagListMatch(list, etag string, weak bool) bool {
	for _, tag := range strings.Split(list, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" {
			return true
		}
		if etag == "" || tag == "" {
			continue
		}
		if weak {
			if strings.TrimPrefix(tag, "W/") == strings.TrimPrefix(etag, "W/") {
				return true
			}
		} else if !strings.HasPrefix(tag, "W/") && tag == etag {
			return true
		}
	}
	return false
}

// FingerprintURL appends a content hash to the filename portion of the URL
// path for cache busting. For example:
//
//	FingerprintURL("/assets/app.js", "abc123") → "/assets/app.abc123.js"
func FingerprintURL(filePath, hash string) string {
	ext := path.Ext(filePath)
	base := strings.TrimSuffix(filePath, ext)
	return base + "." + hash + ext
}

// contentHash returns the first 12 hex characters of the SHA-256 hash of data,
// suitable for use as a fingerprint.
func contentHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:6])
}

// parseMaxAge parses a Cache-Control max-age value from a string like "3600".
func parseMaxAge(s string) time.Duration {
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	return 0
}
