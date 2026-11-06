package component_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

func TestWidgetRendersContext(t *testing.T) {
	for _, child := range []component.Component{ctxAwareComp{}, &ctxOnlyComp{}, plainComp{}} {
		widget := component.NewWidget("cart", child)
		ctx := context.WithValue(context.Background(), ctxKey{}, "cart-user")
		want := component.RenderComponentCtx(ctx, child)
		got := component.RenderComponentCtx(ctx, widget)
		if !strings.Contains(string(got), string(want)) || string(want) == "" {
			t.Fatalf("widget %T = %q, want child %q", child, got, want)
		}
		want = component.RenderComponentCtx(context.Background(), child)
		if got := widget.Render(); !strings.Contains(string(got), string(want)) {
			t.Fatalf("background widget %T = %q, want child %q", child, got, want)
		}
	}
}

func TestCompositionPrefersContext(t *testing.T) {
	child := &ctxOnlyComp{}
	for _, got := range []string{string(component.RenderComponent(child)), string(component.ComponentList(child))} {
		if got != "ctx-only" {
			t.Fatalf("composition = %q, want ctx-only", got)
		}
	}
}
