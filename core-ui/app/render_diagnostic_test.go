package app

import (
	"context"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
	"strings"
	"testing"
)

type brokenRenderBoundary struct{}

func (brokenRenderBoundary) RenderError(error) render.HTML { panic("fallback exploded") }

func TestLayoutRecoveryReportsPanic(t *testing.T) {
	for _, which := range []string{"build", "area", "fallback"} {
		t.Run(which, func(t *testing.T) {
			var messages []string
			ctx := renderdiag.WithObserver(context.Background(), func(s string) { messages = append(messages, s) })
			switch which {
			case "build":
				containedBuild(ctx, func(context.Context, *LayoutTree) render.HTML { panic("build exploded") }, nil)
			case "area":
				containedArea(ctx, func(context.Context, Match) render.HTML { panic("area exploded") }, Match{})
			case "fallback":
				containedRenderError(ctx, brokenRenderBoundary{}, nil)
			}
			if len(messages) != 1 || !strings.Contains(messages[0], which+" exploded") {
				t.Fatalf("recovered %s panic unreported: %v", which, messages)
			}
		})
	}
}
