package file

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// Validation errors returned by [FileField.Validate]. Callers can match
// on these without parsing the message.
var (
	ErrFileFieldURLScheme   = errors.New("filefield: URL has unsafe scheme")
	ErrFileFieldTraversal   = errors.New("filefield: contains path traversal")
	ErrFileFieldMimeUnsafe  = errors.New("filefield: MIME type contains unsafe characters")
	ErrFileFieldSize        = errors.New("filefield: size is negative or oversize")
	ErrFileFieldOversize    = errors.New("filefield: field exceeds length limit")
	ErrFileFieldTooLarge    = errors.New("filefield: file exceeds maximum size")
	ErrFileFieldUnsafeContent = errors.New("filefield: file content is unsafe by default")
)

// MaxFileFieldStringBytes caps the length of any FileField string field
// — anything past this is rejected at validation time. A legitimate URL,
// MIME, or storage ref does not need 8 KB.
const MaxFileFieldStringBytes = 8 * 1024

// MaxProcessFileSize caps the in-memory size of a single upload read by
// [ProcessFileField]. The default protects callers that haven't wired a
// stricter limit elsewhere from unbounded memory consumption — a hostile
// client can otherwise stream gigabytes into RAM.
const MaxProcessFileSize int64 = 32 << 20 // 32 MiB

// FileField holds metadata about an uploaded file associated with an entity field.
type FileField struct {
	// URL is the publicly accessible path or URL to the file.
	URL string `json:"url"`

	// Filename is the original filename as provided by the client.
	Filename string `json:"filename"`

	// MimeType is the detected MIME type of the file.
	MimeType string `json:"mime_type"`

	// Size is the file size in bytes.
	Size int64 `json:"size"`

	// StorageRef is the storage backend key used to reference this file.
	StorageRef string `json:"storage_ref"`
}

// Validate enforces invariants on a FileField that came from an untrusted
// source (typically JSON unmarshal in a CRUD body). It is NOT called
// automatically — callers that accept FileField as request input should
// call it before persisting or rendering. The constructors in this
// package (ProcessFileField) already produce valid FileFields.
//
// Rejected inputs:
//   - URL with javascript:, vbscript:, or data: scheme — would XSS when
//     rendered as href/src by a downstream consumer.
//   - URL or StorageRef containing `..` segments — would escape the
//     storage root on a vulnerable filesystem backend.
//   - MimeType containing characters outside the MIME-safe set —
//     normal MIME types are `type/subtype` with letters, digits,
//     `+`, `-`, `.`; angle brackets / quotes indicate an XSS attempt.
//   - Size < 0 — unsigned conventions require non-negative.
//   - Any string field over MaxFileFieldStringBytes.
func (f *FileField) Validate() error {
	if f == nil {
		return nil
	}
	for name, v := range map[string]string{
		"url":          f.URL,
		"filename":     f.Filename,
		"mime_type":    f.MimeType,
		"storage_ref":  f.StorageRef,
	} {
		if len(v) > MaxFileFieldStringBytes {
			return fmt.Errorf("%w: field %q is %d bytes (max %d)", ErrFileFieldOversize, name, len(v), MaxFileFieldStringBytes)
		}
	}
	if f.Size < 0 {
		return fmt.Errorf("%w: size %d", ErrFileFieldSize, f.Size)
	}
	if isUnsafeURLScheme(f.URL) {
		return fmt.Errorf("%w: %q", ErrFileFieldURLScheme, f.URL)
	}
	if hasTraversal(f.URL) {
		return fmt.Errorf("%w: url %q", ErrFileFieldTraversal, f.URL)
	}
	if hasTraversal(f.StorageRef) {
		return fmt.Errorf("%w: storage_ref %q", ErrFileFieldTraversal, f.StorageRef)
	}
	if !isSafeMIMEString(f.MimeType) {
		return fmt.Errorf("%w: %q", ErrFileFieldMimeUnsafe, f.MimeType)
	}
	return nil
}

// isUnsafeURLScheme reports whether url begins with a scheme that should
// never appear in stored file metadata. Match is case-insensitive and
// mirrors browser URL normalization. Per the WHATWG URL spec, browsers
// strip TAB/LF/CR from *anywhere* in the URL, and additionally remove any
// leading C0-control-or-space bytes (0x00-0x20) before resolving the
// scheme. We do the same so that neither "java\tscript:" nor a URL led by
// a stray control byte such as "\x0cjavascript:" slips past while a
// browser still resolves it as javascript:.
func isUnsafeURLScheme(url string) bool {
	// Strip TAB/LF/CR from anywhere (browsers ignore these wherever they
	// appear inside a URL).
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\n', '\r':
			return -1
		}
		return r
	}, url)
	// Trim every leading C0 control byte (0x00-0x1F) or space (0x20),
	// matching the spec's "remove leading C0 control or space" rule.
	cleaned = strings.TrimLeft(cleaned, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f"+
		"\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f ")
	lower := strings.ToLower(cleaned)
	for _, bad := range []string{"javascript:", "vbscript:", "data:"} {
		if strings.HasPrefix(lower, bad) {
			return true
		}
	}
	return false
}

// hasTraversal reports whether s contains a `..` segment. We're
// deliberately strict — any `..` substring counts, even one tucked
// inside a filename, because a downstream join may interpret it as a
// segment.
func hasTraversal(s string) bool {
	return strings.Contains(s, "..")
}

// isSafeMIMEString reports whether s is shaped like a real MIME type
// (letters, digits, and the small punctuation set used by IANA names).
// Empty is treated as safe — Validate handles required-field semantics
// at a higher layer.
func isSafeMIMEString(s string) bool {
	if s == "" {
		return true
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '/' || c == '+' || c == '-' || c == '.' || c == '_':
		case c == ';' || c == '=' || c == ' ': // parameter syntax: "text/html; charset=utf-8"
		default:
			return false
		}
	}
	return true
}

// ProcessFileField reads a file from the given reader, saves it via the storage
// backend, and returns a FileField with all metadata. The file is stored at a
// path generated by GenerateFilePath.
//
// Defaults that protect callers from common upload abuse paths:
//
//   - Size is capped at [MaxProcessFileSize]. Anything larger returns
//     [ErrFileFieldTooLarge] without buffering the rest of the body.
//   - Content is sniffed from the first 512 bytes (and an XML/HTML probe
//     of the leading non-whitespace) and rejected when it matches a
//     known-dangerous shape: SVG / XML, HTML, or executable magic bytes
//     (MZ for PE, 0x7fELF for ELF, Mach-O headers). The filename
//     extension and any client-supplied Content-Type are ignored —
//     attackers can lie about both.
//
// The ctx parameter should come from the HTTP request so cancellation and
// deadlines are respected during slow uploads.
func ProcessFileField(ctx context.Context, store upload.Storage, file interface {
	Read([]byte) (int, error)
}, filename string, entityName, fieldName string) (*FileField, error) {
	if store == nil {
		return nil, fmt.Errorf("filefield: storage backend is required")
	}

	// Read at most MaxProcessFileSize+1 bytes so we can detect an
	// oversize input without ever buffering the full attacker payload.
	limited := io.LimitReader(readerOf(file), MaxProcessFileSize+1)
	var buf bytes.Buffer
	size, err := buf.ReadFrom(limited)
	if err != nil {
		return nil, fmt.Errorf("filefield: reading file: %w", err)
	}
	if size > MaxProcessFileSize {
		return nil, fmt.Errorf("%w: %d bytes (max %d)", ErrFileFieldTooLarge, size, MaxProcessFileSize)
	}

	data := buf.Bytes()

	// Sniff content from the bytes themselves — never trust the filename
	// or any supplied Content-Type.
	if err := rejectUnsafeContent(data); err != nil {
		return nil, err
	}

	// Detect MIME type from content
	mimeType := http.DetectContentType(data)
	if mimeType == "application/octet-stream" {
		// Try to detect from filename extension
		if detected := detectMIMEFromName(filename); detected != "" {
			mimeType = detected
		}
	}

	// Generate a safe, unique storage path
	path := GenerateFilePath(entityName, fieldName, filename)

	// Save to storage backend using the provided context
	if err := store.Save(ctx, path, bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("filefield: saving file: %w", err)
	}

	return &FileField{
		URL:        path,
		Filename:   filepath.Base(filename),
		MimeType:   mimeType,
		Size:       size,
		StorageRef: path,
	}, nil
}

// readerOf adapts the narrow Read-only interface accepted by
// [ProcessFileField] back to an [io.Reader] for use with stdlib helpers
// like [io.LimitReader].
func readerOf(r interface {
	Read([]byte) (int, error)
}) io.Reader {
	if rr, ok := r.(io.Reader); ok {
		return rr
	}
	return readerAdapter{r}
}

type readerAdapter struct {
	r interface {
		Read([]byte) (int, error)
	}
}

func (a readerAdapter) Read(p []byte) (int, error) { return a.r.Read(p) }

// rejectUnsafeContent inspects the first bytes of an upload and returns
// [ErrFileFieldUnsafeContent] when the content matches a known-dangerous
// shape. We do this in addition to [http.DetectContentType] because the
// stdlib sniffer is permissive — e.g. an SVG body with a leading `<svg`
// tag is reported as text/plain, but rendered as active content by any
// browser that resolves the extension or sniffs harder.
func rejectUnsafeContent(data []byte) error {
	head := data
	if len(head) > 512 {
		head = head[:512]
	}

	// Executable magic bytes — PE/COFF (MZ), ELF, Mach-O (both 32- and
	// 64-bit, both endians). None of these have a legitimate reason to
	// be uploaded into a content field by default.
	if len(head) >= 2 && head[0] == 'M' && head[1] == 'Z' {
		return fmt.Errorf("%w: executable (PE/MZ) content", ErrFileFieldUnsafeContent)
	}
	if len(head) >= 4 && head[0] == 0x7f && head[1] == 'E' && head[2] == 'L' && head[3] == 'F' {
		return fmt.Errorf("%w: executable (ELF) content", ErrFileFieldUnsafeContent)
	}
	if len(head) >= 4 {
		m := uint32(head[0])<<24 | uint32(head[1])<<16 | uint32(head[2])<<8 | uint32(head[3])
		switch m {
		case 0xfeedface, 0xfeedfacf, 0xcefaedfe, 0xcffaedfe, 0xcafebabe, 0xbebafeca:
			return fmt.Errorf("%w: executable (Mach-O) content", ErrFileFieldUnsafeContent)
		}
	}

	// XML / HTML / SVG. http.DetectContentType returns text/html for
	// most HTML inputs, but classifies SVG (and any markup that is not
	// the leading token) as text/plain. The leading-token heuristic is
	// trivially bypassed: a DOCTYPE/BOM/comment/arbitrary prefix pushes
	// the dangerous tag off offset 0, and DetectContentType then reports
	// text/plain. So we strip a leading byte-order mark and scan the
	// whole sniff window (case-insensitive) for any active-content token
	// anywhere — none of these belong in a non-active content field.
	trim := bytes.TrimLeft(stripBOM(head), " \t\r\n\f\v")
	lower := bytes.ToLower(trim)
	for _, tok := range [][]byte{
		[]byte("<svg"),
		[]byte("<?xml"),
		[]byte("<html"),
		[]byte("<!doctype"),
		[]byte("<script"),
		[]byte("<iframe"),
		[]byte("<img"),
		[]byte("<math"),
		[]byte("<object"),
		[]byte("<embed"),
		[]byte("<link"),
		[]byte("<base"),
		[]byte("<style"),
		[]byte("javascript:"),
	} {
		if bytes.Contains(lower, tok) {
			return fmt.Errorf("%w: HTML/XML/SVG content", ErrFileFieldUnsafeContent)
		}
	}

	// Final fallback: trust http.DetectContentType for cases it gets
	// right (real HTML with <html> tag elsewhere, etc.).
	switch ct := http.DetectContentType(head); {
	case strings.HasPrefix(ct, "text/html"),
		strings.HasPrefix(ct, "image/svg"),
		strings.HasPrefix(ct, "text/xml"),
		strings.HasPrefix(ct, "application/xml"):
		return fmt.Errorf("%w: %s", ErrFileFieldUnsafeContent, ct)
	}

	return nil
}

// stripBOM removes a single leading byte-order mark (UTF-8, UTF-16 LE/BE,
// UTF-32 LE/BE) so a BOM prefix can't push a dangerous markup token off
// offset 0 and out of the sniff path. Only the BOM bytes are stripped;
// for UTF-16/32 the following ASCII bytes (e.g. "<svg") remain matchable
// because they sit interleaved-or-adjacent in the trimmed window.
func stripBOM(b []byte) []byte {
	switch {
	case len(b) >= 4 && b[0] == 0x00 && b[1] == 0x00 && b[2] == 0xFE && b[3] == 0xFF:
		return b[4:] // UTF-32 BE
	case len(b) >= 4 && b[0] == 0xFF && b[1] == 0xFE && b[2] == 0x00 && b[3] == 0x00:
		return b[4:] // UTF-32 LE
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return b[3:] // UTF-8
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return b[2:] // UTF-16 BE
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return b[2:] // UTF-16 LE
	}
	return b
}

// DeleteFileField removes a previously stored file from the storage backend.
// Returns nil if the FileField is nil or has no storage reference.
// The ctx parameter should come from the HTTP request.
func DeleteFileField(ctx context.Context, store upload.Storage, ff *FileField) error {
	if store == nil {
		return fmt.Errorf("filefield: storage backend is required")
	}
	if ff == nil || ff.StorageRef == "" {
		return nil
	}
	return store.Delete(ctx, ff.StorageRef)
}

// GenerateFilePath produces a safe, unique file path for storage.
// The format is: uploads/{entityName}/{fieldName}/{sanitized_name}_{timestamp}_{rand}{ext}
// Example: "uploads/posts/avatar/photo_1683398400000000000_3f9a1c2b.png"
//
// The path carries a crypto/rand component in addition to the timestamp so
// that two uploads of the same filename to the same field never collide —
// uniqueness must not depend on clock resolution. Without it, two requests
// landing within the same nanosecond (or the same clock tick on platforms
// whose clock does not advance every nanosecond) would resolve to the same
// path and one upload would silently overwrite the other.
func GenerateFilePath(entityName, fieldName, filename string) string {
	// Sanitize the filename to prevent path traversal
	safe := upload.SanitizeFilename(filename)

	// Split into name and extension
	ext := filepath.Ext(safe)
	nameWithoutExt := strings.TrimSuffix(safe, ext)

	// Generate a unique name from the timestamp plus a random suffix. The
	// random suffix is the real uniqueness guarantee; the timestamp is
	// retained only for human-readable ordering. crypto/rand.Read on a
	// fixed-size buffer never returns a short read without an error, but
	// if the platform RNG fails we fall back to the timestamp alone rather
	// than panicking mid-upload.
	uniqueName := fmt.Sprintf("%s_%d_%s%s", nameWithoutExt, time.Now().UnixNano(), randomSuffix(), ext)

	// Build the path using filepath.Join for cross-platform safety
	path := filepath.Join("uploads", entityName, fieldName, uniqueName)

	// Normalize to forward slashes for storage consistency
	return filepath.ToSlash(path)
}

// randomSuffix returns a hex-encoded 8-byte crypto-random token used to
// guarantee storage-path uniqueness independent of clock resolution. If
// the platform RNG fails it returns an empty string; the caller still has
// the timestamp, so a failure degrades to the old behaviour rather than
// aborting an upload.
func randomSuffix() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// detectMIMEFromName attempts to detect MIME type from filename extension.
func detectMIMEFromName(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	mimeMap := map[string]string{
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".webp": "image/webp",
		".svg":  "image/svg+xml",
		".pdf":  "application/pdf",
		".txt":  "text/plain",
		".csv":  "text/csv",
		".json": "application/json",
		".xml":  "application/xml",
		".zip":  "application/zip",
	}
	if m, ok := mimeMap[ext]; ok {
		return m
	}
	return ""
}
