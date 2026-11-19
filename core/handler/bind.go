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

// Bind fills dst, a non-nil pointer, from header, query and path tags,
// then the JSON body, each later present value replacing an earlier one.
// A tagged type with UnmarshalText binds through it; an empty value is
// absent unless the tag carries ",empty". A body that is not strict JSON
// is a 400, one over 1 MiB a 413.
func Bind(r *http.Request, dst any) error {
	if dst == nil {
		return errors.New("bind: nil destination")
	}

	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("bind: destination must be a non-nil pointer")
	}

	rv = rv.Elem()
	st := derefType(rv.Type())
	if st.Kind() != reflect.Struct {
		hasBody := r.Body != nil && r.ContentLength != 0
		if hasBody || r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			if IsJSONContentType(r.Header.Get("Content-Type")) {
				return bindBody(r, dst)
			}
		}
		return nil
	}

	if err := bindHeaders(r, rv, st); err != nil {
		return err
	}

	if err := bindQuery(r, rv, st); err != nil {
		return err
	}

	if err := bindPath(r, rv, st); err != nil {
		return err
	}

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

// IsJSONContentType reports whether ct parses as application/json or a
// +json structured-syntax type.
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
	return strings.HasSuffix(mt, "+json")
}

const maxBodyBytes = 1 << 20 // 1 MB

func bindBody(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()

	limited := http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
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
	if err := UnmarshalStrict(body, dst, UseNumber(), disallowUnknownFields()); err != nil {
		return err
	}
	if err := jsonexact.Settle(dst); err != nil {
		return Fail(400, ReasonInvalidJSON, map[string]any{"detail": JSONErrorDetail(err)})
	}
	return nil
}

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

		if !f.IsExported() {
			continue
		}

		if name == "" {
			name = f.Name
		}
		out[name] = struct{}{}
	}
}

func sanitizeQueryParam(s string) string {
	for i := range s {
		if c := s[i]; c < 0x20 || c == 0x7f {
			return s[:i]
		}
	}
	return s
}

type taggedField struct {
	name  string
	empty bool
	index []int
}

const tagOptionEmpty = "empty"

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

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer && t.Elem() != t {
		t = t.Elem()
	}
	return t
}

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

func (tf taggedField) absent(s string) bool {
	return s == "" && !tf.empty
}

func setTagged(fv reflect.Value, s string) error {
	if s == "" {
		return setEmpty(fv)
	}
	return setField(fv, s)
}

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

func isTextUnmarshaler(fv reflect.Value) bool {
	return fv.CanAddr() && fv.Addr().Type().Implements(textUnmarshalerType)
}

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

func setField(fv reflect.Value, s string) error {
	if fv.Kind() == reflect.Pointer {
		pt := reflect.New(fv.Type().Elem())
		if err := setField(pt.Elem(), s); err != nil {
			return err
		}
		fv.Set(pt)
		return nil
	}

	if isTextUnmarshaler(fv) {
		return fv.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(s))
	}

	switch fv.Kind() {
	case reflect.String:
		fv.SetString(s)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(s, fv.Type().Bits())
		if err != nil {
			return err
		}
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
