package widget

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// RouteMatcher decides whether a widget is available on a given request path.
type RouteMatcher func(path string) bool

// Position is where the widget's root anchors itself when mounted.
type Position string

const (
	BottomRight  Position = "bottom-right"
	BottomCenter Position = "bottom-center"
	BottomLeft   Position = "bottom-left"
	TopRight     Position = "top-right"
	TopCenter    Position = "top-center"
	TopLeft      Position = "top-left"
	Center       Position = "center"
	Top          Position = "top"
	Bottom       Position = "bottom"
	Edge         Position = "edge-left"
	EdgeRight    Position = "edge-right"
)

// SignalSource produces a JSON-serialisable value the browser reads as a signal.
type SignalSource interface {
	Read() (any, error)
}

// SignalFunc is a func adapter for SignalSource.
type SignalFunc func() (any, error)

func (f SignalFunc) Read() (any, error) { return f() }

// RPCEndpoint is a handler the widget's controls call.
type RPCEndpoint struct {
	Method  string
	Path    string
	Handler http.Handler
}

// Slot is a host-supplied content region in the widget chrome.
type Slot struct {
	Name      string
	Component component.Component
}

// Definition is the full description of a widget.
type Definition struct {
	Name     string
	Position Position
	Slots    []Slot

	// Signals are served at StatePath, the same value to every caller: never per-user data.
	Signals map[string]SignalSource
	RPCs    []RPCEndpoint

	// RequireSession gates /state, /chrome and RPCs behind a browser session, which is not a sign-in.
	RequireSession bool

	// RequireAuthenticated gates /state, /chrome and RPCs behind a signed-in principal.
	RequireAuthenticated bool

	// Skeleton is the host's chrome wrapper.
	Skeleton func(slots map[string]render.HTML) render.HTML

	// ExtraCSS is CSS appended to the widget's stylesheet.
	ExtraCSS func() string

	// Modal flags
	Backdrop            bool
	CloseOnEscape       bool
	CloseOnClickOutside bool

	// Surface paints the root as a panel at a corner or banner position.
	Surface bool

	// Role is the ARIA role applied to the widget root element.
	Role string

	// LabelledBy becomes aria-labelledby on the widget root.
	LabelledBy string

	// DescribedBy becomes aria-describedby on the widget root.
	DescribedBy string

	// Hidden registers the widget without mounting it until data-cui-open opens it.
	Hidden bool

	// DeepLinkKey is the query parameter whose DeepLinkValue opens the widget at first paint.
	DeepLinkKey string
	// DeepLinkValue is the literal value of DeepLinkKey that opens THIS widget.
	DeepLinkValue string
	// DeepLinkParams are mirrored into same-named signals on a deep-link open.
	DeepLinkParams []string

	// Routes scopes the widget to specific request paths.
	Routes []RouteMatcher

	// ListedFor reports whether this caller is offered the widget.
	ListedFor func(r *http.Request) bool

	// DragDismiss enables pointer-driven drag-to-dismiss for bottom-edge widgets.
	DragDismiss bool

	// Asset path overrides. Default routes are derived from Name.
	StylePath string
	StatePath string

	// PollMS is the StatePath re-fetch interval in milliseconds; 0 never.
	PollMS int

	// PollTerminal, when non-nil, is evaluated on every /state fetch.
	PollTerminal func() bool

	mountPrefix string
}

// StyleURL returns the URL Mount serves the widget's stylesheet at.
func (d *Definition) StyleURL() string { return d.mountPrefix + d.StylePath }

// StateURL returns the URL Mount serves the widget's signal snapshot at.
func (d *Definition) StateURL() string { return d.mountPrefix + d.StatePath }

// ChromeURL returns the URL Mount serves the widget's chrome HTML at.
func (d *Definition) ChromeURL() string { return d.mountPrefix + chromePathFor(d) }

var (
	registryMu sync.Mutex
	registry   = map[string]*Definition{}
)

func allWidgets() []*Definition {
	registryMu.Lock()
	defer registryMu.Unlock()
	return slices.SortedFunc(maps.Values(registry), func(a, b *Definition) int {
		return strings.Compare(a.Name, b.Name)
	})
}

// Lookup returns the registered widget with this name.
func Lookup(name string) (*Definition, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	d, ok := registry[name]
	return d, ok
}

// AllForSSR returns a snapshot of every registered widget.
func AllForSSR() []*Definition { return allWidgets() }

// IsAvailableOn reports whether the widget's Routes accept path; none accept every path.
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

// AvailableOn returns the registered widgets available on path.
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

// RenderChrome returns a widget's chrome HTML, rendered with context.Background().
func RenderChrome(d *Definition) string {
	return RenderChromeCtx(context.Background(), d)
}

// RenderChromeCtx is RenderChrome with ctx threaded into each slot.
func RenderChromeCtx(ctx context.Context, d *Definition) string {
	return string((&server{def: *d}).renderSkeletonCtx(ctx))
}

// RenderCSS returns the widget's stylesheet as served.
func RenderCSS(d *Definition) string {
	css := widgetCSS(*d)
	if extra := safeExtraCSS(*d); extra != "" {
		css += "\n" + extra
	}
	return minify.ServedCSS(css)
}

// New starts a builder for a widget Definition with sensible defaults.
func New(name string) *Builder {
	return &Builder{
		def: Definition{
			Name:     name,
			Position: BottomRight,
			Signals:  map[string]SignalSource{},
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
		b.Dismissible()
		if b.def.Role == "" {
			b.def.Role = "dialog"
		}
	}
	return b
}

// Dismissible enables Escape and outside-click dismissal for the widget.
func (b *Builder) Dismissible() *Builder {
	b.def.CloseOnEscape = true
	b.def.CloseOnClickOutside = true
	return b
}

func (b *Builder) Slot(name string, c component.Component) *Builder {
	b.def.Slots = append(b.def.Slots, Slot{Name: name, Component: c})
	return b
}

// Signal registers a named server-side signal source.
func (b *Builder) Signal(name string, src SignalSource) *Builder {
	if b.def.Signals == nil {
		b.def.Signals = map[string]SignalSource{}
	}
	b.def.Signals[name] = src
	return b
}

// Poll sets a re-fetch interval for the widget's signal state.
func (b *Builder) Poll(interval time.Duration) *Builder {
	b.def.PollMS = int(interval / time.Millisecond)
	return b
}

// PollTerminal sets the predicate that ends polling once it holds.
func (b *Builder) PollTerminal(fn func() bool) *Builder {
	b.def.PollTerminal = fn
	return b
}

// RPC registers a server-side handler the widget can invoke from the client.
func (b *Builder) RPC(method, path string, h http.Handler) *Builder {
	b.def.RPCs = append(b.def.RPCs, RPCEndpoint{Method: widgetRPCMethod(method), Path: path, Handler: h})
	return b
}

// Backdrop forces a backdrop regardless of position.
func (b *Builder) Backdrop() *Builder { b.def.Backdrop = true; return b }

// Surface paints the widget root as a panel at a corner or banner position.
func (b *Builder) Surface() *Builder { b.def.Surface = true; return b }

// DragDismiss enables drag-to-dismiss for bottom-edge widgets.
func (b *Builder) DragDismiss() *Builder { b.def.DragDismiss = true; return b }

// Role sets the ARIA role on the widget root.
func (b *Builder) Role(r string) *Builder { b.def.Role = r; return b }

// LabelledBy sets aria-labelledby on the widget root.
func (b *Builder) LabelledBy(id string) *Builder { b.def.LabelledBy = id; return b }

// DescribedBy sets aria-describedby on the widget root.
func (b *Builder) DescribedBy(id string) *Builder { b.def.DescribedBy = id; return b }

// Hidden marks the widget as registered-but-not-auto-mounted.
func (b *Builder) Hidden() *Builder { b.def.Hidden = true; return b }

// Pages scopes the widget to exact path matches.
func (b *Builder) Pages(paths ...string) *Builder {
	for _, p := range paths {
		target := p
		b.def.Routes = append(b.def.Routes, func(path string) bool { return path == target })
	}
	return b
}

// PagesPrefix scopes the widget to paths starting with one of prefixes.
func (b *Builder) PagesPrefix(prefixes ...string) *Builder {
	for _, p := range prefixes {
		target := p
		b.def.Routes = append(b.def.Routes, func(path string) bool {
			return strings.HasPrefix(path, target) //gofastr:allow(GOFASTR1006) literal-prefix semantics are the contract-tested API (TestArchContract_PrefixPageScoping): callers pass "/components/" with the boundary
		})
	}
	return b
}

// PagesMatch scopes the widget to paths accepted by the supplied matcher.
func (b *Builder) PagesMatch(fn func(path string) bool) *Builder {
	b.def.Routes = append(b.def.Routes, fn)
	return b
}

// ListedFor sets Definition.ListedFor.
func (b *Builder) ListedFor(fn func(r *http.Request) bool) *Builder {
	b.def.ListedFor = fn
	return b
}

// DeepLink wires this widget to a query-string pair.
func (b *Builder) DeepLink(key, value string) *Builder {
	b.def.DeepLinkKey = key
	b.def.DeepLinkValue = value
	return b
}

// DeepLinkParam mirrors query key into a same-named signal on a deep-link open.
func (b *Builder) DeepLinkParam(key string) *Builder {
	b.def.DeepLinkParams = append(b.def.DeepLinkParams, key)
	return b
}

// Skeleton overrides the default chrome wrapper.
func (b *Builder) Skeleton(fn func(slots map[string]render.HTML) render.HTML) *Builder {
	b.def.Skeleton = fn
	return b
}

// Definition returns the in-progress Definition.
func (b *Builder) Definition() *Definition { return &b.def }

// Build returns the assembled Definition.
func (b *Builder) Build() Definition { return b.def }

var (
	sessionCheck       atomic.Pointer[func(*http.Request) bool]
	authenticatedCheck atomic.Pointer[func(*http.Request) bool]
)

// SetSessionCheck installs the predicate RequireSession consults; nil removes it.
func SetSessionCheck(fn func(*http.Request) bool) {
	if fn == nil {
		sessionCheck.Store(nil)
		return
	}
	sessionCheck.Store(&fn)
}

// SessionCheck returns the installed session predicate, or nil.
func SessionCheck() func(*http.Request) bool {
	if p := sessionCheck.Load(); p != nil {
		return *p
	}
	return nil
}

// SetAuthenticatedCheck installs the predicate RequireAuthenticated consults; nil removes it.
func SetAuthenticatedCheck(fn func(*http.Request) bool) {
	if fn == nil {
		authenticatedCheck.Store(nil)
		return
	}
	authenticatedCheck.Store(&fn)
}

// AuthenticatedCheck returns the installed authenticated predicate, or nil.
func AuthenticatedCheck() func(*http.Request) bool {
	if p := authenticatedCheck.Load(); p != nil {
		return *p
	}
	return nil
}

type gateLevel int

const (
	gateOff gateLevel = iota
	gateAnySession
	gateAuthenticated
)

func (d *Definition) gateLevelOf() gateLevel {
	switch {
	case d.RequireAuthenticated:
		return gateAuthenticated
	case d.RequireSession:
		return gateAnySession
	}
	return gateOff
}

type gatedWidgetKey struct{}

// GatedWidget returns the widget whose gate is asking an installed predicate about r.
func GatedWidget(r *http.Request) (*Definition, bool) {
	d, ok := r.Context().Value(gatedWidgetKey{}).(*Definition)
	return d, ok
}

func gateSatisfied(level gateLevel, d *Definition, r *http.Request) bool {
	var p *atomic.Pointer[func(*http.Request) bool]
	switch level {
	case gateAnySession:
		p = &sessionCheck
	case gateAuthenticated:
		p = &authenticatedCheck
	default:
		return true
	}
	fn := p.Load()
	if fn == nil {
		return false
	}
	return (*fn)(r.WithContext(context.WithValue(r.Context(), gatedWidgetKey{}, d)))
}

// GateSatisfied reports whether r passes def's gate; a gate with no installed predicate refuses.
func GateSatisfied(def *Definition, r *http.Request) bool {
	return gateSatisfied(def.gateLevelOf(), def, r)
}

// OfferedTo reports whether r's caller is offered the widget.
func (d *Definition) OfferedTo(r *http.Request) bool {
	return GateSatisfied(d, r) && d.listedFor(r)
}

func (d *Definition) listedFor(r *http.Request) (ok bool) {
	if d.ListedFor == nil {
		return true
	}
	defer func() {
		if rec := recover(); rec != nil {
			slog.Default().Error("widget: ListedFor hook panicked; not listing the widget",
				"widget", d.Name, "panic", textsafe.Recovered(rec))
			ok = false
		}
	}()
	return d.ListedFor(r)
}

func gateSession(level gateLevel, d *Definition, h http.Handler) http.Handler {
	if level == gateOff {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !gateSatisfied(level, d, r) {
			i18n.HTTPError(w, r, http.StatusForbidden, reasonWidgetNeedsSession)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// Mount registers the widget's routes on r and adds it to the widget registry.
func Mount(r *router.Router, def *Definition) {
	if def.StylePath == "" {
		def.StylePath = "/core-ui/widget/" + def.Name + "/style.css"
	}
	if def.StatePath == "" {
		def.StatePath = "/core-ui/widget/" + def.Name + "/state"
	}

	prefix := r.Prefix()
	endpoints := widgetEndpoints(def, prefix)
	def.mountPrefix = prefix
	for _, e := range endpoints {
		r.Handle(e.method, e.path, e.handler)
	}

	registryMu.Lock()
	registry[def.Name] = def
	registryMu.Unlock()
}

// MountShared serves on r's root every endpoint of def another router published and r does not serve.
func MountShared(r *router.Router, def *Definition) {
	served := map[string]bool{}
	for _, route := range r.Routes() {
		served[route.Method+" "+route.Pattern] = true
	}
	root := r.Root()
	for _, e := range widgetEndpoints(def, def.mountPrefix) {
		url := def.mountPrefix + e.path
		key := e.method + " " + url
		if served[key] {
			continue
		}
		served[key] = true
		root.Handle(e.method, url, e.handler)
	}
}

type widgetEndpoint struct {
	method, path string
	handler      http.Handler
}

func widgetEndpoints(def *Definition, prefix string) []widgetEndpoint {
	srv := &server{def: *def}
	srv.def.mountPrefix = prefix
	gate := def.gateLevelOf()
	endpoints := []widgetEndpoint{
		{"GET", def.StylePath, http.HandlerFunc(srv.serveStyle)},
		{"GET", def.StatePath, gateSession(gate, def, http.HandlerFunc(srv.serveState))},
		{"GET", chromePathFor(def), gateSession(gate, def, http.HandlerFunc(srv.serveChrome))},
	}
	for _, rpc := range def.RPCs {
		method := widgetRPCMethod(rpc.Method)
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			panic(fmt.Sprintf("widget: %s RPC %s has method %q; want GET, POST, PUT, PATCH or DELETE (empty means POST)", def.Name, rpc.Path, rpc.Method))
		}
		endpoints = append(endpoints, widgetEndpoint{method, rpc.Path, gateSession(gate, def, rpc.Handler)})
	}
	return endpoints
}

func widgetRPCMethod(method string) string {
	if method == "" {
		return "POST"
	}
	return method
}

// MountBuilder builds the widget from b and mounts it on r.
func MountBuilder(r *router.Router, b *Builder) {
	def := b.Build()
	Mount(r, &def)
}

// RuntimeTag returns the markup that loads the runtime and mounts every registered widget.
func RuntimeTag() string {
	return RuntimeModuleManifestScript() +
		`<script src="/__gofastr/runtime.js?v=` + runtimeHash() + `"></script>`
}

// MountRuntime registers the framework runtime endpoints on r.
func MountRuntime(r *router.Router) {
	r.Get("/__gofastr/runtime.js", http.HandlerFunc(serveRuntime))
	r.Get("/__gofastr/widgets", http.HandlerFunc(serveWidgetList))
	r.Get("/__gofastr/runtime/{name}", http.HandlerFunc(serveRuntimeModule))
}
