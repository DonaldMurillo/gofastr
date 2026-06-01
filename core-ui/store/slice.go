package store

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Slice is a typed handle to one declared shared-state value. It is a
// renderer first: its binding helpers emit both the data-fui-signal
// attribute AND the resolved initial value, so the SSR DOM and the
// seeded client store can never drift.
type Slice[T any] struct {
	name  string
	scope Scope
	def   T
	comp  *computedCfg // non-nil for Computed slices
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

// Global marks the slice app-global: seeded on every page and preserved
// across client-side navigation.
func (sl *Slice[T]) Global() *Slice[T] {
	sl.scope = ScopeGlobal
	setScope(sl.name, ScopeGlobal)
	return sl
}

// Seed sets the per-request authoritative value (called by the producer
// — a screen loader or island — at render time). No-op if the request
// context carries no value bag (i.e. rendered outside the UI host).
func (sl *Slice[T]) Seed(ctx context.Context, v T) {
	if b := valuesFrom(ctx); b != nil {
		b.mu.Lock()
		b.m[sl.name] = v
		b.mu.Unlock()
	}
}

// resolve returns the request value if the producer seeded one, else the
// declared default.
func (sl *Slice[T]) resolve(ctx context.Context) T {
	if b := valuesFrom(ctx); b != nil {
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

// Bind renders a read-only consumer element bound to the slice in text
// mode, stamping the resolved value as its text content.
func (sl *Slice[T]) Bind(ctx context.Context, tag string, attrs map[string]string) render.HTML {
	a := cloneAttrs(attrs)
	a["data-fui-signal"] = sl.name
	sl.applyComputed(a)
	return renderEl(tag, a, render.Text(valueString(any(sl.resolve(ctx)))))
}

// applyComputed stamps the computed-wiring attributes when this slice is
// a Computed. The runtime's computed module reads them to recompute the
// value client-side whenever a dependency changes.
func (sl *Slice[T]) applyComputed(a map[string]string) {
	if sl.comp == nil {
		return
	}
	a["data-fui-computed"] = sl.comp.reducer
	a["data-fui-computed-deps"] = strings.Join(sl.comp.deps, ",")
}

// BindAttr binds the slice value to an HTML attribute (attr mode),
// stamping the resolved value into that attribute. URL-bearing attrs are
// guarded against dangerous schemes both at SSR (sanitizeSignalURL below)
// AND on every client-side update by the runtime (_isUnsafeSignalUrl) —
// defense-in-depth parity, because a producer may Seed a request-
// influenced URL into a URL-bound slice.
func (sl *Slice[T]) BindAttr(ctx context.Context, tag, htmlAttr string, attrs map[string]string) render.HTML {
	a := cloneAttrs(attrs)
	a["data-fui-signal"] = sl.name
	a["data-fui-signal-mode"] = "attr"
	a["data-fui-signal-attr"] = htmlAttr
	a[htmlAttr] = sanitizeSignalURL(htmlAttr, valueString(any(sl.resolve(ctx))))
	return renderEl(tag, a)
}

// urlBearingAttrs are the HTML attributes whose value is a URL the
// browser will navigate/fetch. They mirror the runtime's
// _isUnsafeSignalUrl allow-list exactly (core-ui/runtime/runtime.js) so
// the SSR initial paint and client-side updates apply the identical
// scheme guard.
var urlBearingAttrs = map[string]bool{
	"href": true, "src": true, "action": true,
	"xlink:href": true, "formaction": true,
}

// sanitizeSignalURL blanks a value bound to a URL-bearing attribute when
// it carries a dangerous scheme (javascript:/vbscript:/non-image data:).
// Non-URL attributes are returned unchanged. The strip step removes ASCII
// whitespace + C0 control bytes before scheme detection, matching the
// runtime guard (browsers drop those during URL parsing, so an interior
// tab/newline must not defeat the prefix check).
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

// stripURLControlBytes removes every ASCII whitespace and C0 control byte
// (0x00–0x1f) from s — the chars browsers strip during URL parsing.
func stripURLControlBytes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r <= 0x20 { // covers C0 controls (0x00-0x1f) + space (0x20)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// BindHTML binds in html mode (innerHTML). TRUSTED VALUES ONLY — the
// value is written without escaping. Use Bind (text mode) for any value
// that can be influenced by users.
func (sl *Slice[T]) BindHTML(ctx context.Context, tag string, attrs map[string]string) render.HTML {
	a := cloneAttrs(attrs)
	a["data-fui-signal"] = sl.name
	a["data-fui-signal-mode"] = "html"
	return renderEl(tag, a, render.HTML(valueString(any(sl.resolve(ctx)))))
}

// Publish connects a producer's RPC to this slice: on a 2xx response the
// runtime treats the response body as the new signal value and fans it
// out to every consumer client-side — no server re-render of consumers.
// Sugar over interactive.SetSignal(sl.Name()).
//
//	interactive.OnClick(btn, company.Publish(interactive.Post("/rename")))
func (sl *Slice[T]) Publish(a interactive.Action) interactive.Action {
	return a.OnSuccess(interactive.SetSignal(sl.name))
}

// ─── request-scoped value bag ────────────────────────────────────────

type valuesKey struct{}

type values struct {
	mu sync.Mutex
	m  map[string]any
}

// WithValues installs a fresh request-scoped value bag on ctx. The UI
// host calls this before rendering a page so producers can Seed values
// the seed resolver later reads.
func WithValues(ctx context.Context) context.Context {
	return context.WithValue(ctx, valuesKey{}, &values{m: map[string]any{}})
}

func valuesFrom(ctx context.Context) *values {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(valuesKey{}).(*values)
	return v
}

// ─── helpers ─────────────────────────────────────────────────────────

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
	for k, v := range attrs {
		out[k] = v
	}
	return out
}

// valueString renders a value to the same string the runtime's text/attr
// signal renderer would produce, so the stamped SSR value matches the
// seeded value exactly (the single-source-of-truth invariant).
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
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
