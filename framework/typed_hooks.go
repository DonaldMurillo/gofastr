package framework

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// OnBeforeCreate registers a typed BeforeCreate hook on the entity named
// name. The fields fn changes on *T are written back into the body to insert.
func OnBeforeCreate[T any](app *App, name string, fn func(ctx context.Context, value *T) error) {
	app.HookRegistry(name).RegisterHook(hook.BeforeCreate, func(ctx context.Context, data any) error {
		var v T
		if err := unmarshalHookPayload(data, &v); err != nil {
			return &hookBodyError{err: err}
		}
		before := snapshotDeepCopy(v)
		if err := fn(ctx, &v); err != nil {
			return err
		}
		if m, ok := data.(map[string]any); ok {
			return mergeStructIntoMap(&before, &v, m)
		}
		return nil
	})
}

// OnAfterCreate registers a typed AfterCreate hook on the inserted row. The
// fields fn changes reach the response, not the stored row. It runs
// before commit; an error rolls back the write.
func OnAfterCreate[T any](app *App, name string, fn func(ctx context.Context, value *T) error) {
	app.HookRegistry(name).RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
		var v T
		if err := unmarshalHookPayload(data, &v); err != nil {
			return err
		}
		before := snapshotDeepCopy(v)
		if err := fn(ctx, &v); err != nil {
			return err
		}
		if m, ok := data.(map[string]any); ok {
			return mergeStructIntoMap(&before, &v, m)
		}
		return nil
	})
}

// OnBeforeUpdate registers a typed BeforeUpdate hook. *T holds only what
// the caller sent; the fields fn changes are written back into the update.
func OnBeforeUpdate[T any](app *App, name string, fn func(ctx context.Context, value *T) error) {
	app.HookRegistry(name).RegisterHook(hook.BeforeUpdate, func(ctx context.Context, data any) error {
		var v T
		if err := unmarshalHookPayload(data, &v); err != nil {
			return &hookBodyError{err: err}
		}
		before := snapshotDeepCopy(v)
		if err := fn(ctx, &v); err != nil {
			return err
		}
		if m, ok := data.(map[string]any); ok {
			return mergeStructIntoMap(&before, &v, m)
		}
		return nil
	})
}

// OnAfterUpdate registers a typed AfterUpdate hook on the updated row. The
// fields fn changes reach the response, not the stored row.
func OnAfterUpdate[T any](app *App, name string, fn func(ctx context.Context, value *T) error) {
	app.HookRegistry(name).RegisterHook(hook.AfterUpdate, func(ctx context.Context, data any) error {
		var v T
		if err := unmarshalHookPayload(data, &v); err != nil {
			return err
		}
		before := snapshotDeepCopy(v)
		if err := fn(ctx, &v); err != nil {
			return err
		}
		if m, ok := data.(map[string]any); ok {
			return mergeStructIntoMap(&before, &v, m)
		}
		return nil
	})
}

// OnBeforeDelete registers a typed BeforeDelete hook on the record id.
func OnBeforeDelete(app *App, name string, fn func(ctx context.Context, id string) error) {
	registerIDHook(app, name, hook.BeforeDelete, "BeforeDelete", fn)
}

// OnAfterDelete registers a typed AfterDelete hook on the record id.
func OnAfterDelete(app *App, name string, fn func(ctx context.Context, id string) error) {
	registerIDHook(app, name, hook.AfterDelete, "AfterDelete", fn)
}

// OnBeforeList registers a typed BeforeList hook on the *hook.ListPayload.
func OnBeforeList(app *App, name string, fn func(ctx context.Context, p *hook.ListPayload) error) {
	registerPayloadHook(app, name, hook.BeforeList, "BeforeList", fn)
}

// OnAfterList registers a typed AfterList hook on the *hook.ListPayload,
// Results filled in; changing them changes the response.
func OnAfterList(app *App, name string, fn func(ctx context.Context, p *hook.ListPayload) error) {
	registerPayloadHook(app, name, hook.AfterList, "AfterList", fn)
}

// OnBeforeGet registers a typed BeforeGet hook on the *hook.GetPayload; a
// row its AddWhere excludes answers 404.
func OnBeforeGet(app *App, name string, fn func(ctx context.Context, p *hook.GetPayload) error) {
	registerPayloadHook(app, name, hook.BeforeGet, "BeforeGet", fn)
}

// OnAfterGet registers a typed AfterGet hook on the *hook.GetPayload,
// Result filled in; changing it changes the response.
func OnAfterGet(app *App, name string, fn func(ctx context.Context, p *hook.GetPayload) error) {
	registerPayloadHook(app, name, hook.AfterGet, "AfterGet", fn)
}

func registerPayloadHook[P any](app *App, name string, ht hook.HookType, label string, fn func(ctx context.Context, p *P) error) {
	app.HookRegistry(name).RegisterHook(ht, func(ctx context.Context, data any) error {
		p, ok := data.(*P)
		if !ok || p == nil {
			return fmt.Errorf("typed %s hook: payload type = %T, want %s (framework contract drift?)", label, data, reflect.TypeOf((*P)(nil)))
		}
		return fn(ctx, p)
	})
}

func registerIDHook(app *App, name string, ht hook.HookType, label string, fn func(ctx context.Context, id string) error) {
	app.HookRegistry(name).RegisterHook(ht, func(ctx context.Context, data any) error {
		id, ok := data.(string)
		if !ok {
			return fmt.Errorf("typed %s hook: payload type = %T, want string (framework contract drift?)", label, data)
		}
		return fn(ctx, id)
	})
}

var hookBodyPhrase = i18n.Define("framework.hook_body_type", "a value in the request body does not match its field's type")

type hookBodyError struct{ err error }

func (e *hookBodyError) Error() string { return e.err.Error() }
func (e *hookBodyError) Unwrap() error { return e.err }

// Phrased implements i18n.Phrased.
func (e *hookBodyError) Phrased() (i18n.Phrase, map[string]any) { return hookBodyPhrase, nil }

func unmarshalHookPayload(data any, dest any) error {
	if m, ok := data.(map[string]any); ok {
		b, err := json.Marshal(hookDecodeKeys(m))
		if err != nil {
			return err
		}
		return decodeHookJSON(b, dest)
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return jsonexact.Unmarshal(b, dest)
}

func hookDecodeKeys(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)*2)
	maps.Copy(out, m)
	for _, k := range slices.Sorted(maps.Keys(m)) {
		for _, alias := range []string{casing.ToCamel(k), casing.ToSnake(k)} {
			if _, taken := out[alias]; !taken {
				out[alias] = m[k]
			}
		}
	}
	return out
}

func snapshotDeepCopy[T any](v T) T {
	if b, err := json.Marshal(v); err == nil {
		var out T
		if err := decodeHookJSON(b, &out); err == nil {
			return out
		}
	}
	src := reflect.ValueOf(&v).Elem()
	out := reflect.New(src.Type()).Elem()
	out.Set(deepCopyValue(src, map[uintptr]reflect.Value{}))
	return out.Interface().(T)
}

func deepCopyValue(v reflect.Value, seen map[uintptr]reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		if cp, ok := seen[v.Pointer()]; ok {
			return cp
		}
		cp := reflect.New(v.Type().Elem())
		seen[v.Pointer()] = cp
		cp.Elem().Set(deepCopyValue(v.Elem(), seen))
		return cp
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		cp := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			cp.SetMapIndex(iter.Key(), deepCopyValue(iter.Value(), seen))
		}
		return cp
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		cp := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			cp.Index(i).Set(deepCopyValue(v.Index(i), seen))
		}
		return cp
	case reflect.Array:
		cp := reflect.New(v.Type()).Elem()
		for i := range v.Len() {
			cp.Index(i).Set(deepCopyValue(v.Index(i), seen))
		}
		return cp
	case reflect.Struct:
		cp := reflect.New(v.Type()).Elem()
		cp.Set(v)
		for i := range v.NumField() {
			if cp.Field(i).CanSet() {
				cp.Field(i).Set(deepCopyValue(v.Field(i), seen))
			}
		}
		return cp
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		cp := reflect.New(v.Type()).Elem()
		cp.Set(deepCopyValue(v.Elem(), seen))
		return cp
	}
	return v
}

func decodeHookJSON(b []byte, dest any) error {
	return jsonexact.Decode(b, dest, exactHookNumber)
}

func exactHookNumber(n json.Number) (any, error) {
	if i, err := n.Int64(); err == nil {
		return i, nil
	}
	f, err := n.Float64()
	if err != nil {
		return nil, fmt.Errorf("json number %s: %w", n, err)
	}
	if f == math.Trunc(f) && math.Abs(f) >= 1<<53 {
		return n, nil
	}
	return f, nil
}

func mergeStructIntoMap(before, after any, dest map[string]any) error {
	bv := reflect.Indirect(reflect.ValueOf(before))
	av := reflect.Indirect(reflect.ValueOf(after))
	if !bv.IsValid() || !av.IsValid() || av.Kind() != reflect.Struct || bv.Type() != av.Type() {
		return marshalMergeIntoMap(after, dest)
	}
	return mergeFields(bv, av, slices.Sorted(maps.Keys(dest)), dest)
}

func mergeFields(bv, av reflect.Value, keys []string, dest map[string]any) error {
	var wireB, wireA map[string]json.RawMessage
	for _, f := range jsonFields(av.Type()) {
		var b []byte
		if ov, nv := fieldOrZero(bv, f.index), fieldOrZero(av, f.index); ov.CanInterface() && nv.CanInterface() {
			oldVal := ov.Interface()
			newVal := nv.Interface()
			if reflect.DeepEqual(oldVal, newVal) {
				continue
			}
			var err error
			if b, err = json.Marshal(newVal); err != nil {
				return err
			}
		} else {
			if wireA == nil {
				var err error
				if wireB, err = encodeWire(bv); err != nil {
					return err
				}
				if wireA, err = encodeWire(av); err != nil {
					return err
				}
			}
			if bytes.Equal(wireB[f.name], wireA[f.name]) {
				continue
			}
			if b = wireA[f.name]; b == nil {
				b = []byte("null")
			}
		}
		var decoded any
		if err := decodeHookJSON(b, &decoded); err != nil {
			return err
		}
		wrote := false
		for _, k := range keys {
			if casing.ToCamel(k) == casing.ToCamel(f.name) {
				dest[k] = decoded
				wrote = true
			}
		}
		if !wrote {
			dest[casing.ToSnake(f.name)] = decoded
		}
	}
	return nil
}

func encodeWire(v reflect.Value) (map[string]json.RawMessage, error) {
	b, err := json.Marshal(v.Interface())
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

type jsonField struct {
	name   string
	index  []int
	tagged bool
}

// jsonFields mirrors encoding/json's typeFields.
func jsonFields(t reflect.Type) []jsonField {
	var all []jsonField
	depthOf := map[string]int{}
	var walk func(t reflect.Type, index []int, seen []reflect.Type)
	walk = func(t reflect.Type, index []int, seen []reflect.Type) {
		if slices.Contains(seen, t) {
			return
		}
		seen = append(seen, t)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			at := append(slices.Clone(index), i)
			ft := f.Type
			if ft.Name() == "" && ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if !f.IsExported() && (!f.Anonymous || ft.Kind() != reflect.Struct) {
				continue
			}
			name, skip := jsonTagName(f)
			if skip {
				continue
			}
			if name == "" && f.Anonymous && ft.Kind() == reflect.Struct {
				walk(ft, at, seen)
				continue
			}
			tagged := name != ""
			if !tagged {
				name = f.Name
			}
			all = append(all, jsonField{name: name, index: at, tagged: tagged})
			if d, ok := depthOf[name]; !ok || len(at) < d {
				depthOf[name] = len(at)
			}
		}
	}
	walk(t, nil, nil)

	var out []jsonField
	for _, name := range slices.Sorted(maps.Keys(depthOf)) {
		var top []jsonField
		for _, f := range all {
			if f.name == name && len(f.index) == depthOf[name] {
				top = append(top, f)
			}
		}
		if len(top) > 1 {
			top = slices.DeleteFunc(top, func(f jsonField) bool { return !f.tagged })
		}
		if len(top) == 1 {
			out = append(out, top[0])
		}
	}
	slices.SortFunc(out, func(a, b jsonField) int { return slices.Compare(a.index, b.index) })
	return out
}

func fieldOrZero(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Zero(fieldType(v.Type().Elem(), index))
			}
			v = v.Elem()
		}
		v = v.Field(i)
		index = index[1:]
	}
	return v
}

func fieldType(t reflect.Type, index []int) reflect.Type {
	for _, i := range index {
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		t = t.Field(i).Type
	}
	return t
}

func jsonTagName(f reflect.StructField) (name string, skip bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", true
	}
	name, _, _ = strings.Cut(tag, ",")
	if !validJSONTagName(name) {
		return "", false
	}
	return name, false
}

// validJSONTagName is encoding/json's isValidTag.
func validJSONTagName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
}

func marshalMergeIntoMap(src any, dest map[string]any) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	var fresh map[string]any
	if err := decodeHookJSON(b, &fresh); err != nil {
		return err
	}
	snake := casing.MapToSnake(fresh)
	maps.Copy(dest, snake)
	return nil
}
