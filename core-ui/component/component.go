package component

import "github.com/gofastr/gofastr/core/render"

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
	if w.Actions != nil && w.Actions.HasActions() {
		hydrateStrategy = "interaction"
	}
	return render.Tag("div", map[string]string{
		"data-widget":  w.ID,
		"data-hydrate": hydrateStrategy,
	}, w.Component.Render())
}

// IsInteractive returns true if the widget's component has actions.
func (w *Widget) IsInteractive() bool {
	return w.Actions != nil && w.Actions.HasActions()
}

// ComponentList renders multiple components joined together.
func ComponentList(components ...Component) render.HTML {
	var parts []render.HTML
	for _, c := range components {
		parts = append(parts, c.Render())
	}
	return render.Join(parts...)
}
