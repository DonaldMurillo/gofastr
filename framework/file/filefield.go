package file

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/core/upload"
)

// Errors returned by Validate and ProcessFileField.
var (
	ErrFileFieldURLScheme     = errors.New("filefield: URL has unsafe scheme")
	ErrFileFieldTraversal     = errors.New("filefield: contains path traversal")
	ErrFileFieldMimeUnsafe    = errors.New("filefield: MIME type contains unsafe characters")
	ErrFileFieldSize          = errors.New("filefield: size is negative or oversize")
	ErrFileFieldOversize      = errors.New("filefield: field exceeds length limit")
	ErrFileFieldTooLarge      = errors.New("filefield: file exceeds maximum size")
	ErrFileFieldUnsafeContent = errors.New("filefield: file content is unsafe by default")
	ErrFileFieldControlBytes  = errors.New("filefield: field contains control bytes")
	ErrFileFieldType          = errors.New("filefield: file type is not allowed here")
)

// MaxFileFieldStringBytes caps every FileField string field.
const MaxFileFieldStringBytes = 8 * 1024

// MaxProcessFileSize caps the bytes [ProcessFileField] reads.
const MaxProcessFileSize int64 = 32 << 20 // 32 MiB

// FileField holds metadata about an uploaded file associated with an entity field.
type FileField struct {
	// URL is the publicly accessible path or URL to the file.
	URL string `json:"url"`

	// Filename is the original filename as provided by the client.
	Filename string `json:"filename"`

	// MimeType is the detected MIME type of the file.
	MimeType string `json:"mimeType"`

	// Size is the file size in bytes.
	Size int64 `json:"size"`

	// StorageRef is the storage backend key used to reference this file.
	StorageRef string `json:"storageRef"`

	// Image carries renditions and placeholder metadata derived from an
	// uploaded image. Nil unless ProcessFileField ran with
	// WithImageDeriver.
	Image *ImageDerivatives `json:"image,omitempty"`
}

// Validate reports the first rule an untrusted FileField breaks (scheme,
// traversal, control or forged characters, MIME charset, size, length). It
// is not called automatically; call it on FileField request input.
func (f *FileField) Validate() error {
	if f == nil {
		return nil
	}
	for _, entry := range []struct {
		key string
		val string
	}{
		{"url", f.URL},
		{"filename", f.Filename},
		{"mimeType", f.MimeType},
		{"storageRef", f.StorageRef},
	} {
		name, v := entry.key, entry.val
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
	if hasTraversal(f.Filename) {
		return fmt.Errorf("%w: filename %q", ErrFileFieldTraversal, f.Filename)
	}
	for _, entry := range []struct {
		key string
		val string
	}{
		{"url", f.URL},
		{"filename", f.Filename},
		{"storage_ref", f.StorageRef},
	} {
		name, v := entry.key, entry.val
		if i := indexControlByte(v); i >= 0 {
			return fmt.Errorf("%w: %s contains %#x at offset %d", ErrFileFieldControlBytes, name, v[i], i)
		}
	}

	for _, entry := range []struct {
		key string
		val string
	}{
		{"url", f.URL},
		{"filename", f.Filename},
		{"storage_ref", f.StorageRef},
	} {
		name, v := entry.key, entry.val
		if r, off := indexForgedRune(v); off >= 0 {
			return fmt.Errorf("%w: %s contains invisible or bidi rune U+%04X at offset %d",
				ErrFileFieldControlBytes, name, r, off)
		}
	}
	if !isSafeMIMEString(f.MimeType) {
		return fmt.Errorf("%w: %q", ErrFileFieldMimeUnsafe, f.MimeType)
	}
	return f.Image.Validate()
}

func isUnsafeURLScheme(url string) bool {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\n', '\r':
			return -1
		}
		return r
	}, url)
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

func indexControlByte(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return i
		}
	}
	return -1
}

func indexForgedRune(s string) (rune, int) {
	for i, r := range s {
		if textsafe.IsC1(r) || textsafe.IsInvisible(r) {
			return r, i
		}
	}
	return 0, -1
}

func hasTraversal(s string) bool {
	return strings.Contains(s, "..")
}

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
		case c == ';' || c == '=' || c == ' ':
		default:
			return false
		}
	}
	return true
}

// ProcessFileField reads file (capped at [MaxProcessFileSize]), refuses
// unsafe content sniffed from its bytes, saves it under [GenerateFilePath]
// and returns its FileField. Image metadata is kept unless [StripMetadata]
// is passed.
func ProcessFileField(ctx context.Context, store upload.Storage, file interface {
	Read([]byte) (int, error)
}, filename string, entityName, fieldName string, opts ...ProcessOption) (*FileField, error) {
	if store == nil {
		return nil, fmt.Errorf("filefield: storage backend is required")
	}
	var cfg processConfig
	for _, o := range opts {
		o(&cfg)
	}

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

	if err := rejectUnsafeContent(data); err != nil {
		return nil, err
	}
	if cfg.stripMetadata {
		stripped, err := stripImageMetadata(data)
		if err != nil {
			return nil, fmt.Errorf("filefield: stripping image metadata: %w", err)
		}
		if stripped != nil {
			data = stripped
			size = int64(len(data))
			if size > MaxProcessFileSize {
				return nil, fmt.Errorf("%w: %d bytes after metadata strip (max %d)",
					ErrFileFieldTooLarge, size, MaxProcessFileSize)
			}
		}
	}

	mimeType := http.DetectContentType(data)
	if cfg.allow != nil && !slices.Contains(cfg.allow, mimeBase(mimeType)) {
		return nil, fmt.Errorf("%w: %s", ErrFileFieldType, mimeBase(mimeType))
	}
	if mimeType == "application/octet-stream" {
		if detected := detectMIMEFromName(filename); detected != "" {
			mimeType = detected
		}
	}

	path := GenerateFilePath(entityName, fieldName, filename)

	if err := store.Save(ctx, path, bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("filefield: saving file: %w", err)
	}

	ff := &FileField{
		URL:        keyURLPath(path),
		Filename:   upload.SanitizeFilename(filename),
		MimeType:   mimeType,
		Size:       size,
		StorageRef: path,
	}

	if cfg.deriver != nil {
		rec := &recordingStore{Storage: store}
		derived, err := cfg.deriver.DeriveImage(ctx, rec, data, path)
		if err != nil {
			cleanupOrphans(ctx, store, path, derived, rec.written())
			return nil, fmt.Errorf("filefield: deriving image renditions: %w", err)
		}
		if err := derived.Validate(); err != nil {
			cleanupOrphans(ctx, store, path, derived, rec.written())
			return nil, fmt.Errorf("filefield: derived image metadata: %w", err)
		}
		ff.Image = derived
	}
	return ff, nil
}

func cleanupOrphans(ctx context.Context, store upload.Storage, primary string, derived *ImageDerivatives, written []string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	seen := map[string]bool{}
	del := func(key string) {
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		_ = store.Delete(ctx, key)
	}
	if derived != nil {
		for _, v := range derived.Variants {
			del(v.StorageRef)
		}
	}
	for _, key := range written {
		del(key)
	}
	del(primary)
}

type recordingStore struct {
	upload.Storage
	mu   sync.Mutex
	keys []string
}

func (r *recordingStore) Save(ctx context.Context, key string, src io.Reader) error {
	if err := r.Storage.Save(ctx, key, src); err != nil {
		return err
	}
	r.mu.Lock()
	r.keys = append(r.keys, key)
	r.mu.Unlock()
	return nil
}

func (r *recordingStore) Delete(ctx context.Context, key string) error {
	err := r.Storage.Delete(ctx, key)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	r.mu.Lock()
	r.keys = slices.DeleteFunc(r.keys, func(k string) bool { return k == key })
	r.mu.Unlock()
	return err
}

func (r *recordingStore) written() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.keys)
}

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

func mimeBase(t string) string {
	if i := strings.IndexByte(t, ';'); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

func rejectUnsafeContent(data []byte) error {
	head := data
	if len(head) > 512 {
		head = head[:512]
	}

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

	for _, tok := range hardActiveTokens {
		if containsTokenFold(data, tok) {
			return fmt.Errorf("%w: HTML/XML/SVG content", ErrFileFieldUnsafeContent)
		}
	}

	if !isConfirmedInertBinary(head) {
		trim := trimLeadingSpace(stripBOM(head))
		for _, tok := range softActiveTokens {
			if containsTokenFold(trim, tok) {
				return fmt.Errorf("%w: HTML/XML/SVG content", ErrFileFieldUnsafeContent)
			}
		}
	}

	if ct := mimeBase(http.DetectContentType(head)); isMarkupMIME(ct) {
		return fmt.Errorf("%w: %s", ErrFileFieldUnsafeContent, ct)
	}

	return rejectWideText(data)
}

func trimLeadingSpace(b []byte) []byte {
	return bytes.TrimLeft(b, " \t\r\n\f\v")
}

func isMarkupMIME(ct string) bool {
	switch ct {
	case "text/html", "image/svg+xml", "text/xml", "application/xml":
		return true
	}
	return false
}

func isConfirmedInertBinary(head []byte) bool {
	ct := http.DetectContentType(head)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	switch {
	case ct == "application/pdf":
		return true
	case strings.HasPrefix(ct, "image/") && ct != "image/svg+xml":
		return true
	case strings.HasPrefix(ct, "audio/"),
		strings.HasPrefix(ct, "video/"),
		strings.HasPrefix(ct, "font/"),
		ct == "application/font-woff",
		ct == "application/wasm",
		ct == "application/x-rar-compressed",
		ct == "application/zip",
		ct == "application/x-gzip",
		ct == "application/ogg":
		return true
	}
	return false
}

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

var hardActiveTokens = [][]byte{
	[]byte("<script"),
	[]byte("<iframe"),
	[]byte("<html"),
	[]byte("<!doctype"),
	[]byte("<object"),
	[]byte("<embed"),
	[]byte("<base"),
	[]byte("<svg"),
	[]byte("<math"),
}

var softActiveTokens = [][]byte{
	[]byte("<img"),
	[]byte("<?xml"),
	[]byte("<style"),
	[]byte("<link"),
	[]byte("javascript:"),
}

func containsTokenFold(haystack, needle []byte) bool {
	nl := len(needle)
	if nl == 0 {
		return true
	}
	hl := len(haystack)
	if hl < nl {
		return false
	}
	first := needle[0]
	firstUp := first
	if first >= 'a' && first <= 'z' {
		firstUp = first - 32
	}
	for i := 0; i+nl <= hl; i++ {
		c := haystack[i]
		if c != first && c != firstUp {
			continue
		}
		match := true
		for j := 1; j < nl; j++ {
			a := haystack[i+j]
			b := needle[j]
			if a == b {
				continue
			}
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if b >= 'A' && b <= 'Z' {
				b += 32
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// DeleteFileField deletes ff's renditions and then its stored object; an
// object already gone is not an error.
func DeleteFileField(ctx context.Context, store upload.Storage, ff *FileField) error {
	if store == nil {
		return fmt.Errorf("filefield: storage backend is required")
	}
	if ff == nil {
		return nil
	}
	var failed []string
	if ff.Image != nil {
		for _, v := range ff.Image.Variants {
			if v.StorageRef == "" || v.StorageRef == ff.StorageRef {
				continue
			}
			if err := store.Delete(ctx, v.StorageRef); err != nil && !errors.Is(err, os.ErrNotExist) {
				failed = append(failed, fmt.Sprintf("%s (%v)", v.StorageRef, err))
			}
		}
	}
	if ff.StorageRef != "" {
		if err := store.Delete(ctx, ff.StorageRef); err != nil && !errors.Is(err, os.ErrNotExist) {
			failed = append(failed, fmt.Sprintf("%s (%v)", ff.StorageRef, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("filefield: %d object(s) survive deletion and remain reachable: %s",
			len(failed), strings.Join(failed, "; "))
	}
	return nil
}

func keyURLPath(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// GenerateFilePath returns a unique storage key,
// uploads/{entityName}/{fieldName}/{unique name}.
func GenerateFilePath(entityName, fieldName, filename string) string {
	path := filepath.Join("uploads", entityName, fieldName, upload.UniqueFilename(filename))

	return filepath.ToSlash(path)
}

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
