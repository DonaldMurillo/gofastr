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

// Typed hooks
//
// The framework's HookRegistry stores hooks as func(ctx, any) error to keep
// the declarative path (entity declarations from JSON) untyped. Generated
// typed repositories want hook callbacks that receive *T directly. The
// helpers below register an untyped wrapper that marshals the hook payload
// through *T and back, so existing untyped hooks and new typed ones can
// coexist on the same entity.
//
// Casing: hook payloads inside the framework are snake_cased map[string]any
// (because unconvertMapKeys runs before hooks fire). Generated structs use
// JSON tags in camelCase. The wrappers translate via casing.MapToCamel /
// casing.MapToSnake so json.Marshal/Unmarshal round-trips correctly.

// OnBeforeCreate registers a typed BeforeCreate hook on the entity named
// `name`. Mutations the callback makes to *T are reflected back into the
// pending body so the subsequent INSERT picks them up.
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

// OnAfterCreate registers a typed AfterCreate hook. The callback receives
// the just-inserted row (already includes server-generated fields like id).
// Mutations are reflected back into the response body: the hook payload is
// the live result map the crud layer serialises into the HTTP response, so
// clearing a field here (e.g. redacting a secret) changes what the caller
// reads. The stored row is unaffected. Create has already committed it.
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

// OnBeforeUpdate registers a typed BeforeUpdate hook. *T is sparse, it
// holds whatever the caller sent, not the full row.
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

// OnAfterUpdate registers a typed AfterUpdate hook receiving the post-update
// row. Mutations are reflected back into the response body, exactly like
// OnAfterCreate: the hook payload is the live result map the crud layer
// serialises into the HTTP response, so clearing a field here redacts it from
// what the caller reads. The stored row is unaffected.
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

// OnBeforeDelete registers a typed BeforeDelete hook. The payload is the
// record id; no generic parameter needed.
func OnBeforeDelete(app *App, name string, fn func(ctx context.Context, id string) error) {
	registerIDHook(app, name, hook.BeforeDelete, "BeforeDelete", fn)
}

// OnAfterDelete registers a typed AfterDelete hook. Same shape as
// OnBeforeDelete.
func OnAfterDelete(app *App, name string, fn func(ctx context.Context, id string) error) {
	registerIDHook(app, name, hook.AfterDelete, "AfterDelete", fn)
}

// OnBeforeList registers a typed BeforeList hook. The callback receives
// the framework's *hook.ListPayload directly so callers can append WHERE
// clauses (p.AddWhere) without type-asserting from any. Symmetric with
// OnBeforeCreate/OnBeforeUpdate.
func OnBeforeList(app *App, name string, fn func(ctx context.Context, p *hook.ListPayload) error) {
	registerPayloadHook(app, name, hook.BeforeList, "BeforeList", fn)
}

// OnAfterList registers a typed AfterList hook. The callback receives
// the *hook.ListPayload with Results populated, mutate the slice in
// place to redact / drop rows.
func OnAfterList(app *App, name string, fn func(ctx context.Context, p *hook.ListPayload) error) {
	registerPayloadHook(app, name, hook.AfterList, "AfterList", fn)
}

// OnBeforeGet registers a typed BeforeGet hook. The callback receives
// *hook.GetPayload; ID is the request's path-value and AddWhere lets
// the host scope the lookup (mismatch → 404).
func OnBeforeGet(app *App, name string, fn func(ctx context.Context, p *hook.GetPayload) error) {
	registerPayloadHook(app, name, hook.BeforeGet, "BeforeGet", fn)
}

// OnAfterGet registers a typed AfterGet hook. The callback receives
// *hook.GetPayload with Result populated, mutate the map in place to
// redact fields before the response is serialised.
func OnAfterGet(app *App, name string, fn func(ctx context.Context, p *hook.GetPayload) error) {
	registerPayloadHook(app, name, hook.AfterGet, "AfterGet", fn)
}

// registerPayloadHook registers a typed hook whose payload is a framework
// pointer type (*hook.ListPayload, *hook.GetPayload): the wrapper asserts
// the concrete payload and forwards it, refusing nil or foreign types
// with an error naming the drift. It replaces the four former per-hook
// copies inside OnBeforeList / OnAfterList / OnBeforeGet / OnAfterGet.
func registerPayloadHook[P any](app *App, name string, ht hook.HookType, label string, fn func(ctx context.Context, p *P) error) {
	app.HookRegistry(name).RegisterHook(ht, func(ctx context.Context, data any) error {
		p, ok := data.(*P)
		if !ok || p == nil {
			return fmt.Errorf("typed %s hook: payload type = %T, want %s (framework contract drift?)", label, data, reflect.TypeOf((*P)(nil)))
		}
		return fn(ctx, p)
	})
}

// registerIDHook registers a typed hook whose payload is the record id
// (BeforeDelete / AfterDelete). It replaces the two former per-hook
// copies inside OnBeforeDelete / OnAfterDelete.
func registerIDHook(app *App, name string, ht hook.HookType, label string, fn func(ctx context.Context, id string) error) {
	app.HookRegistry(name).RegisterHook(ht, func(ctx context.Context, data any) error {
		id, ok := data.(string)
		if !ok {
			return fmt.Errorf("typed %s hook: payload type = %T, want string (framework contract drift?)", label, data)
		}
		return fn(ctx, id)
	})
}

// hookBodyPhrase is what a client reads when a typed Before hook cannot
// decode the write body into its struct.
var hookBodyPhrase = i18n.Define("framework.hook_body_type", "a value in the request body does not match its field's type")

// hookBodyError is a typed Before hook's failure to decode the body. The
// crud layer answers a Before hook's error with a 400 carrying its text,
// and encoding/json's text names the hook's Go struct and field types.
// The response carries the phrase; Error and Unwrap keep the detail for
// logs and errors.As.
type hookBodyError struct{ err error }

func (e *hookBodyError) Error() string { return e.err.Error() }
func (e *hookBodyError) Unwrap() error { return e.err }

// Phrased implements i18n.Phrased.
func (e *hookBodyError) Phrased() (i18n.Phrase, map[string]any) { return hookBodyPhrase, nil }

// unmarshalHookPayload converts the framework's hook payload (snake-cased
// map[string]any for create/update, string for delete) into a typed value.
// Maps are pre-converted to camelCase so json tags on generated structs line
// up.
func unmarshalHookPayload(data any, dest any) error {
	if m, ok := data.(map[string]any); ok {
		camel := casing.MapToCamel(m)
		b, err := json.Marshal(camel)
		if err != nil {
			return err
		}
		return decodeHookJSON(b, dest)
	}
	// Non-map payload (e.g. delete passes a string id), round-trip through
	// JSON so callers can use *string or even custom typed wrappers.
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return jsonexact.Unmarshal(b, dest)
}

// snapshotDeepCopy returns a deep copy of v via a JSON round-trip, so the
// map, slice, and pointer fields of the snapshot stop aliasing v's. A
// shallow struct copy (`before := v`) shares those backing stores, so an
// in-place mutation by the hook callback (delete v.Meta[k], v.Tags[0]=…,
// v.Profile.Secret="") leaves before and v DeepEqual at merge time and the
// changed field is skipped: the client's original nested value survives
// into the persisted row / response while the host believes its hook
// redacted it. Round-tripping through JSON is the same translation
// unmarshalHookPayload already performs, so any *T this wrapper can
// unmarshal can also be snapshotted. On any marshal failure it degrades to
// the shallow copy (the previous behaviour) rather than refusing the hook.
func snapshotDeepCopy[T any](v T) T {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out T
	if err := decodeHookJSON(b, &out); err != nil {
		return v
	}
	return out
}

// decodeHookJSON decodes b into dest with every JSON number exact: where
// dest holds an interface value (a map[string]any field, an []any), an
// integer literal lands as int64 and any other as float64, the types the
// untyped hook body carries. json.Unmarshal read each as a float64, so a
// hook that edited a JSON field wrote 9007199254740993 back as
// 9007199254740992.
func decodeHookJSON(b []byte, dest any) error {
	return jsonexact.Decode(b, dest, exactHookNumber)
}

// exactHookNumber is n as int64 when it is an integer literal in range,
// else as float64. An integer float64 would round (past int64, or spelled
// with an exponent) stays the json.Number, as the crud body decode keeps
// it, so a hook that edits a neighbouring key does not rewrite its digits.
// A literal out of float64's range (1e400) is an error, as json.Unmarshal
// reports it.
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

// mergeStructIntoMap reflects struct mutations from the typed Before-hook
// callback back into the snake-cased payload map. It diffs the pre-hook
// struct (`before`) against the post-hook struct (`after`) and writes back
// ONLY the fields the hook actually changed, including changes TO a zero
// value (false, 0, "").
//
// Why not just marshal `after`: generated structs tag every field
// `,omitempty` (cmd/gofastr/internal/appgen/generate.go), so json.Marshal silently drops a
// field a defensive hook forced to its zero value. Relying on that
// marshalling makes a hook that does `value.IsAdmin = false` (to override
// attacker-supplied is_admin=true) a no-op, the client's value survives
// into the SET clause. The diff is computed via reflection on the struct
// fields so a forced-zero override lands in dest, while untouched fields on
// a sparse BeforeUpdate body are left alone (we don't add fields the hook
// never touched). Only top-level fields are merged.
func mergeStructIntoMap(before, after any, dest map[string]any) error {
	bv := reflect.Indirect(reflect.ValueOf(before))
	av := reflect.Indirect(reflect.ValueOf(after))
	if !bv.IsValid() || !av.IsValid() || av.Kind() != reflect.Struct || bv.Type() != av.Type() {
		// Fall back to the additive marshal merge for non-struct payloads.
		return marshalMergeIntoMap(after, dest)
	}
	return mergeFields(bv, av, slices.Sorted(maps.Keys(dest)), dest)
}

// mergeFields writes each field JSON would encode that differs between
// bv and av into dest. The field set is encoding/json's: fields promoted
// from an embedded struct whose tag gives no name count, whether or not
// its type is exported, and a name held by several fields goes to the
// shallowest, then to the one tagged with it, else to none. So a hook
// writing an embedded field an outer one shadows changes nothing, as it
// would change nothing in the JSON. A field under a nil embedded pointer
// reads as its zero value, so a hook that drops one clears the fields it
// promoted.
func mergeFields(bv, av reflect.Value, keys []string, dest map[string]any) error {
	var wireB, wireA map[string]json.RawMessage
	for _, f := range jsonFields(av.Type()) {
		var b []byte
		// A nil pointer on one side gives a readable zero value while the
		// other side still holds the unexported field, so both must read.
		if ov, nv := fieldOrZero(bv, f.index), fieldOrZero(av, f.index); ov.CanInterface() && nv.CanInterface() {
			oldVal := ov.Interface()
			newVal := nv.Interface()
			if reflect.DeepEqual(oldVal, newVal) {
				continue // hook left this field untouched, don't widen the body
			}
			// Round-trip the new value through JSON so it matches the shape
			// the untyped path expects (integers as int64, other numbers as
			// float64, time as RFC3339, etc.).
			var err error
			if b, err = json.Marshal(newVal); err != nil {
				return err
			}
		} else {
			// An unexported embedded struct named by its tag: reflection
			// cannot read it out, so read its key from the encoded struct.
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
		// The payload's own key wins: an After payload is the response map,
		// keyed by wire (camelCase) names, and a digit-segment column
		// (line_2) does not survive ToCamel then ToSnake. A field the payload
		// lacks gets the snake_case column name.
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

// encodeWire is v encoded as JSON, keyed by field name.
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

// jsonField is one field of a struct as encoding/json sees it.
type jsonField struct {
	name   string
	index  []int
	tagged bool
}

// jsonFields lists the fields encoding/json encodes for t, in field order,
// each under the name it encodes as.
func jsonFields(t reflect.Type) []jsonField {
	var all []jsonField
	depthOf := map[string]int{}
	var walk func(t reflect.Type, index []int, seen []reflect.Type)
	walk = func(t reflect.Type, index []int, seen []reflect.Type) {
		if slices.Contains(seen, t) {
			return // a type that embeds itself through a pointer
		}
		seen = append(seen, t)
		// The steps and their order are encoding/json's typeFields.
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			at := append(slices.Clone(index), i)
			ft := f.Type
			if ft.Name() == "" && ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			// An unexported embedded struct still counts: its exported
			// fields promote, or it is a named field under its tag.
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

// fieldOrZero is v's field at index, reading through embedded pointers;
// a nil one on the way gives the field's zero value.
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

// fieldType is the type of the field at index under struct type t.
func fieldType(t reflect.Type, index []int) reflect.Type {
	for _, i := range index {
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		t = t.Field(i).Type
	}
	return t
}

// jsonTagName returns the name a field's json tag gives it, "" when the
// tag gives none or one encoding/json rejects, and whether the field is
// JSON-skipped (`json:"-"`). Options like omitempty are ignored.
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

// validJSONTagName is encoding/json's isValidTag: letters, digits and a
// set of punctuation, which excludes quotes and backslashes.
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

// marshalMergeIntoMap is the additive fallback used when the diff path can't
// apply (non-struct payloads). It mirrors the original behaviour, with
// numbers decoded exact the way the struct path decodes them.
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
