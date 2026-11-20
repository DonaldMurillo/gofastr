package store

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Slice is a typed handle to one declared shared value.
type Slice[T any] struct {
	name  string
	scope Scope
	def   T
	comp  *computedCfg
}

type computedCfg struct {
	reducer string
	deps    []string
}

// Name returns the fully-qualified slice name (the client signal key).
func (sl *Slice[T]) Name() string { return sl.name }

// Default returns the declared default value.
func (sl *Slice[T]) Default() T { return sl.def }

// Scope returns the slice's seeding scope.
func (sl *Slice[T]) Scope() Scope { return sl.scope }

// Global marks the slice as seeded on every page and kept across navigation.
func (sl *Slice[T]) Global() *Slice[T] {
	sl.scope = ScopeGlobal
	setScope(sl.name, ScopeGlobal)
	return sl
}

// Seed sets the slice's value for this request; a no-op outside the UI host.
func (sl *Slice[T]) Seed(ctx context.Context, v T) {
	if b := valuesFrom(ctx); b != nil {
		b.mu.Lock()
		b.m[sl.name] = v
		b.mu.Unlock()
	}
}

// Value returns the slice's value for this request, else its default.
func (sl *Slice[T]) Value(ctx context.Context) T { return sl.resolve(ctx) }

func (sl *Slice[T]) resolve(ctx context.Context) T {
	if b := valuesFrom(ctx); b != nil {
		if IsRouteName(sl.name) {
			b.mu.Lock()
			b.routeRead = true
			b.mu.Unlock()
		}
		b.mu.Lock()
		raw, ok := b.m[sl.name]
		b.mu.Unlock()
		if ok {
			if tv, ok := raw.(T); ok {
				return tv
			}
		}
	}
	return sl.def
}

// Bind renders tag bound to the slice in text mode, with the value as its text.
func (sl *Slice[T]) Bind(ctx context.Context, tag string, attrs map[string]string) render.HTML {
	a := cloneAttrs(attrs)
	a["data-cui-signal"] = sl.name
	sl.applyComputed(a)
	return renderEl(tag, a, render.Text(valueString(any(sl.resolve(ctx)))))
}

func (sl *Slice[T]) applyComputed(a map[string]string) {
	if sl.comp == nil {
		return
	}
	a["data-cui-computed"] = sl.comp.reducer
	a["data-cui-computed-deps"] = strings.Join(sl.comp.deps, ",")
}

// BindAttr renders tag with the slice bound to htmlAttr, stamped with the value.
func (sl *Slice[T]) BindAttr(ctx context.Context, tag, htmlAttr string, attrs map[string]string) render.HTML {
	a := cloneAttrs(attrs)
	if !interactive.SignalAttrAllowed(htmlAttr) {
		return renderEl(tag, a)
	}
	a["data-cui-signal"] = sl.name
	a["data-cui-signal-mode"] = "attr"
	a["data-cui-signal-attr"] = htmlAttr
	sl.applyComputed(a)
	v := any(sl.resolve(ctx))
	if booleanSignalAttrs[asciicase.Lower(htmlAttr)] {
		if booleanAttrOn(v) {
			a[htmlAttr] = ""
		} else {
			delete(a, htmlAttr)
		}
		return renderEl(tag, a)
	}
	a[htmlAttr] = sanitizeSignalURL(htmlAttr, valueString(v))
	return renderEl(tag, a)
}

var booleanSignalAttrs = map[string]bool{
	"disabled": true, "hidden": true, "checked": true, "open": true, "selected": true,
}

func booleanAttrOn(v any) bool {
	b, err := json.Marshal(v)
	if err != nil || len(b) == 0 {
		return false
	}
	switch b[0] {
	case 'n', 'f':
		return false
	case 't', '[', '{':
		return true
	case '"':
		var str string
		if json.Unmarshal(b, &str) != nil {
			return false
		}
		return str != "" && str != "false"
	default:
		f, err := strconv.ParseFloat(string(b), 64)
		return (err == nil || errors.Is(err, strconv.ErrRange)) && f != 0
	}
}

var urlBearingAttrs = map[string]bool{
	"href": true, "src": true, "action": true,
	"xlink:href": true, "formaction": true,
}

func sanitizeSignalURL(htmlAttr, value string) string {
	if !urlBearingAttrs[strings.ToLower(htmlAttr)] {
		return value
	}
	t := strings.ToLower(stripURLControlBytes(value))
	switch {
	case strings.HasPrefix(t, "javascript:"), strings.HasPrefix(t, "vbscript:"):
		return ""
	case strings.HasPrefix(t, "data:"):
		if !strings.HasPrefix(t, "data:image/") {
			return ""
		}
	}
	return value
}

func stripURLControlBytes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r <= 0x20 {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// BindHTML binds in html mode, unescaped: trusted values only.
func (sl *Slice[T]) BindHTML(ctx context.Context, tag string, attrs map[string]string) render.HTML {
	if isRouteSliceName(sl.name) {
		panic("store: route slices are text-only; BindHTML on " + sl.name + " would render user-influenced route state as markup")
	}
	a := cloneAttrs(attrs)
	a["data-cui-signal"] = sl.name
	a["data-cui-signal-mode"] = "html"
	sl.applyComputed(a)
	return renderEl(tag, a, render.HTML(valueString(any(sl.resolve(ctx)))))
}

func isRouteSliceName(name string) bool { return IsRouteName(name) }

// Publish makes a's 2xx answer the slice's new value on the client.
func (sl *Slice[T]) Publish(a interactive.Action) interactive.Action {
	return a.OnSuccess(interactive.SetSignal(sl.name))
}

type valuesKey struct{}

type values struct {
	mu        sync.Mutex
	m         map[string]any
	routeRead bool
	routeArea bool
}

// WithValues installs a fresh request value bag, and an id scope when ctx has none.
func WithValues(ctx context.Context) context.Context {
	if !render.HasIDScope(ctx) {
		ctx = render.WithIDScope(ctx)
	}
	return context.WithValue(ctx, valuesKey{}, &values{m: map[string]any{}})
}

func valuesFrom(ctx context.Context) *values {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(valuesKey{}).(*values)
	return v
}

var voidTags = map[string]bool{
	"img": true, "input": true, "br": true, "hr": true, "meta": true,
	"link": true, "area": true, "base": true, "col": true, "embed": true,
	"source": true, "track": true, "wbr": true,
}

func renderEl(tag string, attrs map[string]string, children ...render.HTML) render.HTML {
	if voidTags[tag] {
		return render.VoidTag(tag, attrs)
	}
	return render.Tag(tag, attrs, children...)
}

func cloneAttrs(attrs map[string]string) map[string]string {
	out := make(map[string]string, len(attrs)+3)
	maps.Copy(out, attrs)
	return out
}

func valueString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float32:
		if t == 0 {
			return "0"
		}
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return strconv.FormatFloat(t, 'f', -1, 64)
		}
		if t == 0 {
			return "0"
		}
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	default:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			return ""
		}
		out := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
		switch {
		case string(out) == "null":
			return ""
		case len(out) > 0 && out[0] == '"':
			var str string
			if json.Unmarshal(out, &str) == nil {
				return str
			}
		case len(out) > 0 && (out[0] == '{' || out[0] == '['):
			if js, err := jsKeyOrder(out); err == nil {
				return string(js)
			}
		}
		return string(out)
	}
}

func jsKeyOrder(raw []byte) ([]byte, error) {
	if len(raw) == 0 || (raw[0] != '{' && raw[0] != '[') {
		return raw, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if raw[0] == '[' {
		out.WriteByte('[')
		for i := 0; dec.More(); i++ {
			var v json.RawMessage
			if err := dec.Decode(&v); err != nil {
				return nil, err
			}
			js, err := jsKeyOrder(v)
			if err != nil {
				return nil, err
			}
			if i > 0 {
				out.WriteByte(',')
			}
			out.Write(js)
		}
		out.WriteByte(']')
		return out.Bytes(), nil
	}
	type member struct {
		key   string
		index uint64
		isIdx bool
		val   []byte
	}
	var members []member
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		js, err := jsKeyOrder(v)
		if err != nil {
			return nil, err
		}
		idx, isIdx := arrayIndex(key)
		members = append(members, member{key: key, index: idx, isIdx: isIdx, val: js})
	}
	slices.SortStableFunc(members, func(a, b member) int {
		switch {
		case a.isIdx && b.isIdx:
			return cmp.Compare(a.index, b.index)
		case a.isIdx:
			return -1
		case b.isIdx:
			return 1
		}
		return 0
	})
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	out.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			out.WriteByte(',')
		}
		if err := enc.Encode(m.key); err != nil {
			return nil, err
		}
		out.Truncate(out.Len() - 1)
		out.WriteByte(':')
		out.Write(m.val)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

func arrayIndex(key string) (uint64, bool) {
	if key == "" || len(key) > 10 || (len(key) > 1 && key[0] == '0') {
		return 0, false
	}
	var n uint64
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + uint64(c-'0')
	}
	return n, n < 1<<32-1
}
