package telemetryui

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The group's policy refuses an unadmitted caller before a screen
// renders. The screen asks again on its own, so its data never depends
// on that policy being in front of it.
func TestScreenDrawsNothingForAnUnadmittedCaller(t *testing.T) {
	drew := false
	s := &screen{
		allowed: func(context.Context) bool { return false },
		draw:    func(context.Context) render.HTML { drew = true; return "secret" },
	}
	if got := s.RenderCtx(context.Background()); got != "" || drew {
		t.Fatalf("an unadmitted caller got %q (drew=%v)", got, drew)
	}
	s.allowed = func(context.Context) bool { return true }
	if got := s.RenderCtx(context.Background()); got != "secret" {
		t.Fatalf("an admitted caller got %q", got)
	}
}
