package widget

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/runtime"
	"github.com/gofastr/gofastr/core-ui/style"
)

// server is the per-widget HTTP plumbing: bootstrap script, style
// sheet, and signal state snapshot. One instance per Mount call.
type server struct {
	def Definition
}

//go:embed bootstrap.js
var bootstrapTemplate string

// serveBootstrap returns the per-widget loader script. Composed from a
// shared template plus widget-specific config (signals, SSE bindings,
// initial-state path, slot HTML). The shared template provides:
//
//   - Mounts a root <div data-fui-widget="<name>"> on <body>
//   - Loads core-ui/runtime if not already present (idempotent)
//   - Fetches initial state, renders the chrome with slot HTML, hydrates
//   - Subscribes to SSE bindings, pushes payloads into signals
//   - Wires data-fui-rpc clicks/submits to POST endpoints
//   - Wires data-fui-action="close" to dismiss
func (s *server) serveBootstrap(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	cfg := map[string]any{
		"name":              s.def.Name,
		"position":          string(s.def.Position),
		"backdrop":          s.def.Backdrop,
		"closeOnEscape":     s.def.CloseOnEscape,
		"closeOnClick":      s.def.CloseOnClickOutside,
		"stylePath":         s.def.StylePath,
		"statePath":         s.def.StatePath,
		"sse":               s.def.SSE,
	}
	chrome := s.renderSkeleton()
	init := strings.NewReplacer(
		"__FUI_CONFIG__", encodeJSON(cfg),
		"__FUI_CHROME__", encodeJSON(string(chrome)),
	).Replace(bootstrapTemplate)

	// Prepend the framework runtime so the bootstrap is self-sufficient:
	// any page that includes the script tag gets __gofastr.mountWidget
	// without a separate /__gofastr/runtime.js fetch. The runtime is
	// idempotent (its IIFE registers window.__gofastr only once), so
	// multiple widget tags on the same page don't conflict.
	if rt, err := runtime.RuntimeJS(); err == nil {
		fmt.Fprint(w, rt, "\n", init)
	} else {
		// Fallback: write only the init. Caller must ensure runtime is
		// loaded by some other means.
		fmt.Fprint(w, init)
	}
}

// encodeJSON marshals v with SetEscapeHTML(false) so embedded HTML
// stays readable in the emitted bootstrap (no </> noise).
// Safe because the bootstrap script is served as application/javascript,
// not as HTML, so the usual XSS concern with raw <,> in JSON-in-HTML
// doesn't apply.
func encodeJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	// Encoder appends a trailing newline; trim it for cleaner output.
	return strings.TrimRight(buf.String(), "\n")
}

// serveStyle returns the widget stylesheet. The framework owns
// positioning (corner/center/edge) + chrome (panel, modal, toast,
// backdrop). Hosts that need additional rules — typically content
// styling for slot innards — supply def.ExtraCSS, which is appended
// verbatim after the framework rules.
func (s *server) serveStyle(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, widgetCSS(s.def))
	if s.def.ExtraCSS != nil {
		fmt.Fprint(w, "\n", s.def.ExtraCSS())
	}
}

// serveState returns the current value of every named signal as JSON.
// The bootstrap fetches this on first mount; SSE bindings keep things
// fresh after that.
func (s *server) serveState(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{}
	for name, src := range s.def.Signals {
		v, err := src.Read()
		if err != nil {
			continue
		}
		out[name] = v
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// renderSkeleton produces the initial chrome HTML: framework-default
// for the position, OR the host's custom Skeleton if provided. Slots
// are rendered to HTML and looked up by name.
func (s *server) renderSkeleton() render.HTML {
	slots := map[string]render.HTML{}
	for _, sl := range s.def.Slots {
		slots[sl.Name] = component.RenderComponent(sl.Component)
	}
	if s.def.Skeleton != nil {
		return s.def.Skeleton(slots)
	}
	return defaultSkeleton(s.def, slots)
}

// defaultSkeleton is the framework-built chrome for each Position.
// It writes a <div class="fui-widget fui-pos-<pos>"> with the slot
// contents inserted by name. Hosts that want bespoke chrome supply
// their own Skeleton func.
func defaultSkeleton(def Definition, slots map[string]render.HTML) render.HTML {
	var b strings.Builder
	b.WriteString(`<div class="fui-widget fui-pos-` + string(def.Position) + `" data-fui-widget="` + escAttr(def.Name) + `">`)

	// Render header / body / footer slots if present.
	for _, name := range []string{"header", "body", "footer"} {
		if html, ok := slots[name]; ok {
			b.WriteString(`<div class="fui-slot fui-slot-` + name + `">`)
			b.WriteString(string(html))
			b.WriteString(`</div>`)
		}
	}
	// Render any other slots after the canonical ones.
	canonical := map[string]bool{"header": true, "body": true, "footer": true}
	for name, html := range slots {
		if canonical[name] {
			continue
		}
		b.WriteString(`<div class="fui-slot fui-slot-` + escAttr(name) + `">`)
		b.WriteString(string(html))
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	return render.HTML(b.String())
}

// widgetCSS builds the stylesheet using core-ui/style. Owns:
//
//   - Per-position layout (fui-pos-bottom-right, fui-pos-center, …)
//   - Backdrop styles for modal-mode widgets
//   - Slot containers
//
// The host's slot components carry their own classes (resolved through
// the theme's tokens). This function is intentionally small — it's the
// chrome, not the contents.
func widgetCSS(def Definition) string {
	theme := style.DefaultTheme()
	ss := style.NewStyleSheet(theme)

	// Common widget base.
	ss.Rule(".fui-widget").
		Set(
			"position", "fixed",
			"z-index", "2147483600",
			"font-family", "{fonts.body}",
			"color", "{colors.text}",
			"box-sizing", "border-box",
		).
		End()
	ss.Rule(".fui-widget *, .fui-widget *::before, .fui-widget *::after").
		Set("box-sizing", "border-box").
		End()

	// Position presets.
	for _, p := range []struct{ cls, top, right, bottom, left string }{
		{"fui-pos-bottom-right", "", "20px", "20px", ""},
		{"fui-pos-bottom-left", "", "", "20px", "20px"},
		{"fui-pos-top-right", "20px", "20px", "", ""},
		{"fui-pos-top-left", "20px", "", "", "20px"},
		{"fui-pos-top", "0", "0", "", "0"},
		{"fui-pos-bottom", "", "0", "0", "0"},
	} {
		props := []string{}
		if p.top != "" {
			props = append(props, "top", p.top)
		}
		if p.right != "" {
			props = append(props, "right", p.right)
		}
		if p.bottom != "" {
			props = append(props, "bottom", p.bottom)
		}
		if p.left != "" {
			props = append(props, "left", p.left)
		}
		ss.Rule("." + p.cls).Set(props...).End()
	}
	// Edge mounts (drawer-style).
	ss.Rule(".fui-pos-edge-left").Set("top", "0", "left", "0", "bottom", "0", "width", "min(360px, 90vw)").End()
	ss.Rule(".fui-pos-edge-right").Set("top", "0", "right", "0", "bottom", "0", "width", "min(360px, 90vw)").End()

	// Center / modal: framework provides a backdrop + centered content.
	ss.Rule(".fui-pos-center").
		Set(
			"top", "0", "left", "0", "right", "0", "bottom", "0",
			"display", "flex", "align-items", "center", "justify-content", "center",
		).
		End()

	// Backdrop overlay sits behind any widget that requested it. The
	// bootstrap script appends a <div class="fui-backdrop"> sibling to
	// the widget root when def.Backdrop is true.
	ss.Rule(".fui-backdrop").
		Set(
			"position", "fixed", "inset", "0",
			"background", "rgba(0,0,0,0.45)",
			"z-index", "2147483599",
			"transition", "opacity 0.18s ease",
		).
		End()

	// Slots are pure containers — no styling.
	ss.Rule(".fui-slot").Set("display", "block").End()

	return theme.CSSCustomProperties() + "\n" + ss.CSS()
}

// escAttr does the minimum escaping needed for a value rendered into
// double-quoted HTML attribute. Sufficient for widget names + slot
// names which we control; the host's slot HTML is already escaped by
// component.RenderComponent.
func escAttr(s string) string {
	r := strings.NewReplacer(`"`, `&quot;`, `&`, `&amp;`, `<`, `&lt;`, `>`, `&gt;`)
	return r.Replace(s)
}
