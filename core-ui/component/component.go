package component

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Component is the base interface for all UI components.
// A component describes its appearance via Render.
type Component interface {
	Render() render.HTML
}

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
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("component render panic: %v", r)
			if eb, ok := c.(ErrorBoundary); ok {
				html = eb.RenderError(err)
			} else {
				html = render.HTML(fmt.Sprintf(
					`<div style="border:2px solid #EF4444;padding:16px;border-radius:8px;background:#FEF2F2;color:#991B1B;"><strong>Error:</strong> %s</div>`,
					err.Error(),
				))
			}
		}
	}()
	html = c.Render()
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
