package static

import (
	"mime"
	"path/filepath"
	"strings"
)

// mimeTypes maps file extensions to canonical web MIME types. It is
// consulted BEFORE mime.TypeByExtension on purpose: the stdlib answer
// depends on the host (Linux merges /etc/mime.types, which reports
// e.g. image/vnd.microsoft.icon for .ico and audio/webm for .webm),
// and a framework serving content must return the same Content-Type
// on every platform. Keep entries aligned with what browsers expect
// (.js is text/javascript per RFC 9239).
var mimeTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".htm":   "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".json":  "application/json",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".svg":   "image/svg+xml",
	".ico":   "image/x-icon",
	".webp":  "image/webp",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".eot":   "application/vnd.ms-fontobject",
	".mp4":   "video/mp4",
	".webm":  "video/webm",
	".pdf":   "application/pdf",
	".txt":   "text/plain; charset=utf-8",
	".xml":   "application/xml",
	".map":   "application/json",
	".wasm":  "application/wasm",
}

// DetectFromName returns the MIME type for the given filename based on
// its extension. The built-in canonical table wins so detection is
// platform-independent; mime.TypeByExtension only covers the long tail
// of extensions not in the table, and "application/octet-stream" is
// the final fallback.
func DetectFromName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return "application/octet-stream"
	}

	// Canonical table first: deterministic across platforms.
	if mt, ok := mimeTypes[ext]; ok {
		return mt
	}

	// Long tail: defer to the stdlib (host mime DB may extend this).
	if mt := mime.TypeByExtension(ext); mt != "" {
		return mt
	}

	return "application/octet-stream"
}
