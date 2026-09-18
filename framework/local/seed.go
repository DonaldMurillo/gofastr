package local

import (
	"context"
	"fmt"
	"maps"

	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// SeededSignal joins one record to one core-ui/store slice. The server
// renders the slice's value (its default, or what Seed put on the
// request); after hydration the runtime reads the record and patches
// the signal in place, then writes every later value of the signal
// back to the record and mirrors another tab's write in. The marker
// rides on the bindings, so a page that never binds the slice never
// restores it, the same rule store.Persist keeps.
//
// The record cannot appear at FIRST PAINT: IndexedDB is asynchronous by
// construction. A screen that must not flash the default needs the
// value on the request, a Mirror collection read with Get, not a
// seeded signal.
type SeededSignal[T any] struct {
	coll  *Collection[T]
	key   string
	slice *store.Slice[T]
}

// SeedSignal declares that slice is filled from record key of c. It
// panics on an invalid key and on a slice that is already browser-
// persisted through store.Persist (two owners of one value), and
// marks the slice app-global: the browser's value has to survive a
// client-side navigation, and the app-global merge rule is what keeps
// a partial render from clobbering it.
func SeedSignal[T any](c *Collection[T], key string, slice *store.Slice[T]) *SeededSignal[T] {
	if !validRecordKey(key) {
		panic(fmt.Sprintf("local: SeedSignal on %q: %q is not a valid key", c.def.name, key))
	}
	if slice.Persisted() {
		panic(fmt.Sprintf("local: SeedSignal on %q: slice %q is already store.Persist-ed — one owner per browser value", c.def.name, slice.Name()))
	}
	slice.Global()
	return &SeededSignal[T]{coll: c, key: key, slice: slice}
}

// Attrs returns the two marker attributes a binding carries:
// data-local-store="<app>" and data-local-seed="<collection>:<key>".
// The signal's own name is not among them: store.Slice.Bind already
// puts it on the same element as data-fui-signal, which is where a page
// script reads it.
func (s *SeededSignal[T]) Attrs() map[string]string {
	return map[string]string{
		"data-local-store": s.coll.def.store.app,
		"data-local-seed":  s.coll.def.name + ":" + s.key,
	}
}

func (s *SeededSignal[T]) withMarkers(attrs map[string]string) map[string]string {
	out := make(map[string]string, len(attrs)+2)
	maps.Copy(out, attrs)
	maps.Copy(out, s.Attrs())
	return out
}

// Bind renders the slice's text binding with the seed markers on it.
func (s *SeededSignal[T]) Bind(ctx context.Context, tag string, attrs map[string]string) render.HTML {
	return s.slice.Bind(ctx, tag, s.withMarkers(attrs))
}
