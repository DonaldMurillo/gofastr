package crud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

// MaxMultipartMemory is the in-memory portion of a multipart upload before
// spilling to disk. Files larger than this still upload, they just stream
// through a temp file.
const MaxMultipartMemory = 32 << 20 // 32 MiB

// MaxMultipartBodyBytes caps the total wire size of a multipart request
// accepted by the CRUD write handlers. MaxMultipartMemory above is the
// in-RAM spill threshold, not a body cap, without a wire cap a hostile
// client can stream an unbounded body into parser temp files (and, via
// non-file form values, straight into memory). 64 MiB leaves headroom
// over the 32 MiB per-file ProcessFileField cap for framing and form
// fields. Requests over the cap are rejected 413.
const MaxMultipartBodyBytes int64 = 64 << 20 // 64 MiB

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
// Parsed with mime.ParseMediaType, media types are case-insensitive
// (RFC 9110 §8.3.1) and lowercased by the parser, so this agrees with
// enforceJSONContentType for a header like `Multipart/Form-Data; …`. A
// case-sensitive prefix check here let such a request through the gate
// but not the multipart branch, so the JSON 1 MiB body cap applied and
// >1 MiB uploads failed with a bogus 400.
func isMultipart(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "multipart/form-data"
}

// enforceJSONContentType refuses requests whose Content-Type isn't either
// application/json or multipart/form-data. Returns errUnsupportedMediaType
// for text/plain, application/x-www-form-urlencoded, missing, or any other
// type, these are the "simple-request" content types a browser can send
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

// limitRequestBody wraps r.Body with the http.MaxBytesReader matching the
// request's Content-Type: JSON bodies cap at MaxJSONBodyBytes, multipart
// bodies at MaxMultipartBodyBytes. One cap for both would break whichever
// side it doesn't fit, a JSON-sized cap rejects every multipart upload
// above 1 MiB (multipart framing plus file parts dwarf a JSON record), and
// a multipart-sized cap would let unauthenticated callers pin ~64 MiB of
// parser memory with a JSON body. Callers map the resulting
// errBodyTooLarge to 413.
func limitRequestBody(w http.ResponseWriter, r *http.Request) {
	if isMultipart(r) {
		r.Body = http.MaxBytesReader(w, r.Body, MaxMultipartBodyBytes)
		return
	}
	limitJSONBody(w, r)
}

// limitJSONBody wraps r.Body with http.MaxBytesReader so JSON decoding
// caps at MaxJSONBodyBytes. The wrapped body is installed back onto the
// request so other readers see the same limit.
func limitJSONBody(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
}

// readBodyBytes drains r.Body (already wrapped by limitRequestBody) into
// memory, mapping the body-limit error to errBodyTooLarge. Strict key
// validation needs the raw bytes; the 1 MiB JSON cap bounds the buffer.
func readBodyBytes(r *http.Request) ([]byte, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return nil, errBodyTooLarge
		}
		// http.MaxBytesReader may also report a generic
		// "http: request body too large" error string on some Go
		// versions / paths, match by substring as a fallback.
		if strings.Contains(err.Error(), "request body too large") {
			return nil, errBodyTooLarge
		}
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return data, nil
}

// decodeJSONBody decodes r.Body into v with the request's body already
// wrapped by limitJSONBody, through handler.UnmarshalStrict so duplicate
// and case-folded top-level keys are refused instead of silently resolved.
// Returns errBodyTooLarge if the limit fired, or a 400 *handler.Error
// otherwise. The caller must have applied limitJSONBody first.
func decodeJSONBody(r *http.Request, v any) error {
	data, err := readBodyBytes(r)
	if err != nil {
		return err
	}
	return handler.UnmarshalStrict(data, v)
}

// readRequestBody decodes the incoming request into a snake_cased body map.
// Multipart requests run through parseMultipartBody (no JSON casing conversion,
// multipart field names are taken literally as DB column names); JSON
// requests are decoded and reverse-cased back to snake_case so they match the
// schema's field names regardless of the wire casing.
//
// JSON bodies are checked with crud's own key fold BEFORE the decode: two
// distinct wire keys that resolve to one column (CaseCamel's "bodyText" and
// "body_text", or a case-folded pair) are refused with 400 rather than
// resolved by map iteration order, which made the stored value
// nondeterministic per request. Unknown keys that collide with nothing still
// pass through; that contract is pinned by TestWireName_RoundTripsBothCasings.
//
// Pre-condition: the caller has already validated Content-Type via
// enforceJSONContentType and wrapped r.Body with limitRequestBody.
func (ch *CrudHandler) readRequestBody(r *http.Request) (map[string]any, error) {
	if isMultipart(r) {
		return ch.parseMultipartBody(r)
	}
	data, err := readBodyBytes(r)
	if err != nil {
		return nil, err
	}
	if err := handler.CheckTopLevelKeys(data, ch.wireKeyColumn); err != nil {
		return nil, err
	}
	var body map[string]any
	if err := handler.UnmarshalStrict(data, &body); err != nil {
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
// should validate Content-Type with isMultipart first and wrap r.Body with
// limitRequestBody so the multipart wire cap applies.
func (ch *CrudHandler) parseMultipartBody(r *http.Request) (map[string]any, error) {
	if err := r.ParseMultipartForm(MaxMultipartMemory); err != nil {
		// An over-cap body is a size problem, not a malformed request:
		// map it to errBodyTooLarge so callers answer 413 (matching the
		// JSON path) instead of a 400 the client would retry verbatim.
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) || strings.Contains(err.Error(), "request body too large") {
			return nil, errBodyTooLarge
		}
		return nil, fmt.Errorf("parse multipart: %w", err)
	}

	body := make(map[string]any)

	fileFieldNames := make(map[string]schema.FieldType, len(ch.Entity.GetFields()))
	for _, f := range ch.Entity.GetFields() {
		switch f.Type {
		case schema.Image, schema.File:
			fileFieldNames[f.Name] = f.Type
		default:
			// Not a file-bearing field: nothing to collect.
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
			if err := saveFilePart(r.Context(), ch, key, fileFieldNames[key], fh, body); err != nil {
				return nil, err
			}
		}
	}

	return body, nil
}

// saveFilePart opens one multipart file header, runs ProcessFileField, and
// stores the resulting URL on body[key]. For a schema.Image field with an
// ImageDeriver configured, renditions and placeholder metadata are derived
// too and spread across whichever sibling columns the entity declares.
func saveFilePart(ctx context.Context, ch *CrudHandler, key string, fieldType schema.FieldType, fh *multipart.FileHeader, body map[string]any) error {
	f, err := fh.Open()
	if err != nil {
		return fmt.Errorf("open file part %q: %w", key, err)
	}
	defer f.Close()

	var opts []file.ProcessOption
	// Only Image fields get the pipeline. A File field is any binary, a
	// PDF, a CSV, and decoding it as an image would fail every upload.
	if fieldType == schema.Image {
		if d := ch.deriverFor(key); d != nil {
			opts = append(opts, file.WithImageDeriver(d))
		}
	}

	ff, err := file.ProcessFileField(ctx, ch.Storage, f, fh.Filename, ch.Entity.GetName(), key, opts...)
	if err != nil {
		return fmt.Errorf("upload %q: %w", key, err)
	}
	body[key] = ff.URL
	if ff.Image != nil {
		applyDerivedColumns(ch.Entity, key, ff.Image, body)
	}
	return nil
}

// deriverFor resolves the deriver for one image field: a per-field override
// when present, otherwise the handler-wide default. A per-field entry of nil
// is honoured as "no pipeline for this field", so a single noisy field can be
// opted out without unpicking the app-wide config.
func (ch *CrudHandler) deriverFor(field string) file.ImageDeriver {
	if ch.FieldImageDerivers != nil {
		if d, ok := ch.FieldImageDerivers[field]; ok {
			return d
		}
	}
	return ch.ImageDeriver
}

// derivedColumnSuffixes maps each derived artifact to the sibling column
// that receives it. An Image field named "cover" populates
// "cover_blurhash", "cover_placeholder", and "cover_variants", but only
// those the entity actually declares, so adopting one is a matter of adding
// the column and nothing else. Columns that do not exist are skipped
// silently; that is what makes this additive rather than a schema
// requirement.
var derivedColumnSuffixes = struct {
	blurHash    string
	placeholder string
	variants    string
}{
	blurHash:    "_blurhash",
	placeholder: "_placeholder",
	variants:    "_variants",
}

// applyDerivedColumns writes derived values onto body for the sibling
// columns the entity declares.
func applyDerivedColumns(ent *entity.Entity, field string, d *file.ImageDerivatives, body map[string]any) {
	declared := make(map[string]schema.FieldType, len(ent.GetFields()))
	for _, f := range ent.GetFields() {
		declared[f.Name] = f.Type
	}
	set := func(suffix string, value any) {
		name := field + suffix
		if _, ok := declared[name]; !ok {
			return
		}
		body[name] = value
	}
	if d.BlurHash != "" {
		set(derivedColumnSuffixes.blurHash, d.BlurHash)
	}
	if d.Placeholder != "" {
		set(derivedColumnSuffixes.placeholder, d.Placeholder)
	}
	if len(d.Variants) > 0 {
		// Marshalled here rather than handed over as a slice: the column is
		// declared schema.JSON, and the write path expects a value the
		// driver can bind directly.
		if raw, err := json.Marshal(d.Variants); err == nil {
			set(derivedColumnSuffixes.variants, string(raw))
		}
	}
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
		default:
			// Everything else stays the raw form string. A new type that
			// needs coercion belongs above; landing here silently means
			// the value reaches the driver as text.
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
// stored value flows into HTML attributes and HTTP redirects later,
// any scheme not on this list becomes a phishing / XSS / SSRF vector
// when rendered.
//
// The scheme allow-list is core-ui/urlsafe's Resource policy (http(s) plus
// relative references), not a local copy of it. The traversal check on top is
// storage-specific: a stored key must not climb out of the storage root, and
// urlsafe deliberately allows "../" because a relative href legitimately may.
func isSafeMediaURL(s string) bool {
	if strings.Contains(s, "..") {
		return false
	}
	return urlsafe.OK(s, urlsafe.Resource)
}
