package widget

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/runtime"
	"github.com/gofastr/gofastr/core-ui/style"
)

// server is the per-widget HTTP plumbing: stylesheet + signal state
// snapshot. One instance per Mount call.
type server struct {
	def Definition
}

// serveRuntime returns the framework runtime JS at /__gofastr/runtime.js.
// Single URL for every page; the runtime self-discovers widgets via
// /__gofastr/widgets at startup.
func serveRuntime(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	rt, err := runtime.RuntimeJS()
	if err != nil {
		http.Error(w, "runtime unavailable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, rt)
}

// serveWidgetList returns the JSON list of registered widgets. The
// runtime fetches this at startup, calls mountWidget() for each entry.
// Each entry contains the same cfg + chrome HTML the per-widget
// bootstrap used to inline.
func serveWidgetList(w http.ResponseWriter, _ *http.Request) {
	defs := allWidgets()
	out := make([]map[string]any, 0, len(defs))
	for _, d := range defs {
		s := &server{def: *d}
		chrome := s.renderSkeleton()
		out = append(out, map[string]any{
			"cfg": map[string]any{
				"name":          d.Name,
				"position":      string(d.Position),
				"backdrop":      d.Backdrop,
				"closeOnEscape": d.CloseOnEscape,
				"closeOnClick":  d.CloseOnClickOutside,
				"stylePath":     d.StylePath,
				"statePath":     d.StatePath,
				"sse":           d.SSE,
			},
			"chrome": string(chrome),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
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
