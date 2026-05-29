package component

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Component is the base interface for all UI components.
// A component describes its appearance via Render.
type Component interface {
	Render() render.HTML
}

// ContextComponent is the optional ctx-aware render interface.
// Implementations receive the request context and can branch on
// auth state, locale, or any other request-scoped value that isn't
// loaded into struct fields by Load(ctx).
//
// The framework prefers RenderCtx over Render when both exist.
// ContextComponent intentionally does NOT embed Component — a type
// that only wants the ctx-aware shape can satisfy Component via the
// ContextOnly{} embed below, avoiding the
// `func (c) Render() HTML { return c.RenderCtx(context.Background()) }`
// boilerplate.
type ContextComponent interface {
	RenderCtx(ctx context.Context) render.HTML
}

// ContextOnly is a zero-byte embed helper for components that only
// want to implement RenderCtx. Embedding it provides a Render()
// satisfying the Component interface; that Render is never called
// when the type also implements RenderCtx (the framework dispatch
// prefers RenderCtx and skips Render in that case).
//
//	type Home struct {
//	    component.ContextOnly
//	    // ... your fields
//	}
//	func (h *Home) RenderCtx(ctx context.Context) render.HTML { ... }
//
// No need to write a Render() stub on Home — ContextOnly provides one.
type ContextOnly struct{}

// Render satisfies the Component interface. In practice it is never
// called: SafeRenderCtx and Screen.RenderCtx detect RenderCtx via a
// structural type check and route to it directly.
func (ContextOnly) Render() render.HTML { return "" }

// InteractiveComponent extends Component with event handling.
// Components that implement this interface can respond to user events.
type InteractiveComponent interface {
	Component
	Actions()
}

// ComponentBase provides common fields for all components.
// Embed this in your component structs for convenience.
type ComponentBase struct {
	ID    string
	Class string
}

// RenderComponent calls the Render method of any Component and returns its HTML.
// This is a convenience function for composing components.
func RenderComponent(c Component) render.HTML {
	return c.Render()
}

// IsInteractive returns true if the component implements InteractiveComponent.
func IsInteractive(c Component) bool {
	_, ok := c.(InteractiveComponent)
	return ok
}

// Widget is a self-contained interactive unit that serves as a hydration boundary.
// Widgets are hydrated incrementally on first user interaction.
// Components inside a Widget are compiled to JS for client-side execution.
type Widget struct {
	ID        string          // unique widget ID (used for data-widget attribute)
	Component Component       // the component that renders this widget
	Actions   *ActionRegistry // extracted actions from the component
}

// NewWidget creates a new widget wrapping a component.
// It automatically extracts actions if the component implements InteractiveComponent.
func NewWidget(id string, comp Component) *Widget {
	return &Widget{
		ID:        id,
		Component: comp,
		Actions:   ExtractActions(comp),
	}
}

// Render renders the widget's component wrapped in a data-widget attribute.
// Output: <div data-widget="{id}" data-hydrate="lazy">{component.Render()}</div>
func (w *Widget) Render() render.HTML {
	hydrateStrategy := "lazy"
	attrs := map[string]string{
		"data-widget":  w.ID,
		"data-hydrate": hydrateStrategy,
	}
	if w.Actions != nil && w.Actions.HasActions() {
		hydrateStrategy = "interaction"
		attrs["data-behavior"] = "/__gofastr/widget/" + w.ID + ".js"
	}
	attrs["data-hydrate"] = hydrateStrategy
	return render.Tag("div", attrs, w.Component.Render())
}

// IsInteractive returns true if the widget's component has actions.
func (w *Widget) IsInteractive() bool {
	return w.Actions != nil && w.Actions.HasActions()
}

// ErrorBoundary is implemented by components that provide a custom error fallback.
type ErrorBoundary interface {
	RenderError(err error) render.HTML
}

// SafeRender calls Render() with panic recovery. If the component panics,
// it returns a fallback error UI. Components implementing ErrorBoundary
// get a custom fallback; others get a generic red-bordered box.
func SafeRender(c Component) (html render.HTML, err error) {
	return SafeRenderCtx(context.Background(), c)
}

// SafeRenderCtx is the context-aware variant of SafeRender. If c
// implements ContextComponent its RenderCtx(ctx) is called; otherwise
// Render() is called. Panic recovery and ErrorBoundary handling are
// identical to SafeRender.
func SafeRenderCtx(ctx context.Context, c Component) (html render.HTML, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("component render panic: %v", r)
			if eb, ok := c.(ErrorBoundary); ok {
				html = eb.RenderError(err)
			} else {
				// Build the fallback through the auto-escaper so an
				// attacker-influenced panic message can't inject markup.
				html = render.Tag(
					"div",
					map[string]string{"class": "fui-render-error", "role": "alert"},
					render.Tag("strong", nil, render.Text("Error:")),
					render.Text(" "+err.Error()),
				)
			}
		}
	}()
	if cc, ok := c.(ContextComponent); ok {
		html = cc.RenderCtx(ctx)
	} else {
		html = c.Render()
	}
	return
}

// ComponentList renders multiple components joined together.
func ComponentList(components ...Component) render.HTML {
	var parts []render.HTML
	for _, c := range components {
		parts = append(parts, c.Render())
	}
	return render.Join(parts...)
}
