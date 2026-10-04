package app

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

type rawPanicScreen struct{}

func (rawPanicScreen) Render() render.HTML { panic("test: raw boom") }

// TestRenderRawLayoutPanicIsTagged: RenderRaw tags a contained render
// panic with ErrScreenPanicked on the layout-backed branch, the same
// as the layout-less one, so a caller classifies both alike.
func TestRenderRawLayoutPanicIsTagged(t *testing.T) {
	for _, layout := range []bool{true, false} {
		r := NewRouter()
		var l *Layout
		if layout {
			l = bareShell("shell")
		}
		r.Screen(NewScreen("/boom", rawPanicScreen{}), l)
		_, err := r.RenderRaw("/boom")
		if !errors.Is(err, ErrScreenPanicked) {
			t.Errorf("layout=%v: RenderRaw error %v is not ErrScreenPanicked", layout, err)
		}
	}
}

// boundaryPanicsTwice panics in Render and again in its RenderError.
type boundaryPanicsTwice struct{}

func (boundaryPanicsTwice) Render() render.HTML           { panic("test: render boom") }
func (boundaryPanicsTwice) RenderError(error) render.HTML { panic("test: fallback boom") }

// TestBoundaryFallbackPanicIsContained: a RenderError that panics too
// is contained and tagged ErrScreenPanicked, never an escaped panic.
func TestBoundaryFallbackPanicIsContained(t *testing.T) {
	a := NewApp("boundary-twice")
	a.Register("/boom", boundaryPanicsTwice{}, nil)
	_, err := a.RenderPageResult(context.Background(), "/boom")
	if !errors.Is(err, ErrScreenPanicked) {
		t.Fatalf("RenderPageResult error %v is not ErrScreenPanicked", err)
	}
}
