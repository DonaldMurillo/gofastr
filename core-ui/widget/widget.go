package widget

import (
	"net/http"

	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/core/router"
	"github.com/gofastr/gofastr/core-ui/component"
)

// Position is where the widget's root anchors itself when mounted.
// Modal/Center implies a backdrop and focus trap; Corner positions
// float without a backdrop.
type Position string

const (
	BottomRight Position = "bottom-right"
	BottomLeft  Position = "bottom-left"
	TopRight    Position = "top-right"
	TopLeft     Position = "top-left"
	Center      Position = "center"  // modal — backdrop + focus trap
	Top         Position = "top"     // banner across the top
	Bottom      Position = "bottom"  // banner across the bottom
	Edge        Position = "edge-left" // drawer-style edge mount
	EdgeRight   Position = "edge-right"
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
// and pushes new values into [data-fui-signal="<name>"] elements.
type SignalSource interface {
	Read() (any, error)
}

// SignalFunc is a func adapter for SignalSource.
type SignalFunc func() (any, error)

func (f SignalFunc) Read() (any, error) { return f() }

// SSEBinding maps a server-sent event kind to a signal name. When the
// named SSE event arrives on the bus mounted at Path, the runtime
// pushes the event payload into the named signal — every DOM node
// bound to that signal updates.
type SSEBinding struct {
	Path   string `json:"path"`   // e.g. "/.kiln/events"
	Event  string `json:"event"`  // e.g. "world_edit"
	Signal string `json:"signal"` // e.g. "page"
}

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
	SSE       []SSEBinding
	RPCs      []RPCEndpoint

	// Skeleton is the host's chrome wrapper. If nil, the framework
	// uses a sensible default for the chosen Position (FloatingPanel
	// for corners, Modal for Center, etc.). Most hosts leave this
	// nil and use a preset.
	Skeleton func(slots map[string]render.HTML) render.HTML

	// Modal flags
	Backdrop      bool // dim the page behind the widget
	CloseOnEscape bool // ESC closes the widget
	CloseOnClickOutside bool

	// Asset path overrides. Default routes are derived from Name.
	BootstrapPath string // default: /core-ui/widget/<name>/bootstrap.js
	StylePath     string // default: /core-ui/widget/<name>/style.css
	StatePath     string // default: /core-ui/widget/<name>/state
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

// SSE binds an SSE event to a signal. When the event fires on the
// stream at path, the event's payload becomes the named signal's
// new value.
func (b *Builder) SSE(path, event, signal string) *Builder {
	b.def.SSE = append(b.def.SSE, SSEBinding{
		Path: path, Event: event, Signal: signal,
	})
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

// Skeleton overrides the default chrome wrapper.
func (b *Builder) Skeleton(fn func(slots map[string]render.HTML) render.HTML) *Builder {
	b.def.Skeleton = fn
	return b
}

// Build returns the assembled Definition.
func (b *Builder) Build() Definition { return b.def }

// --- Mount ------------------------------------------------------------

// Mount wires the widget's HTTP routes onto r and returns the bootstrap
// script tag the host can embed in any page. Default paths
// (BootstrapPath, StylePath, StatePath) are filled in on def if unset
// so the caller can read them after Mount returns.
//
// Routes mounted:
//
//	GET  <BootstrapPath>     bootstrap.js — the per-widget loader
//	GET  <StylePath>         widget styles (theme-resolved CSS)
//	GET  <StatePath>         JSON snapshot of all signals (initial render)
//	*    <RPC.Path>          for each RPC, the host's handler
//
// SSE bindings reference an existing event bus the host already serves
// (we don't re-broadcast; we just instruct the client where to listen).
func Mount(r *router.Router, def *Definition) string {
	if def.BootstrapPath == "" {
		def.BootstrapPath = "/core-ui/widget/" + def.Name + "/bootstrap.js"
	}
	if def.StylePath == "" {
		def.StylePath = "/core-ui/widget/" + def.Name + "/style.css"
	}
	if def.StatePath == "" {
		def.StatePath = "/core-ui/widget/" + def.Name + "/state"
	}

	srv := &server{def: *def}

	r.Get(def.BootstrapPath, http.HandlerFunc(srv.serveBootstrap))
	r.Get(def.StylePath, http.HandlerFunc(srv.serveStyle))
	r.Get(def.StatePath, http.HandlerFunc(srv.serveState))

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

	return `<script src="` + def.BootstrapPath + `"></script>`
}
