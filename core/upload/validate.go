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
// characters from a filename to prevent path traversal attacks. It also
// neutralises double-extension smuggling — e.g. `shell.php.jpg` becomes
// `shell_php.jpg` — so a misconfigured web server can't be tricked into
// executing a hidden interior extension.
func SanitizeFilename(name string) string {
	// Remove null bytes — note this is BEFORE filepath.Base because a
	// raw `evil.php\x00.jpg` would otherwise reach filepath.Base intact
	// and look like a `.jpg` file, while the OS open() syscall would
	// truncate at the null byte and write `evil.php`.
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

	// Neutralise dangerous interior extensions. We split on `.`, keep the
	// final segment as the real extension, and replace any *interior*
	// segment that matches a known executable extension with `_`. This
	// preserves legitimate compound extensions like `.tar.gz` while
	// turning `shell.php.jpg` into `shell_php.jpg`.
	name = neutraliseInteriorExecExts(name)

	// If nothing remains, generate a fallback
	if name == "" {
		return "upload"
	}

	return name
}

// dangerousExecExts is the deny-list of file extensions that have ever
// been treated as executable by a web server, shell, or scripting host.
// Anything appearing in an interior position of a multi-dotted filename
// is collapsed to an underscore.
var dangerousExecExts = map[string]bool{
	"php":   true,
	"phtml": true,
	"php3":  true,
	"php4":  true,
	"php5":  true,
	"php7":  true,
	"phar":  true,
	"asp":   true,
	"aspx":  true,
	"cgi":   true,
	"jsp":   true,
	"jspx":  true,
	"pl":    true,
	"py":    true,
	"rb":    true,
	"sh":    true,
	"bash":  true,
	"zsh":   true,
	"bat":   true,
	"cmd":   true,
	"com":   true,
	"exe":   true,
	"dll":   true,
	"so":    true,
	"dylib": true,
	"vbs":   true,
	"vbe":   true,
	"js":    true,
	"mjs":   true,
	"htm":   true,
	"html":  true,
	"svg":   true,
	"xhtml": true,
}

func neutraliseInteriorExecExts(name string) string {
	parts := strings.Split(name, ".")
	if len(parts) < 3 {
		// No interior segment to worry about — either no extension or
		// exactly one (base.ext) which is the legitimate shape.
		return name
	}
	for i := 1; i < len(parts)-1; i++ {
		if dangerousExecExts[strings.ToLower(parts[i])] {
			parts[i] = "_" + parts[i]
		}
	}
	return strings.Join(parts, ".")
}

// detectMIMEFromName attempts to detect MIME type from filename extension.
func detectMIMEFromName(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return ""
	}
	return mime.TypeByExtension(ext)
}
