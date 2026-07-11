package crud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

// MaxMultipartMemory is the in-memory portion of a multipart upload before
// spilling to disk. Files larger than this still upload — they just stream
// through a temp file.
const MaxMultipartMemory = 32 << 20 // 32 MiB

// MaxJSONBodyBytes caps the size of a JSON request body the CRUD handlers
// will read. 1 MiB is large enough for any realistic single record or
// batch envelope, while bounding the memory an unauthenticated caller can
// pin per request.
const MaxJSONBodyBytes int64 = 1 << 20 // 1 MiB

// errStorageNotConfigured is returned when a request includes file parts but
// the handler has no storage backend.
var errStorageNotConfigured = errors.New("server has no file storage configured")

// errUnsupportedMediaType is returned by enforceJSONContentType when the
// caller sends a JSON-only endpoint a body without a JSON Content-Type.
var errUnsupportedMediaType = errors.New("unsupported media type")

// errBodyTooLarge is returned by the JSON decoder when the body exceeds
// MaxJSONBodyBytes. Callers map it to 413 Request Entity Too Large.
var errBodyTooLarge = errors.New("request body too large")

// isMultipart reports whether the request carries a multipart/form-data body.
func isMultipart(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "multipart/form-data")
}

// enforceJSONContentType refuses requests whose Content-Type isn't either
// application/json or multipart/form-data. Returns errUnsupportedMediaType
// for text/plain, application/x-www-form-urlencoded, missing, or any other
// type — these are the "simple-request" content types a browser can send
// cross-origin without a CORS preflight, so accepting them on JSON-only
// write endpoints opens a CSRF vector.
func enforceJSONContentType(r *http.Request) error {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return errUnsupportedMediaType
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return errUnsupportedMediaType
	}
	switch mediaType {
	case "application/json", "multipart/form-data":
		return nil
	}
	return errUnsupportedMediaType
}

// limitJSONBody wraps r.Body with http.MaxBytesReader so JSON decoding
// caps at MaxJSONBodyBytes. The wrapped body is installed back onto the
// request so other readers see the same limit.
func limitJSONBody(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
}

// decodeJSONBody decodes r.Body into v with the request's body already
// wrapped by limitJSONBody. Returns errBodyTooLarge if the limit fired,
// or a generic JSON error otherwise. The caller must have applied
// limitJSONBody first.
func decodeJSONBody(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return errBodyTooLarge
		}
		// http.MaxBytesReader may also report a generic
		// "http: request body too large" error string on some Go
		// versions / paths — match by substring as a fallback.
		if strings.Contains(err.Error(), "request body too large") {
			return errBodyTooLarge
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// readRequestBody decodes the incoming request into a snake_cased body map.
// Multipart requests run through parseMultipartBody (no JSON casing conversion
// — multipart field names are taken literally as DB column names); JSON
// requests are decoded and reverse-cased back to snake_case so they match the
// schema's field names regardless of the wire casing.
//
// Pre-condition: the caller has already validated Content-Type via
// enforceJSONContentType and (for JSON) wrapped r.Body with limitJSONBody.
func (ch *CrudHandler) readRequestBody(r *http.Request) (map[string]any, error) {
	if isMultipart(r) {
		return ch.parseMultipartBody(r)
	}
	var body map[string]any
	if err := decodeJSONBody(r, &body); err != nil {
		return nil, err
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
	ff, err := file.ProcessFileField(ctx, ch.Storage, f, fh.Filename, ch.Entity.GetName(), key)
	if err != nil {
		return fmt.Errorf("upload %q: %w", key, err)
	}
	body[key] = ff.URL
	return nil
}

// coerceFormValue attempts a minimal type coercion based on the schema field
// type so an int or bool field doesn't end up as a string. Unknown fields and
// String/Text/Enum stay as strings.
func coerceFormValue(ent *entity.Entity, name, raw string) any {
	for _, f := range ent.GetFields() {
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

// validateMediaURLs scans body for fields whose schema declares Image or
// File and refuses unsafe URL shapes. The multipart upload path runs
// uploaded files through a sniffer; the JSON path stores whatever
// string the caller supplied, which becomes an `<img src>` / `<a href>`
// later. A `javascript:`/`data:`/`file:` value there is stored XSS;
// path-traversal (`../etc/passwd`) bypasses the storage's path scope.
// Only http(s) URLs, relative paths within the upload tree, and bare
// filenames survive.
func (ch *CrudHandler) validateMediaURLs(body map[string]any) error {
	for _, f := range ch.Entity.GetFields() {
		switch f.Type {
		case schema.Image, schema.File:
		default:
			continue
		}
		raw, ok := body[f.Name]
		if !ok {
			continue
		}
		s, ok := raw.(string)
		if !ok || s == "" {
			continue
		}
		if !isSafeMediaURL(s) {
			return &ValidationError{fields: map[string][]string{f.Name: {"unsafe URL or path"}}}
		}
	}
	return nil
}

// isSafeMediaURL is true for URLs / paths that may be persisted into an
// Image or File field. Allow-list (rather than block-list) because the
// stored value flows into HTML attributes and HTTP redirects later —
// any scheme not on this list becomes a phishing / XSS / SSRF vector
// when rendered.
func isSafeMediaURL(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return false
		}
	}
	low := strings.ToLower(s)
	// Percent-encoded CR/LF tries to smuggle a header line through
	// downstream consumers that re-encode the URL.
	if strings.Contains(low, "%0d") || strings.Contains(low, "%0a") {
		return false
	}
	// Path traversal escapes the storage root.
	if strings.Contains(s, "..") {
		return false
	}
	// Protocol-relative URLs are ambiguous about origin trust.
	if strings.HasPrefix(s, "//") {
		return false
	}
	// Relative paths (no scheme) are fine — they live under the storage
	// prefix.
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ':' {
			scheme := strings.ToLower(s[:i])
			switch scheme {
			case "http", "https":
				return true
			default:
				return false
			}
		}
		if c == '/' || c == '?' || c == '#' || c == '.' {
			// Hit a non-scheme delimiter first — relative path.
			return true
		}
	}
	// No colon at all — bare filename.
	return true
}
