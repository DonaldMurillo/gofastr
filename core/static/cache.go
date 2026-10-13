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
// Within one filesystem the (name, modtime, size) key is sound: edit or
// replace the file and the key changes, so a fresh digest is computed.
type digestKey struct {
	name    string
	modTime int64
	size    int64
}

// fileETag returns a double-quoted content-hash ETag for the file. On a
// cache miss it streams the file through SHA-256, the content is never
// held in memory, which matters for large files. On a cache miss the
// caller MUST Seek the file back to the start before serving the body.
// A read error is returned so the caller can map it to 500 rather than
// masking it as 404.
// The bool reports whether f was READ to compute the digest. On a cache
// hit nothing is read and the handle is still at offset 0; on a miss the
// handle is exhausted and the caller must rewind (or reopen) before
// serving the body.
// The cache is the calling handler's own; a nil cache computes the digest
// without memoising it.
// A file above largeFileETagThreshold with a usable modtime is not read at
// all: it gets statETag.
func fileETag(cache *sync.Map, f fs.File, stat fs.FileInfo, name string) (string, bool, error) {
	if stat.Size() > largeFileETagThreshold && usableModTime(stat.ModTime()) {
		return statETag(stat), false, nil
	}
	key := digestKey{name: name, modTime: stat.ModTime().UnixNano(), size: stat.Size()}
	if cache != nil {
		if v, ok := cache.Load(key); ok {
			return v.(string), false, nil
		}
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", true, err
	}
	etag := `"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`
	if cache != nil {
		cache.Store(key, etag)
	}
	return etag, true, nil
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

// checkPreconditions checks If-None-Match and If-Modified-Since headers.
// Returns true if a 304 should be sent (the client's cache is still valid).
func checkPreconditions(r *http.Request, etag string, modTime time.Time) bool {
	// Check If-None-Match first (ETag takes precedence per RFC 7232).
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		for _, tag := range parseETagList(inm) {
			if tag == etag || tag == "*" {
				return true
			}
		}
		return false
	}

	// Fall back to If-Modified-Since.
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		if imsTime, err := http.ParseTime(ims); err == nil {
			if !modTime.IsZero() && !modTime.After(imsTime) {
				return true
			}
		}
	}

	return false
}

// parseETagList splits a comma-separated list of ETags from If-None-Match.
func parseETagList(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
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

// largeFileETagThreshold is the size above which fileETag stops hashing a
// file that has a usable modtime. Hashing reads the whole file before the
// first byte goes out, and for a multi-GB video that is seconds of disk
// on the first request per revision. Below it the content hash stays: it
// survives a redeploy that resets modtimes, where size+modtime does not.
const largeFileETagThreshold = 16 << 20

// statETag is the validator for a large file: its size and modtime in
// hex, the shape nginx uses. It stays STRONG on purpose. If-Range only
// matches a strong ETag, so a weak one would turn every resumed download
// of exactly these files into a full restart. Size plus nanosecond modtime
// changes with every rewrite that a filesystem records; usableModTime
// keeps out the stamps that say nothing about the revision.
func statETag(stat fs.FileInfo) string {
	return `"` + strconv.FormatInt(stat.Size(), 16) + "-" + strconv.FormatInt(stat.ModTime().UnixNano(), 16) + `"`
}

// usableModTime reports whether a modtime can tell two revisions of a file
// apart. embed.FS reports the zero time for every file, and reproducible
// builds (Nix, SOURCE_DATE_EPOCH=0 images) stamp 0 or 1 on every file, so
// two releases of a same-sized file would share a size+modtime ETag and a
// client would resume a download across them.
func usableModTime(t time.Time) bool {
	return !t.IsZero() && t.Unix() > 1
}
