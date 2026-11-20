package static

import (
	"mime"
	"path/filepath"
	"strings"
)

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

// DetectFromName returns the MIME type for name's extension from the
// built-in table, then mime.TypeByExtension, then application/octet-stream.
func DetectFromName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return "application/octet-stream"
	}

	if mt, ok := mimeTypes[ext]; ok {
		return mt
	}

	if mt := mime.TypeByExtension(ext); mt != "" {
		return mt
	}

	return "application/octet-stream"
}
