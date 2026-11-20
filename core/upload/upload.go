package upload

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

const maxMultipartMemory = 1 << 20

const multipartFramingSlack = 4 << 10

// DefaultMaxSize is Handler's cap when Config.MaxSize is zero or negative.
const DefaultMaxSize int64 = 32 << 20

// UniqueFilename returns SanitizeFilename(filename) with a UnixNano
// timestamp and 16 random hex characters before its extension, cut to fit
// MaxFilenameBytes.
func UniqueFilename(filename string) string {
	safe := SanitizeFilename(filename)
	ext := filepath.Ext(safe)
	name := strings.TrimSuffix(safe, ext)
	if name == "" {
		name = "upload"
	}
	suffix := fmt.Sprintf("_%d_%s", time.Now().UnixNano(), randomSuffix())
	budget := MaxFilenameBytes - len(suffix)
	if len(name)+len(ext) > budget {
		if len(ext) >= budget {
			ext = ""
		}
		name = truncateRunes(name, budget-len(ext))
		if name == "" {
			name = "u"
		}
	}
	return name + suffix + ext
}

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
	MaxSize      int64    // bytes; 0 or less means DefaultMaxSize
	AllowedTypes []string // sniffed MIME types allowed; empty allows all
	AllowedExts  []string // extensions allowed; empty allows all
	Storage      Storage
}

// Handler stores the multipart "file" field of a POST under a
// UniqueFilename key and answers 200 with its JSON Metadata.
func Handler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
			return
		}

		maxSize := cfg.MaxSize
		if maxSize <= 0 {
			maxSize = DefaultMaxSize
		}
		bodyCap := maxSize
		if maxSize <= math.MaxInt64-multipartFramingSlack {
			bodyCap = maxSize + multipartFramingSlack
		}
		r.Body = http.MaxBytesReader(w, r.Body, bodyCap)

		if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				i18n.HTTPError(w, r, http.StatusRequestEntityTooLarge, reasonFileTooLarge)
				return
			}
			i18n.HTTPError(w, r, http.StatusBadRequest, i18n.ReasonInvalidForm)
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()

		file, header, err := r.FormFile("file")
		if err != nil {
			i18n.HTTPError(w, r, http.StatusBadRequest, reasonMissingFile)
			return
		}
		defer file.Close()

		if err := ValidateExt(header.Filename, cfg.AllowedExts); err != nil {
			i18n.HTTPErrorFor(w, r, http.StatusBadRequest, err)
			return
		}

		if err := ValidateSize(header.Size, maxSize); err != nil {
			i18n.HTTPErrorFor(w, r, http.StatusRequestEntityTooLarge, err)
			return
		}

		if err := ValidateMIME(file, cfg.AllowedTypes); err != nil {
			i18n.HTTPErrorFor(w, r, http.StatusUnsupportedMediaType, err)
			return
		}

		detectedMime, err := sniffContentType(file)
		if err != nil {
			i18n.HTTPError(w, r, http.StatusBadRequest, reasonSniffFailed)
			return
		}

		key := UniqueFilename(header.Filename)

		if err := cfg.Storage.Save(r.Context(), key, file); err != nil {
			i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
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

func ext(filename string) string {
	e := filepath.Ext(filename)
	if len(e) > 0 && e[0] == '.' {
		e = e[1:]
	}
	return strings.ToLower(e)
}
