package upload

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// maxMultipartMemory bounds how many bytes of a multipart request are
// buffered in memory before parts spill to temp files. It is deliberately
// small and independent of Config.MaxSize: passing MaxSize as the
// maxMemory arg to ParseMultipartForm only controls the RAM-vs-disk spill
// threshold, it does NOT cap the request body. The body cap is enforced
// separately via http.MaxBytesReader below.
const maxMultipartMemory = 1 << 20 // 1 MiB

// multipartFramingSlack is added to Config.MaxSize when wrapping the
// request body so the multipart boundary markers, part headers, and
// other framing bytes don't push a legitimately-sized file part over
// the body cap and trigger a spurious 413.
const multipartFramingSlack = 4 << 10 // 4 KiB

// UniqueFilename derives a collision-proof storage name from a client
// filename: the sanitized base name, a UnixNano timestamp, and 16 hex
// chars of crypto/rand. The random component is the real uniqueness
// guarantee: the timestamp is retained only for human-readable
// ordering, because two requests landing on the same clock tick must
// still map to different objects (Storage.Save implementations open
// with O_TRUNC, so a colliding key silently overwrites). This is the
// single unique-key generator the framework's upload surfaces share:
// framework/file.GenerateFilePath builds its entity/field-scoped paths
// on top of it; do not grow a second scheme.
//
// There is no error return: crypto/rand.Read cannot fail (see
// randomSuffix), so the random component is always present.
func UniqueFilename(filename string) string {
	safe := SanitizeFilename(filename)
	ext := filepath.Ext(safe)
	name := strings.TrimSuffix(safe, ext)
	if name == "" {
		name = "upload"
	}
	return fmt.Sprintf("%s_%d_%s%s", name, time.Now().UnixNano(), randomSuffix(), ext)
}

// randomSuffix returns a hex-encoded 8-byte crypto-random token. It has no
// error path because crypto/rand.Read has none: it "never returns an
// error, and always fills b entirely", crashing the program irrecoverably
// if the OS source fails. So there is no reachable state where the suffix
// is missing and two requests on the same clock tick could collide into an
// O_TRUNC clobber, which is the whole reason the suffix exists.
func randomSuffix() string {
	var b [8]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Storage defines the interface for file storage backends.
type Storage interface {
	Save(ctx context.Context, key string, r io.Reader) error
	Delete(ctx context.Context, key string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Exists(ctx context.Context, key string) (bool, error)
}

// Metadata holds information about an uploaded file.
type Metadata struct {
	OriginalName string    `json:"originalName"`
	Size         int64     `json:"size"`
	MimeType     string    `json:"mimeType"`
	UploadedAt   time.Time `json:"uploadedAt"`
	Key          string    `json:"key"`
}

// Config holds configuration for the upload handler.
type Config struct {
	MaxSize      int64    // Maximum file size in bytes (0 = no limit)
	AllowedTypes []string // MIME type whitelist (empty = allow all)
	AllowedExts  []string // Extension whitelist (empty = allow all)
	Storage      Storage  // Storage backend implementation
}

// Handler returns an http.HandlerFunc that processes multipart file uploads.
// It expects a single file in the "file" form field.
// On success it responds with 200 and JSON Metadata.
func Handler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Bound the request body before parsing so an attacker can't
		// force the whole multipart payload to be buffered/spilled to
		// disk before the size check runs. MaxBytesReader caps total
		// body bytes (with a small slack for multipart framing); the
		// maxMemory arg to ParseMultipartForm is a separate RAM-vs-disk
		// spill threshold, not a body cap.
		if cfg.MaxSize > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxSize+multipartFramingSlack)
		}

		// Parse multipart form. Use a small fixed in-memory threshold so
		// large parts spill predictably; do NOT pass MaxSize here.
		if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "failed to parse form", http.StatusBadRequest)
			return
		}
		// Remove any temp files multipart spilled to disk on every return
		// path, including the validation-reject paths below. The net/http
		// server does not delete these deterministically.
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "missing file field", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Validate extension
		if err := ValidateExt(header.Filename, cfg.AllowedExts); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Validate size
		if err := ValidateSize(header.Size, cfg.MaxSize); err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}

		// Validate MIME type
		if err := ValidateMIME(file, cfg.AllowedTypes); err != nil {
			http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
			return
		}

		// Sniff the actual content type so stored metadata reflects what
		// the file *is*, not what the client claimed in the multipart
		// header. A misleading Content-Type / extension is a standard
		// MIME-spoofing primitive (HTML uploaded as image/png, etc.):
		// stored metadata must never echo the attacker-controlled value
		// back to downstream consumers.
		detectedMime, err := sniffContentType(file)
		if err != nil {
			http.Error(w, "failed to sniff content", http.StatusBadRequest)
			return
		}

		// Storage key: the sanitized filename PLUS a unique timestamp/rand
		// component. The bare sanitized filename keyed objects per-client-
		// name, and Storage.Save implementations open O_TRUNC: two users
		// uploading report.txt silently overwrote each other. UniqueFilename
		// is the same generator the auto-CRUD path builds on
		// (framework/file.GenerateFilePath).
		key := UniqueFilename(header.Filename)

		// Save via storage backend
		if err := cfg.Storage.Save(r.Context(), key, file); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}

		meta := Metadata{
			OriginalName: header.Filename,
			Size:         header.Size,
			MimeType:     detectedMime,
			UploadedAt:   time.Now().UTC(),
			Key:          key,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(meta)
	}
}

// sniffContentType reads the first 512 bytes of f to detect its content
// type via [http.DetectContentType], then rewinds f so the storage
// backend reads the full payload. The detected type is what the metadata
// must record, never the attacker-controlled multipart Content-Type.
func sniffContentType(f io.ReadSeeker) (string, error) {
	buf := make([]byte, 512)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return http.DetectContentType(buf[:n]), nil
}

// ext returns the lowercase file extension (without dot) from a filename.
func ext(filename string) string {
	e := filepath.Ext(filename)
	if len(e) > 0 && e[0] == '.' {
		return e[1:]
	}
	return e
}
