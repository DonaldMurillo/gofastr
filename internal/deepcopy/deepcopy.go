// Package deepcopy rebuilds every map and slice reachable from a value,
// so a holder of the copy cannot write into the original's containers.
package deepcopy

import (
	"reflect"
	"time"
)

// Map copies row far enough that writing into the copy, at any depth,
// cannot reach row: every map and slice is rebuilt (see [Value]). A nil
// row copies to an empty map. crud copies a scanned row with it so a
// redaction hook cannot reach another holder of the same event.
func Map(row map[string]any) map[string]any {
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[k] = Value(v)
	}
	return out
}

// Value rebuilds every map and slice reachable from v.
//
// The fast paths below are the shapes the framework itself produces. They are
// not sufficient on their own: the containers that matter here are the ones an
// application's write hook INJECTS, and a hand-written type switch only ever
// covers the shapes whoever wrote it thought of. A first version listed three,
// which left []byte, []string, map[string]string, [][]map[string]any and
// anything else sharing storage with the record already handed to the event
// goroutine, so a hook masking inside one wrote through into the bus, and two
// SSE subscribers masking concurrently raced on it. That race is "concurrent
// map writes", a runtime throw no recover() catches. Hence the reflective
// fallback: unknown container, still copied.
//
// Structs are deliberately NOT traversed, and neither is a pointer TO one.
// Copying an arbitrary struct means copying whatever it embeds, a mutex, a
// file handle, a driver connection, and this package cannot know which of
// those a host's hook put in the record. Scalars need no copy.
//
// That rationale covers opaque shapes; it does not cover a pointer to a plain
// container. *map[string]any, *[]any and *[]map[string]any are still the
// record's own data, just one indirection out, and leaving them aliased
// reproduced the exact bug the reflective fallback was added for: a redaction
// hook mutating the response copy wrote through into the record already handed
// to the event goroutine, so two subscribers masking concurrently raced on one
// map, "concurrent map writes", a runtime throw no recover() catches. So
// reflectCopy DOES traverse a pointer whose element is a map, slice or
// array: it copies the pointee and allocates a fresh pointer to it. Every
// other pointer element kind (struct above all, but also chan/func/unsafe)
// is returned as-is.
func Value(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string, bool, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, float32, float64, time.Time:
		return v
	case map[string]any:
		return Map(t)
	case []map[string]any:
		cp := make([]map[string]any, len(t))
		for i, m := range t {
			cp[i] = Map(m)
		}
		return cp
	case []any:
		cp := make([]any, len(t))
		for i, e := range t {
			cp[i] = Value(e)
		}
		return cp
	case []byte:
		return append([]byte(nil), t...)
	}
	return reflectCopy(v)
}

// reflectCopy handles the container shapes the type switch above does not
// name. It copies maps, slices and arrays element-wise, plus pointers TO
// those three, which alias the record just as directly, and returns anything
// else unchanged.
func reflectCopy(v any) any {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer:
		// A pointer to a container is still the record's data. A pointer to a
		// struct (or anything else) is not something this package can clone,
		// see Value's comment.
		if rv.IsNil() {
			return v
		}
		switch rv.Type().Elem().Kind() {
		case reflect.Map, reflect.Slice, reflect.Array:
		default:
			return v
		}
		cp := reflect.ValueOf(Value(rv.Elem().Interface()))
		if !cp.IsValid() || !cp.Type().AssignableTo(rv.Type().Elem()) {
			return v
		}
		out := reflect.New(rv.Type().Elem())
		out.Elem().Set(cp)
		return out.Interface()
	case reflect.Map:
		if rv.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), reflectValue(iter.Value()))
		}
		return out.Interface()
	case reflect.Slice:
		if rv.IsNil() {
			return v
		}
		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out.Index(i).Set(reflectValue(rv.Index(i)))
		}
		return out.Interface()
	case reflect.Array:
		out := reflect.New(rv.Type()).Elem()
		for i := 0; i < rv.Len(); i++ {
			out.Index(i).Set(reflectValue(rv.Index(i)))
		}
		return out.Interface()
	default:
		return v
	}
}

// reflectValue recurses into an element, going back through the typed
// fast paths when the element is an interface holding one of them.
func reflectValue(elem reflect.Value) reflect.Value {
	switch elem.Kind() {
	case reflect.Interface:
		if elem.IsNil() {
			return elem
		}
		return reflect.ValueOf(Value(elem.Interface())).Convert(elem.Type())
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Pointer:
		// Pointer is here for the same reason it is in reflectCopy: a
		// []*map[string]any element aliases the record too. reflectCopy
		// returns non-container pointers unchanged, so structs stay opaque.
		cp := reflectCopy(elem.Interface())
		return reflect.ValueOf(cp)
	default:
		return elem
	}
}
