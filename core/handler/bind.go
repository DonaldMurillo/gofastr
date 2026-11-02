package handler

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// Bind populates dst by merging values from all sources:
// 1. Header values (via `header:"X-Request-ID"` tag)
// 2. Query parameters (via `query:"name"` tag)
// 3. Path parameters (via `path:"id"` tag)
// 4. JSON body (decodes entire struct, highest priority)
//
// Sources are applied in the order above; later present values overwrite
// earlier values. An absent JSON property preserves the lower-priority value.
//
// A tagged field whose type implements encoding.TextUnmarshaler
// (time.Time, a UUID, a validated enum), directly or through a pointer,
// binds through UnmarshalText; its error is a 400 for that parameter.
// An empty query or header value (`?page=`) is the same as an absent
// one: the field keeps its zero value and a pointer stays nil. The tag
// option `,empty` (`query:"q,empty"`) binds an explicit empty instead:
// "" for a string, the zero value for any other kind, and a non-nil
// pointer to it for a pointer field. Any other tag option, or ,empty
// on a path tag, is an error on every bind.
//
// If the body is present but not valid JSON, a 400 error is returned.
// If dst is not a pointer, Bind panics.
func Bind(r *http.Request, dst any) error {
	if dst == nil {
		return errors.New("bind: nil destination")
	}

	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("bind: destination must be a non-nil pointer")
	}

	// A pointer-typed input (Handler[*T, O] binds into a **T) gets the
	// struct rules too: the tags are read off the struct the pointer
	// levels end at, and settableField allocates the levels only when a
	// tagged value arrives, as encoding/json allocates them for a body.
	rv = rv.Elem()
	st := derefType(rv.Type())
	if st.Kind() != reflect.Struct {
		// Non-struct pointer (e.g. *string); only the JSON body applies,
		// under the same Content-Type gate as the struct branch. text/plain
		// is a CORS-simple content type, so a cross-site form can post it
		// with no preflight — the gate must hold for every destination shape.
		hasBody := r.Body != nil && r.ContentLength != 0
		if hasBody || r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			if IsJSONContentType(r.Header.Get("Content-Type")) {
				return bindBody(r, dst)
			}
		}
		return nil
	}

	// 1. Bind header fields first (lowest priority)
	if err := bindHeaders(r, rv, st); err != nil {
		return err
	}

	// 2. Bind query fields
	if err := bindQuery(r, rv, st); err != nil {
		return err
	}

	// 3. Bind path fields
	if err := bindPath(r, rv, st); err != nil {
		return err
	}

	// 4. Bind JSON body (highest priority, overwrites)
	hasBody := r.Body != nil && r.ContentLength != 0
	if hasBody || r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		if IsJSONContentType(r.Header.Get("Content-Type")) {
			if err := bindBody(r, dst); err != nil {
				return err
			}
		}
	}

	return nil
}

// IsJSONContentType reports whether ct names the JSON media type. It
// parses the value through mime.ParseMediaType so "application/json"
// and "application/json; charset=utf-8" both match, while a literal
// prefix check would also accept "application/jsonp" or
// "application/json-evil", a known Content-Type smuggling trick.
//
// Exported for battery/auth's JSON gate (json_limit.go), which
// previously carried its own hand-rolled copy: the shared version
// accepts any "+json" structured-syntax suffix (not only
// application/*+json) and rejects values whose parameters fail to
// parse, where the old copy ignored parameter syntax entirely.
func IsJSONContentType(ct string) bool {
	if ct == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	if mt == "application/json" {
		return true
	}
	// RFC 6839 structured suffix, e.g. application/vnd.api+json
	return strings.HasSuffix(mt, "+json")
}

// bindBody decodes JSON body into dst.
// Limits the request body to 1MB to prevent DoS via oversized payloads.
const maxBodyBytes = 1 << 20 // 1 MB

func bindBody(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()

	// Cap the body reader to prevent memory exhaustion attacks.
	limited := http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		// A body past the cap is a 413, not malformed JSON. Either way the
		// read error stays reachable through errors.As, as DecodeStrict
		// keeps it.
		if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
			return WrapError(http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge), err)
		}
		e := Fail(400, ReasonInvalidJSON, map[string]any{"detail": BodyReadDetail(err)})
		e.Err = err
		return e
	}
	if len(body) == 0 {
		return nil
	}

	// Pre-validate the top-level object: reject duplicate keys,
	// case-folded matches against struct json tags, and unknown
	// fields. Stdlib json.Decoder accepts all three silently, which
	// turns "validated by tag" handlers into mass-assignment and
	// last-key-wins smuggling vectors.
	if err := validateBodyKeys(body, dst); err != nil {
		return err
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	// UseNumber, then Settle: a number under an interface (a
	// map[string]any field) keeps its digits past 2^53.
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		return Fail(400, ReasonInvalidJSON, map[string]any{"detail": JSONErrorDetail(err)})
	}
	// Exactly one value: a second Decode must find nothing. '{"a":1}{"b":2}'
	// decodes its first value and hides the second, leaving two parsers (or
	// a re-reading consumer) disagreeing about what the body said — the
	// same two-ways-to-read ambiguity the strict key rules refuse.
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Fail(400, ReasonJSONMultipleValues)
	}
	if err := jsonexact.Settle(dst); err != nil {
		return Fail(400, ReasonInvalidJSON, map[string]any{"detail": JSONErrorDetail(err)})
	}
	return nil
}

// validateBodyKeys enforces strict top-level key handling for the JSON
// body decoded into dst. It is a no-op when dst is not a pointer to a
// struct (through any number of pointer levels: **T for a Handler[*T, O]
// input) or when the body isn't a top-level object. Both shapes flow through
// the decoder unchanged. For struct destinations it requires every
// top-level key to exact-match a json tag on the struct (rejecting
// case-folded variants) and rejects duplicate keys.
func validateBodyKeys(body []byte, dst any) error {
	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return nil
	}
	st := derefType(rv.Type().Elem())
	if st.Kind() != reflect.Struct {
		return nil
	}
	allowed := jsonTagSet(st)

	dec := json.NewDecoder(bytes.NewReader(body))
	first, err := dec.Token()
	if err != nil {
		return nil
	}
	delim, ok := first.(json.Delim)
	if !ok || delim != '{' {
		return nil
	}

	seen := make(map[string]struct{}, len(allowed))
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil
		}
		key, ok := tok.(string)
		if !ok {
			return nil
		}
		if _, dup := seen[key]; dup {
			return Fail(400, ReasonJSONDuplicateKey, map[string]any{"key": strconv.Quote(key)})
		}
		seen[key] = struct{}{}
		if _, allowedKey := allowed[key]; !allowedKey {
			return Fail(400, ReasonJSONUnknownField, map[string]any{"key": strconv.Quote(key)})
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil
		}
	}
	return nil
}

// jsonTagSet returns the set of exact JSON field names declared on t.
// Fields without a json tag fall back to their Go name (mirroring
// encoding/json's default), and json:"-" is skipped. Anonymous embedded
// struct fields without an explicit json name have their keys promoted
// (matching encoding/json's promotion rules), so the strict-key
// allow-list doesn't false-reject a valid promoted key.
func jsonTagSet(t reflect.Type) map[string]struct{} {
	out := make(map[string]struct{}, t.NumField())
	collectJSONTags(t, out)
	return out
}

func collectJSONTags(t reflect.Type, out map[string]struct{}) {
	collectJSONTagsSeen(t, out, make(map[reflect.Type]bool))
}
func collectJSONTagsSeen(t reflect.Type, out map[string]struct{}, active map[reflect.Type]bool) {
	if active[t] {
		return
	}
	active[t] = true
	defer delete(active, t)
	for f := range t.Fields() {
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")

		// Anonymous embedded struct with no explicit json name:
		// encoding/json promotes its EXPORTED fields into the parent
		// object, even when the embedded type itself is unexported,
		// so merge the embedded type's key set rather than naming the
		// field. The promoted fields' own exportedness is enforced in
		// the recursive call.
		if f.Anonymous && name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectJSONTagsSeen(ft, out, active)
				continue
			}
		}

		// Non-embedded unexported fields are never (de)serialized by
		// encoding/json, so they don't belong on the allow-list.
		if !f.IsExported() {
			continue
		}

		if name == "" {
			name = f.Name
		}
		out[name] = struct{}{}
	}
}

// sanitizeQueryParam truncates s at the first control byte a query
// parameter must never carry into handler input: the same
// truncate-at-first-forbidden-byte walk router.SanitizePathParam
// applies to path segments, over the full C0/DEL forge set. URL.Query
// percent-decodes %0d%0a and friends before Bind runs, so the query is
// a second arrival surface for the bytes the path twin already strips —
// from handler input they flow on into logs, response headers, SSE
// frames, and file or database lookups exactly as raw path bytes did.
// Slashes and dot segments are NOT cut here: unlike a path component, a
// query value legitimately carries both.
func sanitizeQueryParam(s string) string {
	for i := range s {
		if c := s[i]; c < 0x20 || c == 0x7f {
			return s[:i]
		}
	}
	return s
}

// taggedField is a struct field carrying a binding tag, reached from the
// top-level struct through index (an embedded struct adds a step). name
// is the tag's part before the first comma; empty records the `,empty`
// option, which lets an explicit empty value through.
type taggedField struct {
	name  string
	empty bool
	index []int
}

// tagOptionEmpty is the one binding-tag option: `query:"q,empty"` or
// `header:"X-Foo,empty"` binds an explicit empty value (a string gets
// "", a pointer a non-nil pointer to the zero value, any other kind its
// zero value) where the default treats an empty value as absent.
const tagOptionEmpty = "empty"

// taggedFields lists t's fields tagged with key, descending into an
// untagged embedded struct (value or pointer) the way encoding/json
// promotes its fields. A struct already on the path is skipped. A tag
// option the source does not know is a programming error, returned as a
// plain error (a 500, like a nil destination) on every bind, so the
// typo shows up on the first request rather than binding nothing.
func taggedFields(t reflect.Type, key string) ([]taggedField, error) {
	var out []taggedField
	if err := collectTagged(t, key, nil, map[reflect.Type]bool{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func collectTagged(t reflect.Type, key string, prefix []int, active map[reflect.Type]bool, out *[]taggedField) error {
	if active[t] {
		return nil
	}
	active[t] = true
	defer delete(active, t)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get(key)
		index := append(slices.Clone(prefix), i)
		if tag == "" && f.Anonymous {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if err := collectTagged(ft, key, index, active, out); err != nil {
					return err
				}
			}
			continue
		}
		if tag == "" || tag == "-" {
			continue
		}
		name, opts, hasOpts := strings.Cut(tag, ",")
		if name == "" {
			return fmt.Errorf("bind: field %s: tag %s:%q has no name", f.Name, key, tag)
		}
		tf := taggedField{name: name, index: index}
		if hasOpts {
			for opt := range strings.SplitSeq(opts, ",") {
				// Path binding takes no option: a matched route's {name}
				// is never empty, so ",empty" there can only be a mistake.
				if opt == tagOptionEmpty && key != "path" {
					tf.empty = true
					continue
				}
				return fmt.Errorf("bind: field %s: tag %s:%q has unknown option %q", f.Name, key, tag, opt)
			}
		}
		*out = append(*out, tf)
	}
	return nil
}

// derefType follows t's pointer levels to the type they end at, so *T
// and **T name T. A pointer type that names itself (type P *P) ends at
// the pointer.
func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer && t.Elem() != t {
		t = t.Elem()
	}
	return t
}

// settableField returns the field at index under rv, allocating a nil
// pointer level (the input's own, or an embedded one) on the way, or an
// invalid Value when the field (or a nil unexported embedded pointer
// before it) cannot be set.
func settableField(rv reflect.Value, index []int) reflect.Value {
	v := rv
	for _, i := range index {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				if !v.CanSet() {
					return reflect.Value{}
				}
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	if !v.CanSet() {
		return reflect.Value{}
	}
	return v
}

// bindQuery binds query parameters to struct fields tagged with `query:"name"`.
// An empty value (`?page=`, `?q=`) binds as if the parameter were absent,
// whatever the field's kind, unless the tag carries `,empty`.
func bindQuery(r *http.Request, rv reflect.Value, st reflect.Type) error {
	q := r.URL.Query()

	fields, err := taggedFields(st, "query")
	if err != nil {
		return err
	}
	for _, tf := range fields {
		values, ok := q[tf.name]
		if !ok || len(values) == 0 {
			continue
		}
		value := sanitizeQueryParam(values[0])
		if tf.absent(value) {
			continue
		}

		fv := settableField(rv, tf.index)
		if !fv.IsValid() {
			continue
		}

		if err := setTagged(fv, value); err != nil {
			return Fail(400, ReasonInvalidParameter, map[string]any{"source": "query", "name": strconv.Quote(tf.name), "detail": err.Error()})
		}
	}

	return nil
}

// bindPath binds path parameters to struct fields tagged with `path:"name"`.
// Path parameters are read from r.PathValue (Go 1.22+) and sanitized the
// same way core/router's Param sanitizes them. Bind is a second arrival
// surface for the identical property, and it was the unsanitized one: a
// percent-encoded newline or NUL, a decoded "/", or a ".." segment reached
// handler input verbatim, from where it goes on into log lines, response
// headers, SSE frames, and file or database lookups.
//
// A field whose tag the matched route does not declare stays zero: one
// input struct often serves both POST /users and PUT /users/{id}. So a
// matched request reads through router.Params, which holds only the
// declared names, rather than router.Param, which panics on the others.
func bindPath(r *http.Request, rv reflect.Value, st reflect.Type) error {
	var declared map[string]string
	if r.Pattern != "" {
		declared = router.Params(r)
	}

	fields, err := taggedFields(st, "path")
	if err != nil {
		return err
	}
	for _, tf := range fields {
		var value string
		if declared != nil {
			value = declared[tf.name]
		} else {
			value = router.Param(r, tf.name)
		}
		if value == "" {
			continue
		}

		fv := settableField(rv, tf.index)
		if !fv.IsValid() {
			continue
		}

		if err := setField(fv, value); err != nil {
			return Fail(400, ReasonInvalidParameter, map[string]any{"source": "path", "name": strconv.Quote(tf.name), "detail": err.Error()})
		}
	}

	return nil
}

// bindHeaders binds header values to struct fields tagged with `header:"X-Request-ID"`.
// An empty header value binds as if the header were absent, the same rule
// as a query value, unless the tag carries `,empty`.
func bindHeaders(r *http.Request, rv reflect.Value, st reflect.Type) error {
	fields, err := taggedFields(st, "header")
	if err != nil {
		return err
	}
	for _, tf := range fields {
		values := r.Header.Values(tf.name)
		if len(values) == 0 || tf.absent(values[0]) {
			continue
		}

		fv := settableField(rv, tf.index)
		if !fv.IsValid() {
			continue
		}

		if err := setTagged(fv, values[0]); err != nil {
			return Fail(400, ReasonInvalidHeader, map[string]any{"name": strconv.Quote(tf.name), "detail": err.Error()})
		}
	}

	return nil
}

// absent reports whether a value the request carried for tf binds as if
// it were missing: an empty value does, unless the tag carries `,empty`.
// Callers check it before settableField, so an empty value allocates no
// pointer level (a Handler[*T, O] input stays nil on `?q=`).
func (tf taggedField) absent(s string) bool {
	return s == "" && !tf.empty
}

// setTagged fills a tagged field from a value absent let through. An
// empty one (only under `,empty`) gets the field's explicit zero value,
// and a pointer field a non-nil pointer to it, so a handler can tell
// `?q=` from no q at all.
func setTagged(fv reflect.Value, s string) error {
	if s == "" {
		return setEmpty(fv)
	}
	return setField(fv, s)
}

// setEmpty stores the explicit zero value setTagged binds for an empty
// value under `,empty`, refusing a kind setField could not bind either.
func setEmpty(fv reflect.Value) error {
	if fv.Kind() == reflect.Pointer {
		pt := reflect.New(fv.Type().Elem())
		if err := setEmpty(pt.Elem()); err != nil {
			return err
		}
		fv.Set(pt)
		return nil
	}
	if !isTextUnmarshaler(fv) && !scalarKind(fv.Kind()) {
		return errors.New("unsupported type " + fv.Kind().String())
	}
	fv.SetZero()
	return nil
}

// isTextUnmarshaler reports whether fv's address implements
// encoding.TextUnmarshaler (time.Time, a UUID, a validated enum).
func isTextUnmarshaler(fv reflect.Value) bool {
	return fv.CanAddr() && fv.Addr().Type().Implements(textUnmarshalerType)
}

// scalarKind reports whether setField's kind switch binds k.
func scalarKind(k reflect.Kind) bool {
	switch k {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// setField sets a reflect.Value from a string, converting to the appropriate type.
func setField(fv reflect.Value, s string) error {
	// Handle pointer types
	if fv.Kind() == reflect.Pointer {
		pt := reflect.New(fv.Type().Elem())
		if err := setField(pt.Elem(), s); err != nil {
			return err
		}
		fv.Set(pt)
		return nil
	}

	// A type that parses its own text (time.Time, a UUID, an enum whose
	// UnmarshalText allows only its members) binds through that parser,
	// ahead of the kind switch: `type Status string` with an UnmarshalText
	// that refuses "bogus" was refused in a JSON body while ?status=bogus
	// bound it raw through the string case.
	if isTextUnmarshaler(fv) {
		return fv.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(s))
	}

	switch fv.Kind() {
	case reflect.String:
		fv.SetString(s)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Parse against the field's actual bit width so a value that
		// overflows the destination type is rejected instead of silently
		// truncated. Previously every int was parsed as int64 and then
		// SetInt truncated: ?small=300 into an int8 bound 44 with nil
		// error. ParseInt with bitSize=N enforces the intN range.
		n, err := strconv.ParseInt(s, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		// Same range check as the int case. ParseUint bitSize=N enforces
		// the uintN range (and already rejects negative input).
		n, err := strconv.ParseUint(s, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		// ParseFloat with the field's bit width so a float32 field
		// rejects values outside its range (returns a range error
		// instead of silently rounding to ±Inf via SetFloat).
		n, err := strconv.ParseFloat(s, fv.Type().Bits())
		if err != nil {
			return err
		}
		// Reject NaN/±Inf: "NaN" and "Inf" parse cleanly, and every
		// `if v < min || v > max` guard downstream is false for NaN, so a
		// bound check the handler wrote would silently pass. A finite
		// float is the only value a bound can reason about.
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return fmt.Errorf("%q is not a finite number", s)
		}
		fv.SetFloat(n)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	default:
		return errors.New("unsupported type " + fv.Kind().String())
	}
	return nil
}
