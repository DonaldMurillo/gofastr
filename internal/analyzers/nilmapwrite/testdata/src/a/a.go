// Package a reduces crud's typed UpdateOne before and after 0a5181f4b,
// with the postures the rule leaves alone.
package a

import "context"

type hookFunc func(ctx context.Context, data map[string]any) error

type Handler struct {
	hooks  map[string][]hookFunc
	owner  string
	render func(attrs map[string]string) string
	sink   Sink
}

type Sink interface{ Take(m map[string]any) }

func (h *Handler) runHooks(ctx context.Context, kind string, data map[string]any) error {
	for _, fn := range h.hooks[kind] {
		if err := fn(ctx, data); err != nil {
			return err
		}
	}
	return nil
}

// doUpdate is the pre-fix path: body goes to the hooks as it came.
func (h *Handler) doUpdate(ctx context.Context, body map[string]any) error {
	return h.runHooks(ctx, "before_update", body)
}

// UpdateOne is the pre-fix entry: nil reaches the hooks.
func (h *Handler) UpdateOne(ctx context.Context, body map[string]any) error {
	return h.doUpdate(ctx, body) // want `UpdateOne hands its map parameter body to a callback`
}

func (h *Handler) doUpdateFixed(ctx context.Context, body map[string]any) error {
	if body == nil {
		body = map[string]any{}
	}
	return h.runHooks(ctx, "before_update", body)
}

// UpdateFixed is the fix: doUpdateFixed starts from an empty map.
func (h *Handler) UpdateFixed(ctx context.Context, body map[string]any) error {
	return h.doUpdateFixed(ctx, body)
}

// RunDirect hands the map to a func-typed field.
func (h *Handler) RunDirect(attrs map[string]string) string {
	return h.render(attrs) // want `RunDirect hands its map parameter attrs to a callback`
}

// Give hands the map to a host-installed interface.
func (h *Handler) Give(m map[string]any) {
	h.sink.Take(m) // want `Give hands its map parameter m to a callback`
}

func Guarded(fn hookFunc, m map[string]any) error {
	if len(m) == 0 {
		m = map[string]any{}
	}
	return fn(context.Background(), m)
}

// Render is the working-copy shape: it sets attrs then hands them on.
func Render(attrs map[string]string, el func(map[string]string) string) string {
	attrs["type"] = "button" // want `Render writes into its map parameter attrs and then hands it on`
	return el(attrs)
}

func Flag(a map[string]string, name string, on bool) map[string]string {
	if on {
		a[name] = "" // want `Flag writes into its map parameter a and then hands it on`
	}
	return a
}

func FlagFixed(a map[string]string, name string, on bool) map[string]string {
	if a == nil {
		a = map[string]string{}
	}
	if on {
		a[name] = ""
	}
	return a
}

// InjectOwner only fills data in place: its caller owns passing a map.
func (h *Handler) InjectOwner(data map[string]any) {
	if h.owner != "" {
		data["owner_id"] = h.owner
	}
}

func Count(seen map[string]int, key string) {
	seen[key]++
	_ = len(seen)
}

func ReadOnly(m map[string]string) string { return m["k"] }

func PassToDeclared(m map[string]string) string { return ReadOnly(m) }

func DeleteOnly(m map[string]string) { delete(m, "k") }

func unexported(fn hookFunc, m map[string]any) error { return fn(context.Background(), m) }

type executor interface{ Execute(data any) error }

// Exec hands the map on as any: the template reads it.
func Exec(t executor, data map[string]any) error { return t.Execute(data) }

func readGeneric[K comparable](m map[K]int) int { return len(m) }

// Inst names a declared generic function, not a callback.
func Inst(m map[string]int) int { return readGeneric[string](m) }
