package app

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
)

// ScreenType classifies the kind of screen.
type ScreenType int

const (
	// ScreenPage is a full-page view.
	ScreenPage ScreenType = iota
	// ScreenDrawer is a side panel.
	ScreenDrawer
	// ScreenSheet is a bottom sheet.
	ScreenSheet
	// ScreenDialog is a modal dialog.
	ScreenDialog
)

// ScreenSpec is an optional interface that components can implement to
// self-declare their screen metadata. When a component implements ScreenSpec,
// the app reads title, description, and screen type directly from it — no
// need for WithTitle/WithDescription/WithScreenType builder chaining.
//
// Implement only the methods you need; the builder methods still work as overrides.
type ScreenSpec interface {
	// ScreenTitle returns the page title for <title> and route metadata.
	ScreenTitle() string
	// ScreenDescription returns a short description for route preloading.
	ScreenDescription() string
	// ScreenType returns the type of screen (page, drawer, sheet, dialog).
	// Return ScreenPage for normal full-page views.
	ScreenType() ScreenType
}

// ScreenComponentID is an optional extension of ScreenSpec that lets a screen
// declare its component ID for action compilation. If not implemented, the
// ID is derived from the route path. The component ID must match the
// data-component attribute in the rendered HTML.
type ScreenComponentID interface {
	ScreenSpec
	// ComponentID returns the ID used for action registration and data-component.
	ComponentID() string
}

// ScreenActions is an optional interface for screens that declare server actions.
// If implemented, the DevServer auto-compiles actions when the screen is registered.
// The Actions method uses component.On() to register handlers, same as InteractiveComponent.
type ScreenActions interface {
	Actions()
}

// Screen represents a top-level view in the app hierarchy.
type Screen struct {
	// Path is the route pattern, e.g., "/users/:id".
	Path string
	// Name is a human-readable name for the screen.
	Name string
	// Title is the page title for <title> and route graph.
	Title string
	// Description is a short description for route preloading metadata.
	Description string
	// Type classifies the screen as page, drawer, sheet, or dialog.
	Type ScreenType
	// Component is the component that renders this screen.
	Component component.Component
	// Layout is an optional layout override for this screen.
	Layout *Layout

	// routeParams holds extracted dynamic route parameters.
	routeParams map[string]string
}

// NewScreen creates a page screen.
func NewScreen(path string, comp component.Component) *Screen {
	return &Screen{
		Path:      path,
		Name:      path,
		Type:      ScreenPage,
		Component: comp,
	}
}

// WithTitle sets the screen's page title.
func (s *Screen) WithTitle(title string) *Screen {
	s.Title = title
	return s
}

// WithDescription sets the screen's description.
func (s *Screen) WithDescription(desc string) *Screen {
	s.Description = desc
	return s
}

// RouteParams returns the extracted dynamic route parameters for this screen.
// Returns nil if the screen was matched by an exact path.
func (s *Screen) RouteParams() map[string]string {
	return s.routeParams
}

// ParamSetter is implemented by components that accept route parameters
// before rendering. The app calls SetParams after resolving a dynamic route.
type ParamSetter interface {
	SetParams(params map[string]string)
}

// NewDrawer creates a drawer screen.
func NewDrawer(path string, comp component.Component) *Screen {
	return &Screen{
		Path:      path,
		Name:      path,
		Type:      ScreenDrawer,
		Component: comp,
	}
}

// NewSheet creates a sheet screen.
func NewSheet(path string, comp component.Component) *Screen {
	return &Screen{
		Path:      path,
		Name:      path,
		Type:      ScreenSheet,
		Component: comp,
	}
}

// NewDialog creates a dialog screen.
func NewDialog(path string, comp component.Component) *Screen {
	return &Screen{
		Path:      path,
		Name:      path,
		Type:      ScreenDialog,
		Component: comp,
	}
}

// Render renders the screen's component with appropriate ARIA landmarks.
func (s *Screen) Render() render.HTML {
	content := s.Component.Render()

	switch s.Type {
	case ScreenPage:
		return elements.Main(elements.MainConfig{}, content)

	case ScreenDrawer:
		// Runtime handles structural wrapping (backdrop + nav.drawer).
		// Server returns just the inner content.
		return content

	case ScreenSheet:
		// Runtime handles structural wrapping (backdrop + div.sheet).
		// Server returns just the inner content.
		return content

	case ScreenDialog:
		// Runtime handles structural wrapping (backdrop + div.dialog).
		// Server returns just the inner content.
		return content

	default:
		// Fallback: treat as page.
		return elements.Main(elements.MainConfig{}, content)
	}
}

// String returns a human-readable description of the screen type.
func (t ScreenType) String() string {
	switch t {
	case ScreenPage:
		return "page"
	case ScreenDrawer:
		return "drawer"
	case ScreenSheet:
		return "sheet"
	case ScreenDialog:
		return "dialog"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}
