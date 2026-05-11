package framework

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/gofastr/gofastr/core/schema"
)

// MaxMultipartMemory is the in-memory portion of a multipart upload before
// spilling to disk. Files larger than this still upload — they just stream
// through a temp file.
const MaxMultipartMemory = 32 << 20 // 32 MiB

// errStorageNotConfigured is returned when a request includes file parts but
// the handler has no storage backend.
var errStorageNotConfigured = errors.New("server has no file storage configured")

// isMultipart reports whether the request carries a multipart/form-data body.
func isMultipart(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "multipart/form-data")
}

// readRequestBody decodes the incoming request into a snake_cased body map.
// Multipart requests run through parseMultipartBody (no JSON casing conversion
// — multipart field names are taken literally as DB column names); JSON
// requests are decoded and reverse-cased back to snake_case so they match the
// schema's field names regardless of the wire casing.
func (ch *CrudHandler) readRequestBody(r *http.Request) (map[string]any, error) {
	if isMultipart(r) {
		return ch.parseMultipartBody(r)
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return ch.unconvertMapKeys(body), nil
}

// parseMultipartBody reads a multipart request and returns a body map suitable
// for the do* CRUD primitives. File parts whose name matches an Image/File
// field on the entity are saved through the handler's Storage and replaced
// with the resulting URL string. All other form values are mapped onto fields
// by name with type coercion driven by the schema (Int/Float/Bool).
//
// The handler must have Storage set; otherwise the function errors. Callers
// should validate Content-Type with isMultipart first.
func (ch *CrudHandler) parseMultipartBody(r *http.Request) (map[string]any, error) {
	if err := r.ParseMultipartForm(MaxMultipartMemory); err != nil {
		return nil, fmt.Errorf("parse multipart: %w", err)
	}

	body := make(map[string]any)

	fileFieldNames := make(map[string]schema.FieldType, len(ch.Entity.GetFields()))
	for _, f := range ch.Entity.GetFields() {
		switch f.Type {
		case schema.Image, schema.File:
			fileFieldNames[f.Name] = f.Type
		}
	}

	if r.MultipartForm != nil {
		// Plain form values first
		for key, vals := range r.MultipartForm.Value {
			if len(vals) == 0 {
				continue
			}
			body[key] = coerceFormValue(ch.Entity, key, vals[0])
		}

		// File parts override values when the same key is present
		for key, headers := range r.MultipartForm.File {
			if _, isFileField := fileFieldNames[key]; !isFileField {
				continue
			}
			if len(headers) == 0 {
				continue
			}
			if ch.Storage == nil {
				return nil, errStorageNotConfigured
			}
			fh := headers[0]
			if err := saveFilePart(r.Context(), ch, key, fh, body); err != nil {
				return nil, err
			}
		}
	}

	return body, nil
}

// saveFilePart opens one multipart file header, runs ProcessFileField, and
// stores the resulting URL on body[key].
func saveFilePart(ctx context.Context, ch *CrudHandler, key string, fh *multipart.FileHeader, body map[string]any) error {
	f, err := fh.Open()
	if err != nil {
		return fmt.Errorf("open file part %q: %w", key, err)
	}
	defer f.Close()
	ff, err := ProcessFileField(ctx, ch.Storage, f, fh.Filename, ch.Entity.GetName(), key)
	if err != nil {
		return fmt.Errorf("upload %q: %w", key, err)
	}
	body[key] = ff.URL
	return nil
}

// coerceFormValue attempts a minimal type coercion based on the schema field
// type so an int or bool field doesn't end up as a string. Unknown fields and
// String/Text/Enum stay as strings.
func coerceFormValue(entity *Entity, name, raw string) any {
	for _, f := range entity.GetFields() {
		if f.Name != name {
			continue
		}
		switch f.Type {
		case schema.Int:
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
				return n
			}
		case schema.Float, schema.Decimal:
			if n, err := strconv.ParseFloat(raw, 64); err == nil {
				return n
			}
		case schema.Bool:
			switch strings.ToLower(raw) {
			case "true", "1", "yes", "on":
				return true
			case "false", "0", "no", "off", "":
				return false
			}
		}
		return raw
	}
	return raw
}
