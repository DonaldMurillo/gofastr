package static

import (
	"mime"
	"path/filepath"
	"strings"
)

// mimeTypes is a fallback map of file extensions to MIME types,
// used when mime.TypeByExtension returns an empty result.
var mimeTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".htm":   "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "application/javascript",
	".mjs":   "application/javascript",
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
// its extension. It first tries mime.TypeByExtension, then falls back
// to a built-in mapping, and finally returns "application/octet-stream"
// if no match is found.
func DetectFromName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return "application/octet-stream"
	}

	// Try the stdlib first.
	if mt := mime.TypeByExtension(ext); mt != "" {
		return mt
	}

	// Fallback to built-in map.
	if mt, ok := mimeTypes[ext]; ok {
		return mt
	}

	return "application/octet-stream"
}
