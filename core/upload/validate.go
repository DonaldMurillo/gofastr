package upload

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

// ValidateMIME reads the first 512 bytes from file to detect MIME type,
// checks it against the allowed list, then resets the reader.
// If allowed is empty, all MIME types are permitted.
func ValidateMIME(file io.ReadSeeker, allowed []string) error {
	if len(allowed) == 0 {
		return nil
	}

	// Read first 512 bytes for MIME detection
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return fmt.Errorf("reading file for MIME detection: %w", err)
	}

	// Reset reader so the full file can be read later
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("resetting file reader: %w", err)
	}

	detected := http.DetectContentType(buf[:n])

	for _, a := range allowed {
		if detected == a {
			return nil
		}
	}

	return fmt.Errorf("unsupported MIME type: %s (allowed: %s)", detected, strings.Join(allowed, ", "))
}

// ValidateSize checks that the file size does not exceed max.
// A max of 0 means no limit.
func ValidateSize(size int64, max int64) error {
	if max <= 0 {
		return nil
	}
	if size > max {
		return fmt.Errorf("file size %d exceeds maximum %d bytes", size, max)
	}
	return nil
}

// ValidateExt checks that the file extension is in the allowed list.
// Extensions are compared case-insensitively without the leading dot.
// If allowed is empty, all extensions are permitted.
func ValidateExt(filename string, allowed []string) error {
	if len(allowed) == 0 {
		return nil
	}

	fileExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	if fileExt == "" {
		return fmt.Errorf("file has no extension")
	}

	for _, a := range allowed {
		if strings.ToLower(a) == fileExt {
			return nil
		}
	}

	return fmt.Errorf("file extension .%s not allowed (allowed: %s)", fileExt, strings.Join(allowed, ", "))
}

// SanitizeFilename removes path separators, null bytes, and other dangerous
// characters from a filename to prevent path traversal attacks.
func SanitizeFilename(name string) string {
	// Remove null bytes
	name = strings.ReplaceAll(name, "\x00", "")

	// Normalize backslashes to forward slashes for cross-platform safety
	name = strings.ReplaceAll(name, "\\", "/")

	// Get just the base filename (removes any directory components)
	name = filepath.Base(name)

	// Remove leading dots (hidden files, ".." etc)
	for strings.HasPrefix(name, ".") {
		name = strings.TrimPrefix(name, ".")
	}

	// Replace problematic characters
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		"\x00", "",
		"..", "_",
	)
	name = replacer.Replace(name)

	// Trim whitespace
	name = strings.TrimSpace(name)

	// If nothing remains, generate a fallback
	if name == "" {
		return "upload"
	}

	return name
}

// detectMIMEFromName attempts to detect MIME type from filename extension.
func detectMIMEFromName(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return ""
	}
	return mime.TypeByExtension(ext)
}
