package widget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// runtimeHash is the SHA256 of the embedded runtime.js, computed once
// at first use. Cache-busts the script URL so a new build invalidates
// any previously-cached runtime in the browser.
var (
	runtimeHashOnce sync.Once
	runtimeHashVal  string
)

func runtimeHash() string {
	runtimeHashOnce.Do(func() {
		js, err := runtime.RuntimeJS()
		if err != nil {
			runtimeHashVal = "dev"
			return
		}
		sum := sha256.Sum256([]byte(js))
		runtimeHashVal = hex.EncodeToString(sum[:8]) // 16 hex chars is plenty
	})
	return runtimeHashVal
}

// Per-module content-addressed hashes for the split runtime modules.
// Each module URL ships with ?v=<hash>; the response uses
// `Cache-Control: public, max-age=31536000, immutable` so the browser
// caches it forever and a new build (different hash → different URL)
// busts cleanly.
var (
	moduleHashesOnce sync.Once
	moduleHashes     = map[string]string{}
)

// RuntimeModuleHash returns the content-addressed hash for a split
// runtime module. Used by client-side preload tags + by the loader
// to construct `?v=<hash>` URLs. Empty string if the module isn't
// embedded.
func RuntimeModuleHash(name string) string {
	moduleHashesOnce.Do(func() {
		for _, n := range runtime.ModuleNames() {
			src, ok := runtime.Module(n)
			if !ok {
				continue
			}
			sum := sha256.Sum256([]byte(src))
			moduleHashes[n] = hex.EncodeToString(sum[:8])
		}
	})
	return moduleHashes[name]
}

// RuntimeModuleManifestScript emits an inert JSON manifest mapping every
// split runtime module to its content-addressed hash. Returns "" when no
// modules are embedded.
//
// Both RuntimeTag (kiln + manual hosts) and framework/uihost embed this
// script. Pages without the manifest fall through to un-versioned module
// URLs and then collide with the immutable cache headers — see
// TestRuntimeTagEmbedsModuleManifest for the regression that motivated this.
func RuntimeModuleManifestScript() string {
	names := runtime.ModuleNames()
	if len(names) == 0 {
		return ""
	}
	out := make(map[string]string, len(names))
	for _, n := range names {
		out[n] = RuntimeModuleHash(n)
	}
	buf, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-runtime-modules">` +
		escapeJSONForScript(buf) +
		`</script>`
}

// escapeJSONForScript neutralises the one HTML sequence that can
// prematurely terminate an inline <script>…</script> block: closing `</`.
// JSON itself never produces it, but URL strings or user-controlled
// payloads can.
func escapeJSONForScript(buf []byte) string {
	return strings.ReplaceAll(string(buf), `</`, `<\/`)
}

// ServeRuntimeModule is the exported handler for /__gofastr/runtime/<name>.js.
// Hosts that mount routes via uihost get it through framework/uihost;
// kiln and standalone hosts can wire it themselves alongside MountRuntime.
func ServeRuntimeModule(w http.ResponseWriter, r *http.Request) {
	serveRuntimeModule(w, r)
}

// serveRuntimeModule returns one split runtime module by name. URL
// shape: /__gofastr/runtime/<name>.js?v=<hash>. Served with
// long-lived immutable cache headers since the URL itself busts on
// a new build.
func serveRuntimeModule(w http.ResponseWriter, r *http.Request) {
	// URL path: /__gofastr/runtime/<name>.js → strip prefix + .js.
	const prefix = "/__gofastr/runtime/"
	path := r.URL.Path
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, ".js") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSuffix(strings.TrimPrefix(path, prefix), ".js")
	src, ok := runtime.Module(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	// Content-addressed URL (?v=<hash>) → safe to cache forever.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	fmt.Fprint(w, src)
}

// server is the per-widget HTTP plumbing: stylesheet + signal state
// snapshot. One instance per Mount call.
type server struct {
	def Definition
}

// serveRuntime returns the framework runtime JS at /__gofastr/runtime.js.
// Single URL for every page; the runtime self-discovers widgets via
// /__gofastr/widgets at startup. Pages embed the runtime URL with a
// ?v=<hash> cache-bust query param (see RuntimeTag) so a new build
// invalidates any previously cached runtime.
//
// Belt-and-suspenders cache headers: no-store + no-cache + must-revalidate
// + Pragma:no-cache + Expires:0 covers every browser quirk.
func serveRuntime(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	rt, err := runtime.RuntimeJS()
	if err != nil {
		http.Error(w, "runtime unavailable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, rt)
}

// ServeWidgetList returns the JSON list of registered widgets — the
// payload the framework runtime fetches at /__gofastr/widgets to
// discover and mount what's been registered with widget.Mount.
// Exported so hosts that already own the /__gofastr/widgets route
// (e.g. framework/uihost, which serves an empty stub for widget-free
// apps) can delegate to the registry without double-registering the
// HTTP route.
func ServeWidgetList(w http.ResponseWriter, r *http.Request) { serveWidgetList(w, r) }

// serveWidgetList returns the JSON registry of widgets. Metadata
// ONLY — chrome HTML is shipped via the per-widget endpoint
// `/core-ui/widget/<name>/chrome` and fetched lazily when the
// runtime needs to insert the widget. Same pattern as styles +
// state: minimal index, fetch on demand.
//
// The runtime appends `?page=<pathname>` when fetching the catalog
// so widgets scoped via .Pages / .PagesPrefix / .PagesMatch are
// filtered out of pages they don't belong to. A request with no
// page parameter returns the unfiltered registry — backwards-compat
// for static catalog inspection.
//
// statePath is only emitted when the widget declared signals — the
// runtime skips the /state hydrate fetch when this field is absent.
func serveWidgetList(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Query().Get("page")
	var defs []*Definition
	if page != "" {
		defs = AvailableOn(page)
	} else {
		defs = allWidgets()
	}
	out := make([]map[string]any, 0, len(defs))
	for _, d := range defs {
		cfg := map[string]any{
			"name":           d.Name,
			"position":       string(d.Position),
			"backdrop":       d.Backdrop,
			"closeOnEscape":  d.CloseOnEscape,
			"closeOnClick":   d.CloseOnClickOutside,
			"stylePath":      d.StylePath,
			"chromePath":     chromePathFor(d),
			"sse":            d.SSE,
			"deepLinkKey":    d.DeepLinkKey,
			"deepLinkValue":  d.DeepLinkValue,
			"deepLinkParams": d.DeepLinkParams,
		}
		// statePath is omitted entirely when there are no signals so
		// the runtime can skip the empty round-trip on every mount.
		if len(d.Signals) > 0 {
			cfg["statePath"] = d.StatePath
		}
		out = append(out, map[string]any{
			"hidden": d.Hidden,
			"cfg":    cfg,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
}

// chromePathFor returns the route the runtime fetches to get the
// widget's rendered chrome HTML. Path-derivable from name; explicit
// in the registry so hosts can override the convention if needed.
func chromePathFor(d *Definition) string {
	return "/core-ui/widget/" + d.Name + "/chrome"
}

// serveChrome returns the rendered chrome HTML for a single widget.
// Cache-Control: no-store because the chrome may depend on per-widget
// signal defaults that change between deploys; let the client refetch
// rather than serve stale HTML from a CDN.
func (s *server) serveChrome(w http.ResponseWriter, _ *http.Request) {
	chrome := s.renderSkeleton()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(chrome))
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
	b.WriteString(`<div class="fui-widget fui-pos-` + string(def.Position) + `" data-fui-widget="` + escAttr(def.Name) + `"`)
	if def.Role != "" {
		b.WriteString(` role="` + escAttr(def.Role) + `"`)
	}
	if def.Backdrop {
		// aria-modal=true tells assistive tech the rest of the page is
		// not currently interactive. Only meaningful for backdrop'd
		// surfaces; plain panels/banners shouldn't claim it.
		b.WriteString(` aria-modal="true"`)
	}
	if def.LabelledBy != "" {
		b.WriteString(` aria-labelledby="` + escAttr(def.LabelledBy) + `"`)
	}
	if def.DescribedBy != "" {
		b.WriteString(` aria-describedby="` + escAttr(def.DescribedBy) + `"`)
	}
	if def.DragDismiss {
		// data-fui-drag-dismiss is the runtime delegator hook; the
		// runtime listens on the widget root and the handle bar.
		b.WriteString(` data-fui-drag-dismiss="true"`)
	}
	b.WriteString(`>`)

	if def.DragDismiss {
		// Visible drag-handle bar at the top of the panel. role=button
		// keyboard activation isn't meaningful (drag is the gesture);
		// keep it inert for AT and rely on ESC + backdrop click as the
		// keyboard/pointer dismiss path.
		b.WriteString(`<div class="fui-widget-drag-handle" aria-hidden="true" data-fui-drag-handle="true"></div>`)
	}

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
	// Honour the `hidden` HTML attribute even when a `.fui-pos-*`
	// selector tries to apply display: flex/grid. SSR-inlined widgets
	// rely on `hidden` to stay invisible until openWidget removes it.
	ss.Rule(".fui-widget[hidden]").
		Set("display", "none").
		End()
	ss.Rule(".fui-widget *, .fui-widget *::before, .fui-widget *::after").
		Set("box-sizing", "border-box").
		End()

	// Position presets — 6 corner/edge points + 2 full-width banners.
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
	// Horizontally centered top + bottom — toast stacks anchored at
	// the viewport's top-center or bottom-center. transform centers
	// the element regardless of its width.
	ss.Rule(".fui-pos-top-center").
		Set("top", "20px", "left", "50%", "transform", "translateX(-50%)").
		End()
	ss.Rule(".fui-pos-bottom-center").
		Set("bottom", "20px", "left", "50%", "transform", "translateX(-50%)").
		End()

	// Edge mounts (drawer-style). Background + shadow so the drawer
	// is visually distinct from the dimmed page below; overflow:auto
	// lets long content scroll inside the drawer.
	ss.Rule(".fui-pos-edge-left").
		Set("top", "0", "left", "0", "bottom", "0",
			"width", "min(360px, 90vw)",
			"background", "{colors.surface}",
			"box-shadow", "{shadows.xl}",
			"overflow", "auto",
		).
		End()
	ss.Rule(".fui-pos-edge-right").
		Set("top", "0", "right", "0", "bottom", "0",
			"width", "min(360px, 90vw)",
			"background", "{colors.surface}",
			"box-shadow", "{shadows.xl}",
			"overflow", "auto",
		).
		End()

	// Center / modal: framework provides a backdrop + centered content
	// wrapper. The wrapper itself stays transparent so the slot
	// component (e.g. demo-modal-body) owns the visual card chrome.
	//
	// `pointer-events: none` on the wrapper is load-bearing: the
	// wrapper IS the full viewport (top/left/right/bottom: 0) and sits
	// ABOVE the backdrop in the z-order, so without it real mouse
	// clicks on the dim area always land on the wrapper and never
	// reach the backdrop's click handler — the "backdrop-click closes"
	// affordance breaks silently. Direct children are flipped back to
	// `pointer-events: auto` so the slot content stays interactive.
	ss.Rule(".fui-pos-center").
		Set(
			"top", "0", "left", "0", "right", "0", "bottom", "0",
			"display", "flex", "align-items", "center", "justify-content", "center",
			"padding", "{spacing.lg}",
			"pointer-events", "none",
		).
		End()
	ss.Rule(".fui-pos-center > *").
		Set("pointer-events", "auto").
		End()

	// Backdrop overlay sits behind any widget that requested it. The
	// bootstrap script appends a <div class="fui-backdrop"> sibling to
	// the widget root when def.Backdrop is true.
	ss.Rule(".fui-backdrop").
		Set(
			"position", "fixed", "inset", "0",
			"background", "rgba(0,0,0,0.45)",
			"z-index", "2147483599",
			"animation", "fui-backdrop-in {durations.overlay-enter} {easings.ease-out}",
		).
		End()

	// Entrance animations for modal + drawer + sheet surfaces. The
	// runtime appends the widget root after mount; the animation
	// runs once on insertion. Theme-driven duration + easing so a
	// single theme tweak retunes every surface.
	ss.Rule(".fui-pos-center").
		Set("animation", "fui-overlay-scale-in {durations.overlay-enter} {easings.spring}").
		End()
	ss.Rule(".fui-pos-edge-left").
		Set("animation", "fui-edge-left-in {durations.overlay-enter} {easings.ease-out}").
		End()
	ss.Rule(".fui-pos-edge-right").
		Set("animation", "fui-edge-right-in {durations.overlay-enter} {easings.ease-out}").
		End()
	ss.Rule(".fui-pos-bottom").
		Set("animation", "fui-bottom-in {durations.overlay-enter} {easings.ease-out}").
		End()
	ss.Rule(".fui-pos-top").
		Set("animation", "fui-top-in {durations.overlay-enter} {easings.ease-out}").
		End()

	// Slots are pure containers — no styling.
	ss.Rule(".fui-slot").Set("display", "block").End()

	// ─── Drag-to-dismiss handle (bottom sheets) ───────────────────
	// Visible 40×4px rounded bar centered at the top of the panel.
	// The runtime attaches pointer handlers when it sees
	// data-fui-drag-dismiss on the widget root.
	ss.Rule(".fui-widget-drag-handle").
		Set(
			"display", "block",
			"width", "40px",
			"height", "4px",
			"margin", "8px auto 4px",
			"border-radius", "2px",
			"background", "{colors.border}",
			"touch-action", "none",
			"cursor", "grab",
		).
		End()
	// Wider drag affordance: the entire widget root accepts the
	// initial pointerdown when DragDismiss is on. touch-action: none
	// keeps the browser from claiming vertical pans for scrolling.
	ss.Rule(`[data-fui-widget][data-fui-drag-dismiss]`).
		Set("touch-action", "none").
		End()
	// While dragging the runtime sets data-fui-dragging=true; disable
	// the entrance animation + transitions so the live transform isn't
	// fought by competing tweens.
	ss.Rule(`[data-fui-widget][data-fui-dragging]`).
		Set(
			"animation", "none",
			"transition", "none",
			"will-change", "transform",
		).
		End()
	ss.Rule(`[data-fui-widget][data-fui-drag-handle="true"]`).
		Set("cursor", "grabbing").
		End()

	// ─── Anchored popover chrome ────────────────────────────────────
	// When the runtime positions a popover next to its trigger
	// (data-fui-popover-anchor on the trigger), it sets
	// data-fui-popover-side="top|bottom|left|right" on the widget
	// root after picking the final placement (post auto-flip). The
	// rules below paint the surface, constrain its size so it can
	// actually fit beside small triggers on small viewports, and
	// draw a directional arrow back to the trigger via a ::before
	// pseudo-element. All values come from the canonical theme so a
	// single token tweak retunes every popover surface.
	// Popover root keeps overflow visible so the arrow ::before (sitting
	// outside the root's box at -7px) renders. The scroll cap moves
	// to the inner .fui-slot below — a tall slot scrolls inside the
	// popover, the arrow stays visible.
	ss.Rule("[data-fui-widget][data-fui-popover-side]").
		Set("border-radius", "{radii.md}",
			"box-shadow", "{shadows.lg}",
			"background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"max-inline-size", "min(360px, calc(100vw - 32px))",
			"max-block-size", "calc(100vh - 32px)",
			"display", "flex",
			"flex-direction", "column").
		End()
	// Inner slot scrolls when content exceeds the popover's
	// max-block-size. flex: 1 1 auto + min-block-size: 0 lets the
	// slot consume whatever the parent allows; overflow-y: auto then
	// engages on tall content. Keeps the arrow on the root visible.
	ss.Rule(`[data-fui-widget][data-fui-popover-side] > .fui-slot`).
		Set("flex", "1 1 auto",
			"min-block-size", "0",
			"max-block-size", "100%",
			"overflow-y", "auto").
		End()
	ss.Rule(`[data-fui-widget][data-fui-popover-side]::before`).
		Set("content", "''",
			"position", "absolute",
			"inline-size", "12px",
			"block-size", "12px",
			"background", "{colors.surface}",
			"border-inline-start", "1px solid {colors.border}",
			"border-block-start", "1px solid {colors.border}").
		End()
	// side="top" → popover sits ABOVE trigger → arrow at its
	// bottom edge, pointing DOWN.
	ss.Rule(`[data-fui-widget][data-fui-popover-side="top"]::before`).
		Set("inset-block-end", "-7px",
			"inset-inline-start", "var(--ui-popover-arrow-x, 16px)",
			"transform", "translateX(-50%) rotate(225deg)").
		End()
	// side="bottom" → popover sits BELOW trigger → arrow at its
	// top edge, pointing UP.
	ss.Rule(`[data-fui-widget][data-fui-popover-side="bottom"]::before`).
		Set("inset-block-start", "-7px",
			"inset-inline-start", "var(--ui-popover-arrow-x, 16px)",
			"transform", "translateX(-50%) rotate(45deg)").
		End()
	// side="left" → popover sits LEFT of trigger → arrow at its
	// right edge, pointing RIGHT.
	ss.Rule(`[data-fui-widget][data-fui-popover-side="left"]::before`).
		Set("inset-inline-end", "-7px",
			"inset-block-start", "var(--ui-popover-arrow-y, 16px)",
			"transform", "translateY(-50%) rotate(135deg)").
		End()
	// side="right" → popover sits RIGHT of trigger → arrow at its
	// left edge, pointing LEFT.
	ss.Rule(`[data-fui-widget][data-fui-popover-side="right"]::before`).
		Set("inset-inline-start", "-7px",
			"inset-block-start", "var(--ui-popover-arrow-y, 16px)",
			"transform", "translateY(-50%) rotate(-45deg)").
		End()

	// Trigger highlight: the runtime adds .is-popover-trigger-active
	// to the originating button while its popover is open. Default
	// styling: invert background to the primary color so the user
	// can clearly tell which trigger fired the surface. Apps that
	// want a different highlight can override the rule at site level.
	ss.Rule(".is-popover-trigger-active").
		Set("background", "{colors.primary}",
			"color", "{colors.primary-fg}",
			"border-color", "{colors.primary}",
			"box-shadow", "0 0 0 3px color-mix(in oklab, {colors.primary} 25%, transparent)").
		End()

	// Keyframes + reduced-motion suppression. Emitted as raw CSS
	// because the StyleSheet builder doesn't yet model @keyframes /
	// @media — small targeted escape hatch.
	const animationCSS = `
@keyframes fui-backdrop-in    { from { opacity: 0; } to { opacity: 1; } }
@keyframes fui-overlay-scale-in { from { opacity: 0; transform: scale(0.96); } to { opacity: 1; transform: scale(1); } }
@keyframes fui-edge-left-in     { from { transform: translateX(-100%); } to { transform: translateX(0); } }
@keyframes fui-edge-right-in    { from { transform: translateX(100%);  } to { transform: translateX(0); } }
@keyframes fui-bottom-in        { from { transform: translateY(100%);  } to { transform: translateY(0); } }
@keyframes fui-top-in           { from { transform: translateY(-100%); } to { transform: translateY(0); } }
@media (prefers-reduced-motion: reduce) {
  .fui-backdrop, .fui-pos-center, .fui-pos-edge-left, .fui-pos-edge-right, .fui-pos-bottom, .fui-pos-top {
    animation: none !important;
  }
}
`
	// Do NOT prepend theme.CSSCustomProperties() — that would emit a
	// :root block of the framework DefaultTheme tokens AFTER the
	// host's app.css, overwriting any custom theme an app has set
	// via app.WithTheme(...). The widget's CSS uses bare var(--…)
	// refs; those resolve against whatever :root the page already
	// has (app.css owns that). DefaultTheme is only used here to
	// supply token VALUES to the token-substituting StyleSheet DSL
	// during build, never as an emitted :root override.
	return ss.CSS() + animationCSS
}

// escAttr does the minimum escaping needed for a value rendered into
// double-quoted HTML attribute. Sufficient for widget names + slot
// names which we control; the host's slot HTML is already escaped by
// component.RenderComponent.
func escAttr(s string) string {
	r := strings.NewReplacer(`"`, `&quot;`, `&`, `&amp;`, `<`, `&lt;`, `>`, `&gt;`)
	return r.Replace(s)
}
