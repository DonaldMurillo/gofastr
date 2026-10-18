package app

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestRouteAreaContentsKeepsSwapAddress(t *testing.T) {
	tree := &LayoutTree{ctx: context.Background(), key: "docs", spec: &LayoutSpec{Areas: []AreaSpec{{Name: "header", Contents: true}}}}
	out := string(tree.RouteArea("header", func(context.Context, Match) render.HTML { return render.Raw("<header>Navigation</header>") }))
	for _, want := range []string{`data-cui-area="docs~header"`, `data-cui-comp="cui-layout-area"`, `<header>Navigation</header>`} {
		if !strings.Contains(out, want) {
			t.Fatalf("contents area missing %s: %s", want, out)
		}
	}
}
