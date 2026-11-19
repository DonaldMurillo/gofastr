package upload

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// ValidateMIME sniffs the first 512 bytes of file, rewinds it, and refuses
// a base media type not in allowed; an empty allowed permits all.
func ValidateMIME(file io.ReadSeeker, allowed []string) error {
	if len(allowed) == 0 {
		return nil
	}

	buf := make([]byte, 512)
	n, err := io.ReadFull(file, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return fmt.Errorf("reading file for MIME detection: %w", err)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("resetting file reader: %w", err)
	}

	detected := http.DetectContentType(buf[:n])

	base := mediaType(detected)
	if slices.ContainsFunc(allowed, func(a string) bool { return mediaType(a) == base }) {
		return nil
	}

	return i18n.NewError(reasonMIMENotAllowed, map[string]any{"type": detected, "allowed": strings.Join(allowed, ", ")})
}

func mediaType(v string) string {
	if i := strings.IndexByte(v, ';'); i >= 0 {
		v = v[:i]
	}
	return strings.ToLower(strings.TrimSpace(v))
}

// ValidateSize refuses size over max; a max of 0 or less is no limit.
func ValidateSize(size int64, max int64) error {
	if max <= 0 {
		return nil
	}
	if size > max {
		return i18n.NewError(reasonSizeExceeded, map[string]any{"size": size, "max": max})
	}
	return nil
}

// ValidateExt refuses filename unless its extension, without the dot and
// folded by ASCII case only, is in allowed; an empty allowed permits all.
func ValidateExt(filename string, allowed []string) error {
	if len(allowed) == 0 {
		return nil
	}

	fileExt := asciicase.Lower(strings.TrimPrefix(filepath.Ext(filename), "."))
	if fileExt == "" {
		return i18n.NewError(reasonNoExtension)
	}

	for _, a := range allowed {
		if asciicase.Lower(a) == fileExt {
			return nil
		}
	}

	return i18n.NewError(reasonExtNotAllowed, map[string]any{"ext": fileExt, "allowed": strings.Join(allowed, ", ")})
}

// MaxFilenameBytes caps SanitizeFilename's result.
const MaxFilenameBytes = 255

// SanitizeFilenameInputBound caps SanitizeFilename's input before any pass
// runs over it.
const SanitizeFilenameInputBound = 4 * MaxFilenameBytes

func boundFilenameInput(name string) string {
	if len(name) <= SanitizeFilenameInputBound {
		return name
	}
	return truncateRunes(name, SanitizeFilenameInputBound)
}

// SanitizeFilename returns a safe base name for name: controls and
// invisible runes dropped, directories, leading dots and shell or Windows
// metacharacters removed, interior executable extensions defused
// (shell.php.jpg becomes shell._php.jpg), and the result cut on a rune
// boundary to MaxFilenameBytes, extension kept. A name left empty is
// "upload".
func SanitizeFilename(name string) string {
	name = boundFilenameInput(name)

	name = stripControlBytes(name)

	name = strings.ReplaceAll(name, "\\", "/")

	name = filepath.Base(name)

	for strings.HasPrefix(name, ".") {
		name = strings.TrimPrefix(name, ".")
	}

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

	name = strings.TrimSpace(name)

	name = neutraliseInteriorExecExts(name)

	if isBlankOrDottyOnly(name) {
		return "upload"
	}

	if len(name) > MaxFilenameBytes {
		ext := filepath.Ext(name)
		if len(ext) >= MaxFilenameBytes {
			name = truncateRunes(name, MaxFilenameBytes)
		} else {
			name = truncateRunes(name, MaxFilenameBytes-len(ext)) + ext
		}
	}

	return name
}

func truncateRunes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func stripControlBytes(s string) string {
	if !strings.ContainsFunc(s, isStrippableControl) {
		return s
	}
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

func isStrippableControl(r rune) bool {
	switch r {
	case '\u2028', '\u2029', '\u0085':
		return true
	}
	if r < 0x20 || r == 0x7f || textsafe.IsC1(r) {
		return true
	}
	return textsafe.IsInvisible(r)
}

func isBlankOrDottyOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '.' && c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}

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
		return name
	}
	for i := 1; i < len(parts)-1; i++ {
		if dangerousExecExts[strings.ToLower(parts[i])] {
			parts[i] = "_" + parts[i]
		}
	}
	return strings.Join(parts, ".")
}
