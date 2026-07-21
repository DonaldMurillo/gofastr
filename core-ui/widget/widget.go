package widget

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// RouteMatcher decides whether a widget is available on a given
// request path. A Definition with one or more matchers is filtered
// out of catalogs + SSR-inlining for paths that no matcher accepts.
// A Definition with NO matchers is available on every path (the
// historical default).
type RouteMatcher func(path string) bool

// Position is where the widget's root anchors itself when mounted.
// Modal/Center implies a backdrop and focus trap; Corner positions
// float without a backdrop.
type Position string

const (
	BottomRight  Position = "bottom-right"
	BottomCenter Position = "bottom-center" // toast / banner stack mid-edge
	BottomLeft   Position = "bottom-left"
	TopRight     Position = "top-right"
	TopCenter    Position = "top-center" // toast / banner stack mid-edge
	TopLeft      Position = "top-left"
	Center       Position = "center"    // modal — backdrop + focus trap
	Top          Position = "top"       // banner across the top
	Bottom       Position = "bottom"    // banner across the bottom
	Edge         Position = "edge-left" // drawer-style edge mount
	EdgeRight    Position = "edge-right"
)

// BootstrapMode selects how the widget injects itself onto a page.
//   - AutoScript: framework emits a <script> tag and the runtime mounts
//     it on every page that includes the tag. Used by kiln's panel.
//   - Embedded:   the widget is rendered as part of an existing page
//     tree (no script tag). Useful when the page already runs core-ui.
type BootstrapMode string

const (
	AutoScript BootstrapMode = "auto-script"
	Embedded   BootstrapMode = "embedded"
)

// SignalSource produces JSON-serializable values that flow to the
// browser as named signals. The runtime polls (or receives via SSE)
// and pushes new values into [data-fui-signal="<name>"] html.
type SignalSource interface {
	Read() (any, error)
}

// SignalFunc is a func adapter for SignalSource.
type SignalFunc func() (any, error)

func (f SignalFunc) Read() (any, error) { return f() }

// RPCEndpoint is a server-side HTTP handler the widget can invoke
// from the client (typically via a button click or form submit). The
// runtime POSTs to Path; on success it can push the response body
// into a signal named ResponseSignal (optional).
type RPCEndpoint struct {
	Method         string // "POST" by default
	Path           string
	Handler        http.Handler
	ResponseSignal string
}

// Slot is a host-supplied content region in the widget chrome. The
// runtime renders the widget skeleton and embeds the slot's component
// at the matching named placeholder.
type Slot struct {
	Name      string
	Component component.Component
}

// Definition is the full description of a widget. It is built via
// the New(name) builder and consumed by Mount(app, def).
type Definition struct {
	Name      string
	Position  Position
	Bootstrap BootstrapMode
	Slots     []Slot
	Signals   map[string]SignalSource
	RPCs      []RPCEndpoint

	// Skeleton is the host's chrome wrapper. If nil, the framework
	// uses a sensible default for the chosen Position (FloatingPanel
	// for corners, Modal for Center, etc.). Most hosts leave this
	// nil and use a preset.
	Skeleton func(slots map[string]render.HTML) render.HTML

	// ExtraCSS is host-supplied CSS appended after the framework's
	// chrome rules in the per-widget stylesheet (/<StylePath>). Use
	// for content styling (slot innards, host-specific class names)
	// that doesn't fit in the page theme. Generate it through
	// core-ui/style.NewStyleSheet for token consistency, or pass an
	// already-resolved CSS string.
	ExtraCSS func() string

	// Modal flags
	Backdrop            bool // dim the page behind the widget
	CloseOnEscape       bool // ESC closes the widget
	CloseOnClickOutside bool

	// Role is the ARIA role applied to the widget root element.
	// Defaults to "dialog" for backdrop'd widgets and is left empty
	// for plain panels / floating surfaces. Use "alertdialog" for
	// widgets that demand the user's immediate attention.
	Role string

	// LabelledBy is the id of an element (typically a heading) inside
	// the slot HTML that names this widget for screen readers. Becomes
	// aria-labelledby on the widget root. The host is responsible for
	// putting a matching id on the element.
	LabelledBy string

	// DescribedBy is the id of an element inside the slot HTML that
	// provides supplementary description for the widget. Becomes
	// aria-describedby on the widget root.
	DescribedBy string

	// Hidden=true means the widget is registered but NOT auto-mounted
	// on page load. A button with data-fui-open="<name>" calls
	// __gofastr.openWidget(name) to mount it on demand. Use for
	// modals + drawers that should appear in response to user action.
	Hidden bool

	// DeepLinkKey is the URL query parameter that controls open state
	// for this widget — e.g. "modal". When the request URL contains
	// `?<DeepLinkKey>=<DeepLinkValue>`, the SSR layer renders the
	// widget open at first paint AND the runtime mirrors open/close to
	// pushState so refresh/share/back-button all stay consistent.
	//
	// Empty (the default) disables deep-linking — the widget remains
	// purely click-driven via data-fui-open.
	//
	// Only meaningful for Hidden widgets (modal / drawer). Toasts and
	// dropdowns intentionally do NOT support deep links.
	DeepLinkKey string
	// DeepLinkValue is the literal value of DeepLinkKey that opens
	// THIS widget. Multiple widgets can share the same DeepLinkKey
	// ("modal") as long as their DeepLinkValue is distinct
	// ("user-edit", "confirm-delete").
	DeepLinkValue string
	// DeepLinkParams lists additional query parameters whose values
	// should be mirrored into named signals when the widget opens via
	// deep link. e.g. ["user_id"] with URL `?modal=user-edit&user_id=42`
	// seeds signal "user_id"="42" before the slot renders.
	DeepLinkParams []string

	// Routes scopes the widget to specific request paths. When non-
	// empty, the SSR layer and the runtime catalog only expose this
	// widget on pages whose path is accepted by at least one matcher.
	// Empty (the default) means "available on every page" — the
	// behaviour before per-page scoping shipped.
	//
	// Constructed via the Builder methods .Pages, .PagesPrefix,
	// .PagesMatch (or manually for advanced cases).
	Routes []RouteMatcher

	// DragDismiss enables pointer-driven drag-to-dismiss for bottom-edge
	// widgets. The chrome renders a visible drag-handle bar at the top
	// of the panel; the runtime listens for pointerdown/move/up on the
	// handle (and on the panel itself) and closes the widget when the
	// user drags past a distance + velocity threshold. Snaps back to
	// the resting position when released earlier.
	//
	// Only meaningful for Bottom (and bottom-edge) positions today;
	// silently no-op elsewhere.
	DragDismiss bool

	// Asset path overrides. Default routes are derived from Name.
	BootstrapPath string // default: /core-ui/widget/<name>/bootstrap.js
	StylePath     string // default: /core-ui/widget/<name>/style.css
	StatePath     string // default: /core-ui/widget/<name>/state

	// PollMS is the freshness interval (milliseconds) the runtime
	// should re-fetch StatePath on. Zero (the default) disables
	// polling. Emitted as "pollMs" in the catalog ONLY when both
	// PollMS > 0 AND the widget declares at least one Signal — a
	// widget without signals has no StatePath to poll. The runtime
	// applies ±10% jitter, pauses while document.hidden, fetches
	// immediately on visibility regain, and backs off (doubling,
	// capped at 5×) on fetch failure until the next success.
	// Set via Builder.Poll.
	PollMS int
}

// registry is the process-global list of mounted widgets. The framework
// runtime (window.__gofastr) fetches /__gofastr/widgets at startup and
// mounts every entry. One scripted runtime URL on the page; arbitrarily
// many widgets register through the registry.
//
// Coexistence with core-ui/registry: this registry is widget-specific
// (Position, Slots, RPCs, Skeleton, polling, etc.). The newer
// core-ui/registry handles per-component CSS for plain styled
// components and is fetched by the runtime as
// window.__gofastr_catalog. Both share the data-fui-style="<name>"
// link dedup key on the client, so a widget and a registered component
// can't collide on names.
//
// TODO(follow-up): consider folding Definition into
// core-ui/registry.Entry as an opaque Widget *any field once the
// import graph allows it without cycles. Today widget imports
// signal, island, component, html, render, style; registry imports
// only style + component. Inverting that requires a small Widget
// interface in registry, with widget defining the concrete type.
var (
	registryMu sync.Mutex
	registry   = map[string]*Definition{}
)

// allWidgets returns the registered widgets (snapshot copy), sorted by
// name. Sorting matters: every consumer emits bytes derived from the
// walk — the /__gofastr/widgets catalog JSON, SSR-inlined chrome order,
// and the static export's widget dump (whose tree hash versions the PWA
// cache) — and Go's map iteration would make all of them flap once a
// second widget registers.
func allWidgets() []*Definition {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := make([]*Definition, 0, len(registry))
	for _, d := range registry {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// AllForSSR returns a snapshot of every registered widget. Exported
// so SSR hosts (framework/uihost) can walk the registry and inline
// chrome HTML on the page response. The returned slice is a copy of
// the live registry; callers may iterate freely.
func AllForSSR() []*Definition { return allWidgets() }

// IsAvailableOn returns true when the widget is registered for the
// given request path. Widgets with no Routes (the default) are
// available everywhere; widgets with one or more matchers are
// available on paths accepted by at least one matcher.
func (d *Definition) IsAvailableOn(path string) bool {
	if len(d.Routes) == 0 {
		return true
	}
	for _, m := range d.Routes {
		if m != nil && m(path) {
			return true
		}
	}
	return false
}

// AvailableOn returns the subset of registered widgets visible on
// the given request path. The SSR host + per-request catalog handler
// use this to keep page-scoped widgets out of unrelated pages.
func AvailableOn(path string) []*Definition {
	all := allWidgets()
	out := make([]*Definition, 0, len(all))
	for _, d := range all {
		if d.IsAvailableOn(path) {
			out = append(out, d)
		}
	}
	return out
}

// RenderChrome returns the rendered chrome HTML for a single widget,
// using its registered Skeleton or the framework's defaultSkeleton.
// Exported so SSR hosts can inline chrome without instantiating
// `server` themselves.
func RenderChrome(d *Definition) string {
	return string((&server{def: *d}).renderSkeleton())
}

// RenderCSS returns the widget's stylesheet: the framework positioning +
// chrome rules plus the widget's optional ExtraCSS. Exported so the static
// exporter can dump each widget's CSS as a query-free file alongside its
// chrome HTML. Mirrors what serveStyle writes to the live style endpoint.
func RenderCSS(d *Definition) string {
	css := widgetCSS(*d)
	if d.ExtraCSS != nil {
		css += "\n" + d.ExtraCSS()
	}
	return css
}

// New starts a builder for a widget Definition with sensible defaults.
func New(name string) *Builder {
	return &Builder{
		def: Definition{
			Name:      name,
			Position:  BottomRight,
			Bootstrap: AutoScript,
			Signals:   map[string]SignalSource{},
		},
	}
}

// Builder fluently composes a Definition. Use widget.New(name) to start.
type Builder struct {
	def Definition
}

func (b *Builder) Mount(p Position) *Builder {
	b.def.Position = p
	if p == Center {
		b.def.Backdrop = true
		b.def.CloseOnEscape = true
		b.def.CloseOnClickOutside = true
		if b.def.Role == "" {
			b.def.Role = "dialog"
		}
	}
	return b
}

func (b *Builder) Bootstrap(m BootstrapMode) *Builder {
	b.def.Bootstrap = m
	return b
}

func (b *Builder) Slot(name string, c component.Component) *Builder {
	b.def.Slots = append(b.def.Slots, Slot{Name: name, Component: c})
	return b
}

// Signal registers a named server-side signal source. The runtime
// pushes new values to client DOM nodes bound to it.
func (b *Builder) Signal(name string, src SignalSource) *Builder {
	if b.def.Signals == nil {
		b.def.Signals = map[string]SignalSource{}
	}
	b.def.Signals[name] = src
	return b
}

// Poll sets a re-fetch interval for the widget's signal state. The
// runtime GETs StatePath on this cadence and overwrites each declared
// signal with the fresh value (skipping the DOM write when the value
// is unchanged). ±10% jitter, pause-while-hidden, and exponential
// back-off on fetch failure are applied automatically by the runtime.
//
// No-op in catalog emission when the widget declares no Signals
// (statePath is omitted, so there is nothing to poll). interval is
// recorded verbatim as milliseconds; the browser runtime enforces a
// 100ms floor (Go callers are trusted config — the page-attribute
// path clamps at 5s instead, because markup is cheap to typo).
func (b *Builder) Poll(interval time.Duration) *Builder {
	b.def.PollMS = int(interval / time.Millisecond)
	return b
}

// RPC registers a server-side handler the widget can invoke from
// the client. method=="" defaults to "POST".
func (b *Builder) RPC(method, path string, h http.Handler) *Builder {
	if method == "" {
		method = "POST"
	}
	b.def.RPCs = append(b.def.RPCs, RPCEndpoint{Method: method, Path: path, Handler: h})
	return b
}

// RPCWithSignal is RPC + ResponseSignal: on success the handler's
// JSON response body is pushed into the named signal. Useful for
// "fetch and render" flows where a button click updates a region.
func (b *Builder) RPCWithSignal(method, path string, h http.Handler, signal string) *Builder {
	if method == "" {
		method = "POST"
	}
	b.def.RPCs = append(b.def.RPCs, RPCEndpoint{
		Method: method, Path: path, Handler: h, ResponseSignal: signal,
	})
	return b
}

// Backdrop forces a backdrop regardless of position.
func (b *Builder) Backdrop() *Builder { b.def.Backdrop = true; return b }

// DragDismiss enables drag-to-dismiss for bottom-edge widgets. See
// Definition.DragDismiss for the full contract.
func (b *Builder) DragDismiss() *Builder { b.def.DragDismiss = true; return b }

// Role sets the ARIA role on the widget root (e.g. "dialog",
// "alertdialog", "menu"). Pair with LabelledBy / DescribedBy for a
// complete a11y label.
func (b *Builder) Role(r string) *Builder { b.def.Role = r; return b }

// LabelledBy sets aria-labelledby on the widget root. The id MUST
// match an element in the rendered slot HTML.
func (b *Builder) LabelledBy(id string) *Builder { b.def.LabelledBy = id; return b }

// DescribedBy sets aria-describedby on the widget root. The id MUST
// match an element in the rendered slot HTML.
func (b *Builder) DescribedBy(id string) *Builder { b.def.DescribedBy = id; return b }

// Hidden marks the widget as registered-but-not-auto-mounted. Open
// it from a button with data-fui-open="<name>".
func (b *Builder) Hidden() *Builder { b.def.Hidden = true; return b }

// Pages scopes the widget to exact path matches. The widget is
// hidden from the catalog + SSR-inlining on every page whose path
// isn't in the list. Stack with PagesPrefix / PagesMatch to combine
// rules.
func (b *Builder) Pages(paths ...string) *Builder {
	for _, p := range paths {
		target := p
		b.def.Routes = append(b.def.Routes, func(path string) bool { return path == target })
	}
	return b
}

// PagesPrefix scopes the widget to paths that start with any of the
// given prefixes. Useful for section-wide modals (e.g. every
// /customers/* page).
func (b *Builder) PagesPrefix(prefixes ...string) *Builder {
	for _, p := range prefixes {
		target := p
		b.def.Routes = append(b.def.Routes, func(path string) bool {
			return strings.HasPrefix(path, target)
		})
	}
	return b
}

// PagesMatch scopes the widget to paths accepted by the supplied
// matcher. Use for non-trivial rules (e.g. regex, glob, denylist).
func (b *Builder) PagesMatch(fn func(path string) bool) *Builder {
	b.def.Routes = append(b.def.Routes, fn)
	return b
}

// DeepLink wires this widget to a query-string pair. When the URL
// includes `?key=value`, the SSR layer opens the widget at first paint
// and the runtime mirrors open/close as pushState updates — so refresh,
// share, and the browser back button all work.
//
// Pair with DeepLinkParam to pass extra data into the widget's signals.
//
// Intended for modal/drawer presets; not for toasts or dropdowns.
func (b *Builder) DeepLink(key, value string) *Builder {
	b.def.DeepLinkKey = key
	b.def.DeepLinkValue = value
	return b
}

// DeepLinkParam registers a query-string key whose value is mirrored
// into a same-named signal whenever this widget opens via deep link.
// Call once per param. Example:
//
//	preset.Modal("user-edit").
//	    Hidden().
//	    DeepLink("modal", "user-edit").
//	    DeepLinkParam("user_id").
//	    Signal("user_id", widget.SignalFunc(func() (any, error) { ... })).
//
// Visiting `/users?modal=user-edit&user_id=42` opens the modal with
// signal "user_id" pre-seeded to "42".
func (b *Builder) DeepLinkParam(key string) *Builder {
	b.def.DeepLinkParams = append(b.def.DeepLinkParams, key)
	return b
}

// Skeleton overrides the default chrome wrapper.
func (b *Builder) Skeleton(fn func(slots map[string]render.HTML) render.HTML) *Builder {
	b.def.Skeleton = fn
	return b
}

// Definition returns a pointer to the in-progress Definition so
// preset builders (drawer / modal / toast) can tweak fields the
// fluent API doesn't expose setters for. Callers building widgets by
// hand should still finish with .Build().
func (b *Builder) Definition() *Definition { return &b.def }

// Build returns the assembled Definition.
func (b *Builder) Build() Definition { return b.def }

// --- Mount ------------------------------------------------------------

// Mount registers the widget's HTTP routes on r and adds it to the
// process-global registry the framework runtime auto-discovers. Hosts
// don't embed a per-widget script tag — they embed ONE shared runtime
// tag (see RuntimeTag) and the runtime mounts every registered widget.
//
// Routes mounted on r:
//
//	GET  <StylePath>         widget styles (theme-resolved CSS)
//	GET  <StatePath>         JSON snapshot of all signals (initial render)
//	GET  /core-ui/widget/<name>/chrome   rendered chrome HTML (lazy)
//	*    <RPC.Path>          for each RPC, the host's handler
//
// Default paths are filled in on def if unset so the caller can read
// them after Mount returns. Mount is idempotent on def.Name.
func Mount(r *router.Router, def *Definition) {
	if def.StylePath == "" {
		def.StylePath = "/core-ui/widget/" + def.Name + "/style.css"
	}
	if def.StatePath == "" {
		def.StatePath = "/core-ui/widget/" + def.Name + "/state"
	}

	srv := &server{def: *def}
	r.Get(def.StylePath, http.HandlerFunc(srv.serveStyle))
	r.Get(def.StatePath, http.HandlerFunc(srv.serveState))
	// Chrome endpoint — runtime fetches HTML lazily on first open
	// instead of receiving it inline in the /widgets catalog. Keeps
	// the registry small and lets browsers cache by URL.
	r.Get(chromePathFor(def), http.HandlerFunc(srv.serveChrome))

	for _, rpc := range def.RPCs {
		method := rpc.Method
		switch method {
		case "GET":
			r.Get(rpc.Path, rpc.Handler)
		case "POST":
			r.Post(rpc.Path, rpc.Handler)
		case "PUT":
			r.Put(rpc.Path, rpc.Handler)
		case "DELETE":
			r.Delete(rpc.Path, rpc.Handler)
		default:
			r.Post(rpc.Path, rpc.Handler)
		}
	}

	registryMu.Lock()
	registry[def.Name] = def
	registryMu.Unlock()
}

// MountBuilder builds the widget from b and mounts it on r — sugar over the
// two-step
//
//	def := b.Build()
//	widget.Mount(r, &def)
//
// which every caller otherwise repeats. Use Builder.Hidden() in the chain for
// widgets (modals, drawers) that start closed.
func MountBuilder(r *router.Router, b *Builder) {
	def := b.Build()
	Mount(r, &def)
}

// RuntimeTag returns the markup a host page embeds to load the framework
// runtime + auto-mount every registered widget. The URL includes a
// content-hash query param so a new server build invalidates any cached
// runtime in the browser without manual hard-reload.
//
// Also emits an inert <script type="application/json"
// id="gofastr-runtime-modules"> manifest mapping each split runtime module
// (popover, toasts, widgets, …) to its content-addressed hash. The
// client-side module loader reads this manifest to build cache-busted
// `?v=<hash>` URLs. Without it, kiln-style hosts that don't go through
// framework/uihost would fall through to un-versioned URLs and end up
// poisoning the browser cache (server returns `Cache-Control: ...immutable`
// unconditionally).
//
// Implemented as a func, not a const, because the hashes are computed
// lazily from the embedded JS bytes.
func RuntimeTag() string {
	return `<script src="/__gofastr/runtime.js?v=` + runtimeHash() + `"></script>` +
		RuntimeModuleManifestScript()
}

// MountRuntime registers the framework runtime endpoints on r:
//
//	GET /__gofastr/runtime.js                 the default bundled runtime
//	                                          (single-payload IIFE every
//	                                          page ships)
//	GET /__gofastr/runtime/<name>.js          one split runtime module
//	                                          (loaded on demand via the
//	                                          optional module-loader path)
//	GET /__gofastr/widgets                    JSON list of registered widgets
//
// Call this once per host (kiln serve, examples/site, etc.).
// The runtime IIFE is idempotent, so re-mounting on rebuilds is safe.
func MountRuntime(r *router.Router) {
	r.Get("/__gofastr/runtime.js", http.HandlerFunc(serveRuntime))
	r.Get("/__gofastr/widgets", http.HandlerFunc(serveWidgetList))
	// Per-module URL pattern. Router supports parameter capture via the
	// trailing wildcard; the handler validates the name + extracts it
	// from the path itself so we accept .js suffix uniformly.
	r.Get("/__gofastr/runtime/{name}", http.HandlerFunc(serveRuntimeModule))
}
