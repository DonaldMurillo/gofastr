package handler

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

// Bind populates dst by merging values from all sources:
// 1. Header values (via `header:"X-Request-ID"` tag)
// 2. Query parameters (via `query:"name"` tag)
// 3. Path parameters (via `path:"id"` tag)
// 4. JSON body (decodes entire struct — highest priority)
//
// If the request has a JSON body and Content-Type is application/json,
// the body is decoded first. Then query/path/header values fill in
// any zero-valued fields.
//
// If the body is present but not valid JSON, a 400 error is returned.
// If dst is not a pointer, Bind panics.
func Bind(r *http.Request, dst any) error {
	if dst == nil {
		return errors.New("bind: nil destination")
	}

	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return errors.New("bind: destination must be a non-nil pointer")
	}

	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		// Non-struct pointer (e.g. *string) — only JSON body applies.
		return bindBody(r, dst)
	}

	// 1. Bind header fields first (lowest priority)
	if err := bindHeaders(r, rv); err != nil {
		return err
	}

	// 2. Bind query fields
	if err := bindQuery(r, rv); err != nil {
		return err
	}

	// 3. Bind path fields
	if err := bindPath(r, rv); err != nil {
		return err
	}

	// 4. Bind JSON body (highest priority — overwrites)
	hasBody := r.Body != nil && r.ContentLength != 0
	if hasBody || r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		if isJSONContentType(r.Header.Get("Content-Type")) {
			if err := bindBody(r, dst); err != nil {
				return err
			}
		}
	}

	return nil
}

// isJSONContentType reports whether ct names the JSON media type. It
// parses the value through mime.ParseMediaType so "application/json"
// and "application/json; charset=utf-8" both match, while a literal
// prefix check would also accept "application/jsonp" or
// "application/json-evil" — a known Content-Type smuggling trick.
func isJSONContentType(ct string) bool {
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

	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(dst); err != nil {
		return Errorf(400, "invalid JSON: %s", err.Error())
	}
	return nil
}

// bindQuery binds query parameters to struct fields tagged with `query:"name"`.
func bindQuery(r *http.Request, rv reflect.Value) error {
	rt := rv.Type()
	q := r.URL.Query()

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("query")
		if tag == "" {
			continue
		}
		if tag == "-" {
			continue
		}

		values, ok := q[tag]
		if !ok || len(values) == 0 {
			continue
		}

		fv := rv.Field(i)
		if !fv.CanSet() {
			continue
		}
		if !fv.IsZero() {
			continue // don't overwrite existing values
		}

		if err := setField(fv, values[0]); err != nil {
			return Errorf(400, "invalid query parameter %q: %s", tag, err.Error())
		}
	}

	return nil
}

// bindPath binds path parameters to struct fields tagged with `path:"name"`.
// Path parameters are read from r.PathValue (Go 1.22+).
func bindPath(r *http.Request, rv reflect.Value) error {
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("path")
		if tag == "" || tag == "-" {
			continue
		}

		value := r.PathValue(tag)
		if value == "" {
			continue
		}

		fv := rv.Field(i)
		if !fv.CanSet() {
			continue
		}
		if !fv.IsZero() {
			continue
		}

		if err := setField(fv, value); err != nil {
			return Errorf(400, "invalid path parameter %q: %s", tag, err.Error())
		}
	}

	return nil
}

// bindHeaders binds header values to struct fields tagged with `header:"X-Request-ID"`.
func bindHeaders(r *http.Request, rv reflect.Value) error {
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("header")
		if tag == "" || tag == "-" {
			continue
		}

		value := r.Header.Get(tag)
		if value == "" {
			continue
		}

		fv := rv.Field(i)
		if !fv.CanSet() {
			continue
		}
		if !fv.IsZero() {
			continue
		}

		if err := setField(fv, value); err != nil {
			return Errorf(400, "invalid header %q: %s", tag, err.Error())
		}
	}

	return nil
}

// setField sets a reflect.Value from a string, converting to the appropriate type.
func setField(fv reflect.Value, s string) error {
	// Handle pointer types
	if fv.Kind() == reflect.Ptr {
		pt := reflect.New(fv.Type().Elem())
		if err := setField(pt.Elem(), s); err != nil {
			return err
		}
		fv.Set(pt)
		return nil
	}

	switch fv.Kind() {
	case reflect.String:
		fv.SetString(s)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
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
