package crud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/file"
	"github.com/DonaldMurillo/gofastr/internal/bodylimit"
)

// MaxMultipartMemory is how much of a multipart body is held in memory
// before the rest spills to a temp file.
const MaxMultipartMemory = 32 << 20 // 32 MiB

// MaxMultipartBodyBytes caps the wire size of a multipart write; a larger
// body answers 413.
const MaxMultipartBodyBytes int64 = 64 << 20 // 64 MiB

// MaxJSONBodyBytes caps the size of a JSON write body; a larger body answers 413.
const MaxJSONBodyBytes = bodylimit.MaxJSONBodyBytes

var errStorageNotConfigured = errors.New("server has no file storage configured")

var errUnsupportedMediaType = errors.New("unsupported media type")

var errBodyTooLarge = errors.New("request body too large")

func isMultipart(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "multipart/form-data"
}

var errCrossSiteForm = errors.New("cross-site form write")

// enforceJSONContentType admits application/json, and multipart/form-data
// that passes the cross-site guard.
func enforceJSONContentType(r *http.Request) error {
	mediaType, err := writeMediaType(r)
	if err != nil {
		return err
	}
	switch mediaType {
	case "application/json":
		return nil
	case "multipart/form-data":
		if handler.IsCrossSiteRequest(r) {
			return errCrossSiteForm
		}
		return nil
	}
	return errUnsupportedMediaType
}

// enforceJSONOnly admits application/json only, for _batch and transitions.
func enforceJSONOnly(r *http.Request) error {
	mediaType, err := writeMediaType(r)
	if err != nil || mediaType != "application/json" {
		return errUnsupportedMediaType
	}
	return nil
}

func writeMediaType(r *http.Request) (string, error) {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return "", errUnsupportedMediaType
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return "", errUnsupportedMediaType
	}
	return mediaType, nil
}

func writeContentTypeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errCrossSiteForm) {
		writeJSONError(w, r, http.StatusForbidden, handler.ReasonCrossSiteRefused)
		return
	}
	writeJSONError(w, r, http.StatusUnsupportedMediaType, i18n.ReasonJSONMediaType)
}

func limitRequestBody(w http.ResponseWriter, r *http.Request) {
	if isMultipart(r) {
		r.Body = http.MaxBytesReader(w, r.Body, MaxMultipartBodyBytes)
		return
	}
	limitJSONBody(w, r)
}

func limitJSONBody(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
}

// readBodyBytes reads a capped r.Body; it refuses a body that is not UTF-8.
func readBodyBytes(r *http.Request) ([]byte, error) {
	//gofastr:allow(unboundedbody) every production caller caps the body first (limitRequestBody); decodeJSONBody is test-only
	data, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return nil, errBodyTooLarge
		}
		if strings.Contains(err.Error(), "request body too large") {
			return nil, errBodyTooLarge
		}
		return nil, bodyReadError(err)
	}
	if err := handler.CheckUTF8(data); err != nil {
		return nil, err
	}
	return data, nil
}

// bodyDecodeError is an invalid_json error with a phrased detail.
func bodyDecodeError(err error) error {
	return &i18n.Error{Reason: handler.ReasonInvalidJSON, Params: map[string]any{"detail": handler.JSONErrorDetail(err)}, Err: err}
}

func bodyReadError(err error) error {
	return &i18n.Error{Reason: reasonInvalidBody, Params: map[string]any{"detail": handler.BodyReadDetail(err)}, Err: err}
}

// decodeJSONBody decodes a capped r.Body into v with handler.UnmarshalStrict.
func decodeJSONBody(r *http.Request, v any) error {
	data, err := readBodyBytes(r)
	if err != nil {
		return err
	}
	return handler.UnmarshalStrict(data, v)
}

// readRequestBody decodes a JSON or multipart write body into a map keyed by
// column, and returns the storage keys a multipart parse saved, which the
// caller deletes (deleteSavedUploads) if the write then fails.
func (ch *CrudHandler) readRequestBody(r *http.Request) (map[string]any, []string, error) {
	if isMultipart(r) {
		return ch.parseMultipartBody(r)
	}
	data, err := readBodyBytes(r)
	if err != nil {
		return nil, nil, err
	}
	if err := handler.CheckTopLevelKeys(data, ch.wireKeyColumn); err != nil {
		return nil, nil, err
	}
	body, err := decodeStrictUseNumber(data)
	if err != nil {
		return nil, nil, err
	}
	return ch.unconvertMapKeys(body), nil, nil
}

// parseMultipartBody is readRequestBody for a multipart body. On error it
// still returns the keys saved so far.
func (ch *CrudHandler) parseMultipartBody(r *http.Request) (map[string]any, []string, error) {
	if err := r.ParseMultipartForm(MaxMultipartMemory); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) || strings.Contains(err.Error(), "request body too large") {
			return nil, nil, errBodyTooLarge
		}
		return nil, nil, &i18n.Error{Reason: reasonBadMultipart, Err: err}
	}

	body := make(map[string]any)

	fileFieldNames := make(map[string]schema.FieldType, len(ch.snapshotFields()))
	for _, f := range ch.snapshotFields() {
		if (f.ReadOnly || f.Hidden) && !serverWrites(r.Context()) {
			continue
		}
		switch f.Type {
		case schema.Image, schema.File:
			fileFieldNames[f.Name] = f.Type
		default:
		}
	}

	var rec *savedKeyLedger
	if r.MultipartForm != nil {
		keys := make(map[string]any, len(r.MultipartForm.Value)+len(r.MultipartForm.File))
		for key := range r.MultipartForm.Value {
			keys[key] = nil
		}
		for key := range r.MultipartForm.File {
			keys[key] = nil
		}
		if err := ch.itemKeyFoldsUnambiguous(keys); err != nil {
			return nil, nil, err
		}
		for _, key := range slices.Sorted(maps.Keys(r.MultipartForm.Value)) {
			vals := r.MultipartForm.Value[key]
			column := ch.wireKeyColumn(key)
			if len(vals) == 0 {
				continue
			}
			if len(vals) == 2 && vals[0] == "false" && ch.isBoolColumn(column) {
				body[column] = coerceFormValue(ch.Entity, column, vals[1])
				continue
			}
			if len(vals) == 1 {
				body[column] = coerceFormValue(ch.Entity, column, vals[0])
				continue
			}
			if ch.isJSONColumn(column) {
				list := make([]any, len(vals))
				for i, v := range vals {
					list[i] = v
				}
				body[column] = list
				continue
			}
			return nil, rec.saved(), i18n.NewError(reasonDupFormKey, map[string]any{"key": strconv.Quote(key)})
		}

		for _, key := range slices.Sorted(maps.Keys(r.MultipartForm.File)) {
			headers := r.MultipartForm.File[key]
			column := ch.wireKeyColumn(key)
			if _, isFileField := fileFieldNames[column]; !isFileField {
				continue
			}
			if len(headers) == 0 {
				continue
			}
			if len(headers) > 1 {
				return nil, rec.saved(), i18n.NewError(reasonDupFilePart, map[string]any{"key": strconv.Quote(key)})
			}
			if ch.Storage == nil {
				return nil, rec.saved(), errStorageNotConfigured
			}
			if rec == nil {
				rec = &savedKeyLedger{Storage: ch.Storage}
			}
			fh := headers[0]
			if err := saveFilePart(r.Context(), ch, rec, column, fileFieldNames[column], fh, body); err != nil {
				return nil, rec.saved(), err
			}
		}
	}

	return body, rec.saved(), nil
}

func (ch *CrudHandler) isBoolColumn(name string) bool {
	for _, f := range ch.snapshotFields() {
		if f.Name == name {
			return f.Type == schema.Bool
		}
	}
	return false
}

// saveFilePart saves one file part and sets body[key] to its storage key.
func saveFilePart(ctx context.Context, ch *CrudHandler, store upload.Storage, key string, fieldType schema.FieldType, fh *multipart.FileHeader, body map[string]any) error {
	f, err := fh.Open()
	if err != nil {
		return fmt.Errorf("open file part %q: %w", key, err)
	}
	defer f.Close()

	var opts []file.ProcessOption
	if ch.StripUploadMetadata {
		opts = append(opts, file.StripMetadata())
	}
	if fieldType == schema.Image {
		opts = append(opts, file.AllowTypes(file.ImageTypes...))
		if d := ch.deriverFor(key); d != nil {
			opts = append(opts, file.WithImageDeriver(d))
		}
	}

	ff, err := file.ProcessFileField(ctx, store, f, fh.Filename, ch.Entity.GetName(), key, opts...)
	if err != nil {
		return fmt.Errorf("upload %q: %w", key, err)
	}
	body[key] = ff.StorageRef
	if ff.Image != nil {
		applyDerivedColumns(ch.Entity, key, ff.Image, body)
	}
	return nil
}

// deriverFor is field's FieldImageDerivers entry, nil included, else
// ImageDeriver.
func (ch *CrudHandler) deriverFor(field string) file.ImageDeriver {
	if ch.FieldImageDerivers != nil {
		if d, ok := ch.FieldImageDerivers[field]; ok {
			return d
		}
	}
	return ch.ImageDeriver
}

// derivedColumnSuffixes names the sibling columns an Image field's derived
// values go to, when the entity declares them.
var derivedColumnSuffixes = struct {
	blurHash    string
	placeholder string
	variants    string
}{
	blurHash:    "_blurhash",
	placeholder: "_placeholder",
	variants:    "_variants",
}

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
		if raw, err := json.Marshal(d.Variants); err == nil {
			set(derivedColumnSuffixes.variants, string(raw))
		}
	}
}

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
		case schema.Decimal:
			return raw
		case schema.Float:
			if n, ok := floatText(raw); ok {
				return n
			}
		case schema.Bool:
			switch strings.ToLower(raw) {
			case "true", "1", "yes", "on":
				return true
			case "false", "0", "no", "off":
				return false
			}
		default:
		}
		return raw
	}
	return raw
}

func (ch *CrudHandler) validateMediaURLs(body map[string]any) error {
	for _, m := range ch.bodyMediaRefs(body) {
		if !isSafeMediaURL(m.ref) {
			return &ValidationError{fields: map[string][]schema.Violation{m.field: {{Reason: reasonUnsafeURL}}}}
		}
	}
	return nil
}

// isSafeMediaURL admits what urlsafe.Resource admits, minus any value
// containing "..".
func isSafeMediaURL(s string) bool {
	if strings.Contains(s, "..") {
		return false
	}
	return urlsafe.OK(s, urlsafe.Resource)
}

type savedKeyLedger struct {
	upload.Storage
	mu   sync.Mutex
	keys []string
}

type storageSaveError struct{ err error }

func (e *storageSaveError) Error() string { return "storage save: " + e.err.Error() }
func (e *storageSaveError) Unwrap() error { return e.err }

// writeBodyError answers a readRequestBody failure: 413, a fixed 500 for a
// storage failure, or 400.
func writeBodyError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errBodyTooLarge) {
		status, phrase := bodylimit.TooLargeRefusal()
		writeJSONError(w, r, status, phrase)
		return
	}
	if _, ok := errors.AsType[*storageSaveError](err); ok {
		logf("crud: upload save failed: %v", err)
		writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidBody)
}

func (l *savedKeyLedger) Save(ctx context.Context, key string, src io.Reader) error {
	if err := l.Storage.Save(ctx, key, src); err != nil {
		return &storageSaveError{err: err}
	}
	l.mu.Lock()
	l.keys = append(l.keys, key)
	l.mu.Unlock()
	return nil
}

func (l *savedKeyLedger) Delete(ctx context.Context, key string) error {
	err := l.Storage.Delete(ctx, key)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	l.mu.Lock()
	l.keys = deleteKey(l.keys, key)
	l.mu.Unlock()
	return err
}

func (l *savedKeyLedger) saved() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.keys))
	copy(out, l.keys)
	return out
}

func deleteKey(keys []string, key string) []string {
	for i, k := range keys {
		if k == key {
			return append(keys[:i], keys[i+1:]...)
		}
	}
	return keys
}

// deleteSavedUploads deletes the keys a failed write's parse saved.
func (ch *CrudHandler) deleteSavedUploads(ctx context.Context, keys []string) {
	if len(keys) == 0 || ch.Storage == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	for _, key := range keys {
		if err := ch.Storage.Delete(ctx, key); err != nil {
			logf("crud: compensating delete of orphaned upload %q failed: %v", key, err)
		}
	}
}
