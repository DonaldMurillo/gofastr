package component

import (
	"context"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
)

// Component is anything that renders HTML.
type Component interface {
	Render() render.HTML
}

// ContextComponent renders with the request context; it is preferred over Render.
type ContextComponent interface {
	RenderCtx(ctx context.Context) render.HTML
}

// ContextOnly gives a RenderCtx-only type the Render that Component needs.
type ContextOnly struct{}

// Render satisfies Component; it is never called.
func (ContextOnly) Render() render.HTML { return "" }

// InteractiveComponent is a Component whose Actions registers event handlers.
type InteractiveComponent interface {
	Component
	Actions()
}

// ComponentBase holds the ID and Class most components carry.
type ComponentBase struct {
	ID    string
	Class string
}

// RenderComponent renders c with a background context.
func RenderComponent(c Component) render.HTML {
	return RenderComponentCtx(context.Background(), c)
}

// RenderComponentCtx renders c, through RenderCtx when it is a ContextComponent.
func RenderComponentCtx(ctx context.Context, c Component) render.HTML {
	if cc, ok := c.(ContextComponent); ok {
		return cc.RenderCtx(ctx)
	}
	return c.Render()
}

// IsInteractive returns true if the component implements InteractiveComponent.
func IsInteractive(c Component) bool {
	_, ok := c.(InteractiveComponent)
	return ok
}

// Widget is a hydration boundary around a component and its actions.
type Widget struct {
	ID        string
	Component Component
	Actions   *ActionRegistry
}

// NewWidget wraps comp with a slugged id and its extracted actions.
func NewWidget(id string, comp Component) *Widget {
	return &Widget{
		ID:        SlugifyWidgetID(id),
		Component: comp,
		Actions:   ExtractActions(comp),
	}
}

// SlugifyWidgetID maps every rune outside [A-Za-z0-9_-] to '-'.
func SlugifyWidgetID(id string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, id)
}

// Render renders the component in a data-widget div, with data-behavior when it has actions.
func (w *Widget) Render() render.HTML {
	return w.RenderCtx(context.Background())
}

// RenderCtx renders the widget and passes the request context to its component.
func (w *Widget) RenderCtx(ctx context.Context) render.HTML {
	attrs := map[string]string{"data-widget": w.ID}
	if w.IsInteractive() {
		attrs["data-behavior"] = "/__gofastr/widget/" + w.ID + ".js"
	}
	return render.Tag("div", attrs, RenderComponentCtx(ctx, w.Component))
}

// IsInteractive returns true if the widget's component has actions.
func (w *Widget) IsInteractive() bool {
	return w.Actions != nil && w.Actions.HasActions()
}

// ErrorBoundary is a component with its own render-panic fallback.
type ErrorBoundary interface {
	RenderError(err error) render.HTML
}

// SafeRender renders c, turning a panic into a fallback and an error.
func SafeRender(c Component) (html render.HTML, err error) {
	return SafeRenderCtx(context.Background(), c)
}

// SafeRenderCtx is SafeRender with the request context.
func SafeRenderCtx(ctx context.Context, c Component) (html render.HTML, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("component render panic: %v", r)
			renderdiag.Report(ctx, "component", c, r)
			if eb, ok := c.(ErrorBoundary); ok {
				html = eb.RenderError(err)
			} else {
				html = render.Tag(
					"div",
					map[string]string{"class": "cui-render-error", "role": "alert"},
					render.Tag("strong", nil, render.Text("Error:")),
					render.Text(" "+textsafe.Recovered(r)),
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
		parts = append(parts, RenderComponent(c))
	}
	return render.Join(parts...)
}
