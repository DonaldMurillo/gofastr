package store

import (
	"context"
	"strings"
	"testing"
)

// TestRouteFamilyRefusesBindHTML (DESIGN "Route signals": every value
// is text): the route family refuses html mode outright — the path and
// title carry user-influenced text, params and query come from the URL,
// so BindHTML panics naming the slice while Bind and BindAttr keep
// working. The guard is the emitter (slice.go), not a runtime warning
// after the markup shipped.
func TestRouteFamilyRefusesBindHTML(t *testing.T) {
	slices := map[string]*Slice[string]{
		"route.path":      Route.Path,
		"route.title":     Route.Title,
		"route.params.id": Route.Param("id"),
		"route.query.q":   Route.Query("q"),
	}
	for name, sl := range slices {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("%s.BindHTML must panic — every route value is text", name)
					return
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, name) {
					t.Errorf("%s.BindHTML panic must name the slice, got: %v", name, r)
				}
			}()
			_ = sl.BindHTML(WithValues(context.Background()), "div", nil)
		}()
		// The text and attr modes keep working.
		ctx := WithValues(context.Background())
		if html := sl.Bind(ctx, "span", nil); html == "" {
			t.Errorf("%s.Bind rendered nothing", name)
		}
		if html := sl.BindAttr(ctx, "span", "data-x", nil); html == "" {
			t.Errorf("%s.BindAttr rendered nothing", name)
		}
	}
}
