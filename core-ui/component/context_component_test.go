package component_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type plainComp struct{}

func (plainComp) Render() render.HTML { return render.HTML("plain") }

type ctxKey struct{}

type ctxAwareComp struct{}

func (ctxAwareComp) Render() render.HTML { return render.HTML("fallback") }
func (ctxAwareComp) RenderCtx(ctx context.Context) render.HTML {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return render.HTML("ctx:" + v)
	}
	return render.HTML("ctx:nil")
}

func TestSafeRenderCtx_PrefersContextComponent(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKey{}, "hello")
	got, err := component.SafeRenderCtx(ctx, ctxAwareComp{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(string(got), "ctx:hello") {
		t.Fatalf("want RenderCtx output, got %q", got)
	}
}

func TestSafeRenderCtx_FallsBackToRender(t *testing.T) {
	got, err := component.SafeRenderCtx(context.Background(), plainComp{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if string(got) != "plain" {
		t.Fatalf("want plain Render output, got %q", got)
	}
}

// TestContextOnly_EmbedSatisfiesComponent pins the no-boilerplate
// pattern: a type embedding component.ContextOnly + defining only
// RenderCtx is a valid Component and the framework dispatch routes to
// RenderCtx.
type ctxOnlyComp struct {
	component.ContextOnly
}

func (c *ctxOnlyComp) RenderCtx(ctx context.Context) render.HTML {
	return render.HTML("ctx-only")
}

func TestContextOnly_EmbedSatisfiesComponent(t *testing.T) {
	var c component.Component = &ctxOnlyComp{} // compile-time check
	got, err := component.SafeRenderCtx(context.Background(), c)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(got) != "ctx-only" {
		t.Fatalf("want ctx-only, got %q (ContextOnly's stub Render should not have been called)", got)
	}
}

func TestSafeRender_DelegatesToContext(t *testing.T) {
	got, err := component.SafeRender(ctxAwareComp{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(string(got), "ctx:nil") {
		t.Fatalf("SafeRender should use RenderCtx with bg ctx, got %q", got)
	}
}
