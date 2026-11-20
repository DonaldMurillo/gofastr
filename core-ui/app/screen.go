package app

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
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

// ScreenTitler is an optional interface for components that declare their own page title.
type ScreenTitler interface {
	ScreenTitle(ctx context.Context) string
}

// ScreenDescriber is an optional interface for components that declare their own page description
// (used for route preloading metadata and SEO).
type ScreenDescriber interface {
	ScreenDescription(ctx context.Context) string
}

// RefuseRetiredScreenMethods panics on a component still declaring the no-argument ScreenTitle,
// ScreenDescription, ScreenSEO or ScreenSchema.
func RefuseRetiredScreenMethods(where string, comp any) {
	if comp == nil {
		return
	}
	t := reflect.TypeOf(comp)
	for _, name := range retiredScreenMethods {
		if m, ok := t.MethodByName(name); ok && m.Type.NumIn() == 1 {
			panic(fmt.Sprintf("app: %s: %T declares %s() with no context; "+
				"it is %s(ctx context.Context) now", where, comp, name, name))
		}
	}
}

var retiredScreenMethods = []string{"ScreenTitle", "ScreenDescription", "ScreenSEO", "ScreenSchema"}

// ScreenLanger is an optional interface for components that declare the document language of their
// own page as a BCP-47 tag ("es", "pt-BR").
type ScreenLanger interface {
	ScreenLang() string
}

// ScreenTyper is an optional interface for components that declare their screen type.
type ScreenTyper interface {
	ScreenType() ScreenType
}

// ScreenSpec is a convenience interface that groups ScreenTitler, ScreenDescriber, and ScreenTyper.
type ScreenSpec interface {
	ScreenTitler
	ScreenDescriber
	ScreenTyper
}

// ScreenComponentID is an optional extension of ScreenSpec that lets a screen declare its component
// ID for action compilation.
type ScreenComponentID interface {
	ScreenSpec
	// ComponentID returns the ID used for action registration and data-component.
	ComponentID() string
}

// ScreenActions is an optional interface for screens that declare server actions.
type ScreenActions interface {
	Actions()
}

// ScreenLoader is an optional interface for screens that need to fetch data before rendering.
type ScreenLoader interface {
	Load(ctx context.Context) error
}

// StaticPathsProvider lists the concrete params a screen on a dynamic pattern is exported for.
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
	Layout          *Layout
	policies        []Policy
	notFoundOutcome bool
	errorOutcome    bool

	// NoLLMMD disables auto-generated /path/llm.md for this screen.
	NoLLMMD bool
	// Article marks the screen's content as an article so browsers offer their built-in Reader Mode
	// (Safari Reader, Firefox Reader View) and render it well.
	Article bool

	// NoSPA excludes the screen from the client route manifest: links to it resolve as full document
	// loads instead of soft navigations.
	NoSPA bool

	// Intercept, when set, makes this screen present as an overlay for a soft navigation that started
	// on the declared origin route.
	Intercept *Intercept

	// Preload declares how the client may prefetch this route before it is navigated to: "" (never, the
	// default), PreloadHover, PreloadVisible, or PreloadEager.
	Preload string

	group *ScreenGroup

	resolvers []resolverDecl

	ownStyle *ownstyle.Sheet

	fills map[*Outlet]fillDecl

	// Loading declares what the SWAP SLOT shows while a navigation to this screen is in flight (,
	// rendered once into the route manifest the client already carries, shown in the swap slot only.
	Loading *Loading
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

// RoutePath returns the app route this screen is mounted at ([Screen.Path]).
func (s *Screen) RoutePath() string { return s.Path }

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

// WithLoading declares what the swap slot shows while a navigation to this screen is in flight (, .
func (s *Screen) WithLoading(ld *Loading) *Screen {
	s.Loading = ld
	return s
}

// WithPolicy appends a Policy to the screen's own chain.
func (s *Screen) WithPolicy(p Policy) *Screen {
	if p == nil {
		return s
	}
	s.policies = append(s.policies, p)
	return s
}

// PolicyChain returns a copy of the screen's own policy chain (does not include inherited group
// policies, use EffectivePolicies for the full walk).
func (s *Screen) PolicyChain() []Policy {
	if len(s.policies) == 0 {
		return nil
	}
	out := make([]Policy, len(s.policies))
	copy(out, s.policies)
	return out
}

// ParamSetter is implemented by components that accept route parameters before rendering.
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

func (s *Screen) newInstance() component.Component {
	if s.Component == nil {
		return nil
	}
	return newComponentInstance(s.Component)
}

// Render renders the screen's component with appropriate ARIA landmarks.
func (s *Screen) Render() render.HTML {
	return s.RenderCtx(context.Background())
}

// RenderCtx renders the screen's component with ARIA landmarks, passing ctx through to
// ContextComponent implementations.
func (s *Screen) RenderCtx(ctx context.Context) render.HTML {
	content, renderErr := component.SafeRenderCtx(ctx, s.Component)
	if renderErr != nil {
		slog.Default().Error("app: screen render panicked; rendering fallback",
			"panic", textsafe.Recovered(renderErr))
	}
	return wrapByScreenType(s.Type, s.Title, content)
}

func wrapByScreenType(t ScreenType, title string, content render.HTML) render.HTML {
	switch t {
	case ScreenPage:
		return html.Main(html.MainConfig{}, content)

	case ScreenDrawer:
		return render.Tag("div", map[string]string{
			"role":       "complementary",
			"aria-label": title,
		}, content)

	case ScreenSheet:
		return render.Tag("div", map[string]string{
			"role":       "complementary",
			"aria-label": title,
		}, content)

	case ScreenDialog:
		return render.Tag("div", map[string]string{
			"role":       "dialog",
			"aria-modal": "true",
			"aria-label": title,
		}, content)

	default:
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

// ValidateScreenOutput checks a screen's rendered output for common mistakes and returns a slice of
// warning strings.
func ValidateScreenOutput(screen *Screen, output string) []string {
	var warnings []string

	if screen.Type == ScreenPage {
		if strings.Contains(output, "<main") {
			warnings = append(warnings,
				fmt.Sprintf("screen %q: component output contains <main> but the framework already wraps ScreenPage in <main>. This creates nested <main> elements (invalid HTML). Return the content without the <main> wrapper.",
					screen.Path))
		}
	}

	return warnings
}
