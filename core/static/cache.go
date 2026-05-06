package static

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// generateETag returns a double-quoted ETag string derived from the SHA-256
// hash of data, truncated to 32 hex characters for brevity.
func generateETag(data []byte) string {
	h := sha256.Sum256(data)
	return `"` + hex.EncodeToString(h[:16]) + `"`
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
