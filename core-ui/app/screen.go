package app

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
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

// ScreenTitler is an optional interface for components that declare their
// own page title. The returned string is the page-specific portion — the
// framework appends the app name from AppConfig.Name to produce the full
// <title>. For example, ScreenTitle() returning "Dashboard" with
// AppConfig.Name "MyApp" produces <title>Dashboard — MyApp</title>.
type ScreenTitler interface {
	ScreenTitle() string
}

// ScreenDescriber is an optional interface for components that declare their
// own page description (used for route preloading metadata and SEO).
type ScreenDescriber interface {
	ScreenDescription() string
}

// ScreenTyper is an optional interface for components that declare their
// screen type. If not implemented, the screen defaults to ScreenPage.
// Most screens can omit this — only implement it for drawers, sheets, or dialogs.
type ScreenTyper interface {
	ScreenType() ScreenType
}

// ScreenSpec is a convenience interface that groups ScreenTitler,
// ScreenDescriber, and ScreenTyper. Components can implement the full
// interface, or implement just the individual interfaces they need.
// For example, a component that only needs ScreenTitle can implement
// ScreenTitler alone — ScreenType defaults to ScreenPage and description
// defaults to empty.
//
// The builder methods (WithTitle, WithDescription, WithScreenType) still
// work as overrides when using RegisterScreen instead of Register.
type ScreenSpec interface {
	ScreenTitler
	ScreenDescriber
	ScreenTyper
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
// If implemented, the host auto-compiles actions when the screen is registered.
// The Actions method uses component.On() to register handlers, same as InteractiveComponent.
type ScreenActions interface {
	Actions()
}

// ScreenLoader is an optional interface for screens that need to fetch data
// before rendering. Load runs AFTER route params have been injected and AFTER
// DI services are wired, but BEFORE Render. Mutating fields on the screen is
// the expected pattern — those fields are then read by Render.
//
// The same Load runs for both SSR (per-request) and SSG (at build time), so
// implementations should be deterministic given the same context inputs.
// Network calls are fine; non-deterministic state (random IDs, time-of-day
// branches) will produce different output between SSG runs.
type ScreenLoader interface {
	Load(ctx context.Context) error
}

// StaticPathsProvider is an optional interface for screens that mount on a
// dynamic route pattern (e.g. "/products/:slug") and want to participate in
// static-site generation. The returned slice is one entry per concrete URL
// the SSG build should produce; each entry maps each ":param" in the route
// pattern to the value used for that build.
//
//	func (s *ProductDetailScreen) StaticPaths(ctx context.Context) []map[string]string {
//	    return []map[string]string{
//	        {"slug": "device"},
//	        {"slug": "gadget"},
//	    }
//	}
//
// Routes whose screen does not implement StaticPathsProvider are skipped at
// build time (they remain reachable via SSR if the server is running).
type StaticPathsProvider interface {
	StaticPaths(ctx context.Context) []map[string]string
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

	// NoLLMMD disables auto-generated /path/llm.md for this screen.
	NoLLMMD bool

	// routeParams holds extracted dynamic route parameters.
	routeParams map[string]string

	// mu protects SetParams + render for concurrent access on dynamic routes.
	mu sync.Mutex
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
		return html.Main(html.MainConfig{}, content)

	case ScreenDrawer:
		return render.Tag("div", map[string]string{
			"role":       "complementary",
			"aria-label": s.Title,
		}, content)

	case ScreenSheet:
		return render.Tag("div", map[string]string{
			"role":       "complementary",
			"aria-label": s.Title,
		}, content)

	case ScreenDialog:
		return render.Tag("div", map[string]string{
			"role":       "dialog",
			"aria-modal": "true",
			"aria-label": s.Title,
		}, content)

	default:
		// Fallback: treat as page.
		return html.Main(html.MainConfig{}, content)
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

// ValidateScreenOutput checks a screen's rendered output for common mistakes
// and returns a slice of warning strings. An empty slice means no issues.
// This is intended for dev-mode linting — it does not affect production
// rendering.
//
// Currently checks:
//   - Nested <main> elements: ScreenPage screens are already wrapped in <main>
//     by the framework, so including <main> in the component output creates
//     nested <main> elements (invalid HTML).
func ValidateScreenOutput(screen *Screen, output string) []string {
	var warnings []string

	// Only check <main> nesting for page-type screens. Drawers, sheets,
	// and dialogs don't get the <main> wrapper, so <main> in their output
	// is fine.
	if screen.Type == ScreenPage {
		if strings.Contains(output, "<main") {
			warnings = append(warnings,
				fmt.Sprintf("screen %q: component output contains <main> but the framework already wraps ScreenPage in <main> — this creates nested <main> elements (invalid HTML). Return the content without the <main> wrapper.",
					screen.Path))
		}
	}

	return warnings
}
