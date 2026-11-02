// Package arms holds the 2026-11 embedskip arms, reduced from
// core/handler bind.go before 8708ddd5 and 4cd4d974 and
// internal/jsonexact before cdf2cb1b.
package arms

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
)

type taggedField struct {
	tag   string
	index []int
}

// The tag-keyed walker every binder below reaches (it descends, so the
// loop rule itself is quiet).
func taggedFields(t reflect.Type, key string) []taggedField {
	var out []taggedField
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous {
			continue
		}
		out = append(out, taggedField{tag: f.Tag.Get(key), index: f.Index})
	}
	return out
}

// ---- arm 1: the entry gate ----

// Pre-8708ddd5 Bind: one Elem below the destination, then the struct gate.
func Bind(dst any, q map[string]string) error {
	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("bind: destination must be a non-nil pointer")
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct { // want `struct kind gate on reflect.ValueOf\(dst\).Elem\(\) binds nothing for a pointer-typed input`
		return nil
	}
	return bindQuery(q, rv)
}

// Pre-8708ddd5 bindToolParams: the inline spelling, == form.
func bindToolParams(params map[string]string, dst any) error {
	rv := reflect.ValueOf(dst).Elem()
	if rv.Kind() == reflect.Struct { // want `struct kind gate on reflect.ValueOf\(dst\).Elem\(\)`
		return bindQuery(params, rv)
	}
	return nil
}

// The gate read through .Type(), the validateBodyKeys spelling.
func validateBodyKeys(dst any) []taggedField {
	t := reflect.ValueOf(dst).Elem().Type()
	if t.Kind() != reflect.Struct { // want `struct kind gate on reflect.ValueOf\(dst\).Elem\(\)`
		return nil
	}
	return taggedFields(t, "json")
}

func bindQuery(q map[string]string, rv reflect.Value) error {
	for _, tf := range taggedFields(rv.Type(), "query") {
		if s, ok := q[tf.tag]; ok {
			if err := setField(rv.FieldByIndex(tf.index), s); err != nil {
				return err
			}
		}
	}
	return nil
}

// The fix: follow the pointer levels first.
func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer && t.Elem() != t {
		t = t.Elem()
	}
	return t
}

func BindFixed(dst any, q map[string]string) error {
	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("bind: destination must be a non-nil pointer")
	}
	rv = rv.Elem()
	st := derefType(rv.Type())
	if st.Kind() != reflect.Struct {
		return nil
	}
	return bindQuery(q, rv)
}

// An inline deref loop after the Elem is the same fix.
func BindLoop(dst any, q map[string]string) error {
	rv := reflect.ValueOf(dst).Elem()
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	return bindQuery(q, rv)
}

// A pointer-kind branch after the Elem that handles the pointer input on
// its own path is the same fix in another spelling.
func BindBranch(dst any, q map[string]string) error {
	rv := reflect.ValueOf(dst).Elem()
	if rv.Kind() == reflect.Pointer {
		return BindFixed(rv.Interface(), q)
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	return bindQuery(q, rv)
}

// Near miss: the same gate in a function that reaches no tag walk is
// not a binder (isStructPointer's caller decides what to do).
func isStructPointer(dst any) bool {
	rv := reflect.ValueOf(dst)
	return rv.Kind() == reflect.Pointer && rv.Elem().Kind() == reflect.Struct
}

// Near miss: a gate on a typed parameter's Elem (a slice element type)
// is not the destination's pointee.
func elemStructs(t reflect.Type) []taggedField {
	if t.Elem().Kind() == reflect.Struct {
		return taggedFields(t.Elem(), "json")
	}
	return nil
}

// ---- arm 2: the kind setter with no TextUnmarshaler path ----

// Pre-4cd4d974 setField.
func setField(fv reflect.Value, s string) error {
	if fv.Kind() == reflect.Pointer {
		pt := reflect.New(fv.Type().Elem())
		if err := setField(pt.Elem(), s); err != nil {
			return err
		}
		fv.Set(pt)
		return nil
	}
	switch fv.Kind() { // want `tag binder sets fields by kind with no encoding.TextUnmarshaler check`
	case reflect.String:
		fv.SetString(s)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		fv.SetInt(n)
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

// ---- arm 3: pointer recursion with no visited set ----

// Pre-cdf2cb1b walk: the pointer case recurses on v.Elem() with nothing
// remembering where it has been.
func walk(v reflect.Value, leaf func(json.Number) (any, error)) error {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			return walk(v.Elem(), leaf) // want `reflect walk recurses through a pointer with no visited set`
		}
	case reflect.Interface:
		if !v.IsNil() {
			return walk(v.Elem(), leaf)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() || v.Type().Field(i).Anonymous {
				if err := walk(f, leaf); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// The if-spelling of the same recursion.
func scrub(v reflect.Value) {
	if v.Kind() == reflect.Pointer && !v.IsNil() {
		scrub(v.Elem()) // want `reflect walk recurses through a pointer with no visited set`
		return
	}
	if v.Kind() != reflect.Struct {
		return
	}
	for i := range v.NumField() {
		if v.Type().Field(i).Anonymous || v.Field(i).CanSet() {
			scrub(v.Field(i))
		}
	}
}

// The fix: a visited set keyed on the pointer.
func walkFixed(v reflect.Value, seen map[uintptr]bool) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() || seen[v.Pointer()] {
			return
		}
		seen[v.Pointer()] = true
		walkFixed(v.Elem(), seen)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).Anonymous || v.Field(i).CanSet() {
				walkFixed(v.Field(i), seen)
			}
		}
	}
}

// A depth bound the body compares ends the recursion too.
func walkDepth(v reflect.Value, depth int) {
	if depth > 32 {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		walkDepth(v.Elem(), depth+1)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).Anonymous || v.Field(i).CanSet() {
				walkDepth(v.Field(i), depth+1)
			}
		}
	}
}

// Near miss: a recursion through Elem in a function that walks no
// struct fields (a slice/map element descent) is outside the arm.
func flatten(v reflect.Value) int {
	if v.Kind() == reflect.Pointer {
		return flatten(v.Elem())
	}
	return v.Len()
}

// A gate that refuses a non-struct out loud is not the silent skip
// (core-ui/di Inject, core/config Load).
func Inject(target any) error {
	tv := reflect.ValueOf(target)
	if tv.Kind() != reflect.Pointer || tv.IsNil() {
		return errors.New("di: target must be a non-nil pointer to a struct")
	}
	ev := tv.Elem()
	if ev.Kind() != reflect.Struct {
		return errors.New("di: target must point to a struct")
	}
	return bindQuery(nil, ev)
}

// Near miss for the cycle arm: a walk that neither keys by tag nor
// descends on Anonymous (a deep-equal style diff over plain config) is
// outside embedskip's scope.
func diff(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Pointer:
		return diff(a.Elem(), b.Elem())
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if !diff(a.Field(i), b.Field(i)) {
				return false
			}
		}
	}
	return true
}
