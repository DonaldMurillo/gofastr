package upload

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"
)

// Storage defines the interface for file storage backends.
type Storage interface {
	Save(ctx context.Context, key string, r io.Reader) error
	Delete(ctx context.Context, key string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Exists(ctx context.Context, key string) (bool, error)
}

// Metadata holds information about an uploaded file.
type Metadata struct {
	OriginalName string    `json:"original_name"`
	Size         int64     `json:"size"`
	MimeType     string    `json:"mime_type"`
	UploadedAt   time.Time `json:"uploaded_at"`
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

		// Parse multipart form
		if err := r.ParseMultipartForm(cfg.MaxSize); err != nil {
			http.Error(w, "failed to parse form", http.StatusBadRequest)
			return
		}

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

		// Sanitize filename for storage key
		safeName := SanitizeFilename(header.Filename)
		key := safeName
		if key == "" {
			key = fmt.Sprintf("upload_%d", time.Now().UnixNano())
		}

		// Save via storage backend
		if err := cfg.Storage.Save(r.Context(), key, file); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}

		meta := Metadata{
			OriginalName: header.Filename,
			Size:         header.Size,
			MimeType:     header.Header.Get("Content-Type"),
			UploadedAt:   time.Now().UTC(),
			Key:          key,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(meta)
	}
}

// ext returns the lowercase file extension (without dot) from a filename.
func ext(filename string) string {
	e := filepath.Ext(filename)
	if len(e) > 0 && e[0] == '.' {
		return e[1:]
	}
	return e
}
