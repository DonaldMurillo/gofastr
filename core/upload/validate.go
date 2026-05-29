package upload

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"
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

// MaxFilenameBytes caps the sanitised filename length. A user-supplied
// filename has no legitimate reason to exceed a few hundred bytes; the
// cap protects log lines, filesystem APIs, and database columns from
// pathological inputs.
const MaxFilenameBytes = 255

// SanitizeFilename removes path separators, null bytes, and other dangerous
// characters from a filename to prevent path traversal attacks. It also
// neutralises double-extension smuggling — e.g. `shell.php.jpg` becomes
// `shell_php.jpg` — so a misconfigured web server can't be tricked into
// executing a hidden interior extension.
//
// Control bytes (CR, LF, TAB, anything < 0x20) are dropped so a logged
// filename can't escape its log line via injected newlines or terminal
// control sequences. The final result is truncated to MaxFilenameBytes
// (preserving the extension) so an attacker can't ship a 10 MB filename.
func SanitizeFilename(name string) string {
	// Drop every control byte (NUL, CR, LF, TAB, ESC, ...) and DEL up
	// front. Done before filepath.Base because `evil.php\x00.jpg` would
	// otherwise reach filepath.Base intact and look like a `.jpg` file,
	// while the OS open() syscall would truncate at the NUL and write
	// `evil.php`. Same hazard applies to CR/LF: log lines / multipart
	// Content-Disposition headers split on those bytes.
	name = stripControlBytes(name)

	// Normalize backslashes to forward slashes for cross-platform safety
	name = strings.ReplaceAll(name, "\\", "/")

	// Get just the base filename (removes any directory components)
	name = filepath.Base(name)

	// Remove leading dots (hidden files, ".." etc)
	for strings.HasPrefix(name, ".") {
		name = strings.TrimPrefix(name, ".")
	}

	// Replace problematic characters. Beyond path separators and traversal
	// segments, also neutralise shell-dangerous characters (`;`, `|`, `&`,
	// backtick, `$`, `<`, `>`, `*`, `?`) and Windows-illegal characters
	// (`:` , `"`) so a downstream consumer that interpolates the filename
	// into a shell or a Windows path can't be tricked into command
	// injection or NTFS alternate-data-stream tricks.
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		"\x00", "",
		"..", "_",
		";", "_",
		"|", "_",
		"&", "_",
		"`", "_",
		"$", "_",
		"<", "_",
		">", "_",
		"*", "_",
		"?", "_",
		":", "_",
		"\"", "_",
		"'", "_",
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

	// If the surviving filename is empty or consists only of dots /
	// spaces (e.g. ". .", " . ", "..."), fall back to a safe label.
	// A bare-dot or all-dots filename is still a hidden file on POSIX
	// and an invalid name on Windows.
	if isBlankOrDottyOnly(name) {
		return "upload"
	}

	// Length cap. Keep the extension if there is one so the truncated
	// name still round-trips through MIME detection. Truncation walks
	// back to a UTF-8 rune boundary so the result is never invalid
	// UTF-8 — an orphaned lead/continuation byte would corrupt the
	// storage key, utf8mb4 DB columns, and JSON consumers.
	if len(name) > MaxFilenameBytes {
		ext := filepath.Ext(name)
		if len(ext) >= MaxFilenameBytes {
			// Extension itself is absurdly long; drop it.
			name = truncateRunes(name, MaxFilenameBytes)
		} else {
			name = truncateRunes(name, MaxFilenameBytes-len(ext)) + ext
		}
	}

	return name
}

// truncateRunes returns the longest prefix of s that is at most maxBytes
// bytes and ends on a UTF-8 rune boundary, so the result is always valid
// UTF-8.
func truncateRunes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	// Back up until cut lands on a rune boundary (i.e. not in the middle
	// of a multibyte sequence).
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// stripControlBytes drops every ASCII control byte (< 0x20, plus DEL
// 0x7f) and every Unicode control / line-terminator rune. The latter
// closes a gap that a byte-only filter leaves open: U+0085 (NEL),
// U+2028 (LINE SEPARATOR), and U+2029 (PARAGRAPH SEPARATOR) are all
// encoded entirely with bytes >= 0x80, so they survive a byte-only
// scan yet are treated as line breaks by terminals, log processors,
// and JavaScript/JSON tooling — the same newline-injection hazard the
// ASCII filter defends against.
func stripControlBytes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isStrippableControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isStrippableControl reports whether r is a control character or a
// Unicode line/paragraph separator that must never survive into a
// stored filename.
func isStrippableControl(r rune) bool {
	switch r {
	case ' ', // LINE SEPARATOR
		' ', // PARAGRAPH SEPARATOR
		'': // NEXT LINE (NEL)
		return true
	}
	// C0 controls, DEL, and C1 controls (0x80–0x9f, which includes NEL
	// above but also other invisible control runes).
	if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
		return true
	}
	return false
}

// isBlankOrDottyOnly reports whether s is empty or made up of nothing
// but dots and ASCII whitespace.
func isBlankOrDottyOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '.' && c != ' ' && c != '\t' {
			return false
		}
	}
	return true
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
