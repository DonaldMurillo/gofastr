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

// ComponentList renders multiple components joined together.
func ComponentList(components ...Component) render.HTML {
	var parts []render.HTML
	for _, c := range components {
		parts = append(parts, c.Render())
	}
	return render.Join(parts...)
}
