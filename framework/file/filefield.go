package file

import (
	"bytes"
	"context"
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
// tolerates leading whitespace (browsers do too).
func isUnsafeURLScheme(url string) bool {
	trimmed := strings.TrimLeft(url, " \t\r\n")
	lower := strings.ToLower(trimmed)
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
	// most HTML inputs, but classifies SVG as text/plain. Sniff the
	// leading non-whitespace token directly so both fall into the same
	// reject path.
	trim := bytes.TrimLeft(head, " \t\r\n\f\v")
	lower := bytes.ToLower(trim)
	if bytes.HasPrefix(lower, []byte("<svg")) ||
		bytes.HasPrefix(lower, []byte("<?xml")) ||
		bytes.HasPrefix(lower, []byte("<html")) ||
		bytes.HasPrefix(lower, []byte("<!doctype html")) ||
		bytes.HasPrefix(lower, []byte("<script")) ||
		bytes.HasPrefix(lower, []byte("<iframe")) {
		return fmt.Errorf("%w: HTML/XML/SVG content", ErrFileFieldUnsafeContent)
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
// The format is: uploads/{entityName}/{fieldName}/{sanitized_name}_{timestamp}{ext}
// Example: "uploads/posts/avatar/photo_1683398400000000000.png"
func GenerateFilePath(entityName, fieldName, filename string) string {
	// Sanitize the filename to prevent path traversal
	safe := upload.SanitizeFilename(filename)

	// Split into name and extension
	ext := filepath.Ext(safe)
	nameWithoutExt := strings.TrimSuffix(safe, ext)

	// Generate a unique name using nanosecond timestamp
	uniqueName := fmt.Sprintf("%s_%d%s", nameWithoutExt, time.Now().UnixNano(), ext)

	// Build the path using filepath.Join for cross-platform safety
	path := filepath.Join("uploads", entityName, fieldName, uniqueName)

	// Normalize to forward slashes for storage consistency
	return filepath.ToSlash(path)
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
