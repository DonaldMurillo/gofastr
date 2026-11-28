package widget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/compute"
	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	corestatic "github.com/DonaldMurillo/gofastr/core/static"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

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
		runtimeHashVal = hex.EncodeToString(sum[:8])
	})
	return runtimeHashVal
}

// RuntimeHash returns the core runtime's content hash, its ?v= value.
func RuntimeHash() string { return runtimeHash() }

// Per-module content-addressed hashes for the split runtime modules.
func RuntimeModuleHash(name string) string { return runtime.ModuleHash(name) }

// RuntimeModuleManifestJSON maps every runtime module to its hash, or is nil.
func RuntimeModuleManifestJSON() []byte {
	names := runtime.ModuleNames()
	if len(names) == 0 {
		return nil
	}
	out := make(map[string]string, len(names))
	for _, n := range names {
		out[n] = RuntimeModuleHash(n)
	}
	buf, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return buf
}

// RuntimeModuleManifestScript renders the module manifest as an inert JSON script.
func RuntimeModuleManifestScript() string {
	var script string
	if buf := RuntimeModuleManifestJSON(); buf != nil {
		script = `<script type="application/json" id="gofastr-runtime-modules">` +
			escapeJSONForScript(buf) +
			`</script>`
	}
	return script + BehaviorsManifestScript() + ComputeManifestScript() + uiregistry.DataBlocksHTML()
}

// BehaviorsManifestScript renders the behaviours block as an inert JSON script.
func BehaviorsManifestScript() string {
	buf := runtime.BehaviorsJSON()
	if buf == nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-behaviors">` +
		escapeJSONForScript(buf) +
		`</script>`
}

// ComputeManifestScript renders the compute asset hashes as an inert JSON script.
func ComputeManifestScript() string {
	return computeManifestScript(compute.Manifest())
}

func computeManifestScript(manifest map[string]compute.Versions) string {
	if len(manifest) == 0 {
		return ""
	}
	buf, err := json.Marshal(manifest)
	if err != nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-compute-assets">` +
		escapeJSONForScript(buf) +
		`</script>`
}

func escapeJSONForScript(buf []byte) string {
	return strings.ReplaceAll(string(buf), `</`, `<\/`)
}

// ServeRuntimeModule is the exported handler for /__gofastr/runtime/<name>.js.
func ServeRuntimeModule(w http.ResponseWriter, r *http.Request) {
	serveRuntimeModule(w, r)
}

func serveRuntimeModule(w http.ResponseWriter, r *http.Request) {
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
	if serveContentAddressed(w, r, runtime.ModuleHash(name)) {
		return
	}
	fmt.Fprint(w, src)
}

func serveContentAddressed(w http.ResponseWriter, r *http.Request, hash string) bool {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	etag := ""
	if hash != "" {
		etag = `"` + hash + `"`
		h.Set("ETag", etag)
	}
	if hash != "" && r.URL.Query().Get("v") == hash {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	if etag != "" && corestatic.IfNoneMatch(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return r.Method == http.MethodHead
}

const computeWorkerCSP = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'"

// ServeComputeAsset serves /__gofastr/compute/<name>.js and .wasm.
func ServeComputeAsset(w http.ResponseWriter, r *http.Request) {
	const prefix = "/__gofastr/compute/"
	path := r.URL.Path
	if !strings.HasPrefix(path, prefix) {
		http.NotFound(w, r)
		return
	}
	relative := strings.TrimPrefix(path, prefix)

	var (
		asset       *compute.Asset
		contentType string
		ok          bool
	)
	switch {
	case strings.HasSuffix(relative, ".js"):
		asset, ok = compute.LookupWorker(strings.TrimSuffix(relative, ".js"))
		contentType = "application/javascript; charset=utf-8"
	case strings.HasSuffix(relative, ".wasm"):
		asset, ok = compute.LookupWASM(strings.TrimSuffix(relative, ".wasm"))
		contentType = "application/wasm"
	default:
		http.NotFound(w, r)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if strings.HasSuffix(relative, ".js") {
		w.Header().Set("Content-Security-Policy", computeWorkerCSP)
	}
	if serveContentAddressed(w, r, asset.Hash()) {
		return
	}
	_, _ = asset.WriteTo(w)
}

type server struct {
	def Definition
}

func serveRuntime(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	rt, err := runtime.RuntimeJS()
	if err != nil {
		log.Printf("widget: runtime unavailable: %v", err)
		i18n.HTTPError(w, r, http.StatusInternalServerError, reasonRuntimeUnavailable)
		return
	}
	fmt.Fprint(w, rt)
}

// ServeWidgetList serves the /__gofastr/widgets catalog.
func ServeWidgetList(w http.ResponseWriter, r *http.Request) { serveWidgetList(w, r) }

type catalogEntry struct {
	Cfg    catalogConfig `json:"cfg"`
	Hidden bool          `json:"hidden"`
}

type catalogConfig struct {
	Backdrop       bool     `json:"backdrop"`
	ChromePath     string   `json:"chromePath"`
	CloseOnClick   bool     `json:"closeOnClick"`
	CloseOnEscape  bool     `json:"closeOnEscape"`
	DeepLinkKey    string   `json:"deepLinkKey"`
	DeepLinkParams []string `json:"deepLinkParams"`
	DeepLinkValue  string   `json:"deepLinkValue"`
	Name           string   `json:"name"`
	PollMS         int      `json:"pollMs,omitempty"`
	Position       Position `json:"position"`
	StatePath      string   `json:"statePath,omitempty"`
	StylePath      string   `json:"stylePath"`
}

func serveWidgetList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Cookie")
	page, err := url.PathUnescape(r.URL.Query().Get("page"))
	if err != nil {
		i18n.HTTPError(w, r, http.StatusBadRequest, reasonInvalidPage)
		return
	}
	var defs []*Definition
	if page != "" {
		defs = AvailableOn(page)
	} else {
		defs = allWidgets()
	}
	out := make([]catalogEntry, 0, len(defs))
	for _, d := range defs {
		if !d.OfferedTo(r) {
			continue
		}
		cfg := catalogConfig{
			Name:           d.Name,
			Position:       d.Position,
			Backdrop:       d.Backdrop,
			CloseOnEscape:  d.CloseOnEscape,
			CloseOnClick:   d.CloseOnClickOutside,
			StylePath:      d.StyleURL(),
			ChromePath:     d.ChromeURL(),
			DeepLinkKey:    d.DeepLinkKey,
			DeepLinkValue:  d.DeepLinkValue,
			DeepLinkParams: d.DeepLinkParams,
		}
		if len(d.Signals) > 0 {
			cfg.StatePath = d.StateURL()
			cfg.PollMS = max(d.PollMS, 0)
		}
		out = append(out, catalogEntry{Cfg: cfg, Hidden: d.Hidden})
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		slog.DebugContext(r.Context(), "widget catalog write failed", "err", err)
	}
}

func chromePathFor(d *Definition) string {
	return "/core-ui/widget/" + d.Name + "/chrome"
}

// MaxChromeContext bounds the trigger-carried chrome context (#321).
const MaxChromeContext = 256

type chromeCtxKey struct{}

// WithChromeContext returns ctx carrying v as a chrome render's trigger context.
func WithChromeContext(ctx context.Context, v string) context.Context {
	return context.WithValue(ctx, chromeCtxKey{}, v)
}

// ChromeContext returns the trigger context of this chrome render, or "".
func ChromeContext(ctx context.Context) string {
	v, _ := ctx.Value(chromeCtxKey{}).(string)
	return v
}

func validChromeContext(v string) bool {
	if len(v) > MaxChromeContext {
		return false
	}
	if !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return false
		}
	}
	return true
}

func (s *server) serveChrome(w http.ResponseWriter, r *http.Request) {
	triggerCtx := r.URL.Query().Get("ctx")
	if !validChromeContext(triggerCtx) {
		i18n.HTTPError(w, r, http.StatusBadRequest, reasonInvalidChrome)
		return
	}
	chrome := s.renderSkeletonCtx(WithChromeContext(r.Context(), triggerCtx))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(chrome))
}

func (s *server) serveStyle(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, RenderCSS(&s.def))
}

func safeExtraCSS(def Definition) (css string) {
	if def.ExtraCSS == nil {
		return ""
	}
	defer func() {
		if rec := recover(); rec != nil {
			slog.Default().Error("widget: ExtraCSS hook panicked; appending no extra rules",
				"widget", def.Name, "panic", textsafe.Recovered(rec))
			css = ""
		}
	}()
	return def.ExtraCSS()
}

func (s *server) serveState(w http.ResponseWriter, _ *http.Request) {
	terminal := s.def.PollTerminal != nil && s.def.PollTerminal()
	out := map[string]json.RawMessage{}
	partial := false
	for name, src := range s.def.Signals {
		v, err := src.Read()
		if err != nil {
			partial = true
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			slog.Default().Error("widget: signal value has no JSON form; omitting it",
				"widget", s.def.Name, "signal", name, "err", err)
			partial = true
			continue
		}
		out[name] = b
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if terminal && !partial {
		w.Header().Set("X-Gofastr-Poll-Stop", "1")
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		slog.Default().Debug("widget: state write", "widget", s.def.Name, "err", err)
	}
}

func (s *server) renderSkeletonCtx(ctx context.Context) render.HTML {
	slots := map[string]render.HTML{}
	for _, sl := range s.def.Slots {
		slots[sl.Name], _ = component.SafeRenderCtx(ctx, sl.Component)
	}
	if s.def.Skeleton != nil {
		return s.safeSkeleton(slots)
	}
	return defaultSkeleton(s.def, slots)
}

func (s *server) safeSkeleton(slots map[string]render.HTML) (out render.HTML) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Default().Error("widget: Skeleton hook panicked; rendering empty chrome",
				"widget", s.def.Name, "panic", textsafe.Recovered(rec))
			out = ""
		}
	}()
	return s.def.Skeleton(slots)
}

var paintsOwnPanel = map[Position]bool{Center: true, Edge: true, EdgeRight: true, Bottom: true}

func defaultSkeleton(def Definition, slots map[string]render.HTML) render.HTML {
	var b strings.Builder
	cls := "cui-widget cui-pos-" + render.Escape(string(def.Position))
	if def.Surface && !paintsOwnPanel[def.Position] {
		cls += " cui-surface"
	}
	tag := "div"
	if def.Backdrop {
		tag = "dialog"
	}
	b.WriteString(`<` + tag + ` class="` + cls + `" data-cui-widget="` + render.Escape(def.Name) + `"`)
	if def.Role != "" && (tag == "div" || def.Role != "dialog") {
		b.WriteString(` role="` + render.Escape(def.Role) + `"`)
	}
	if def.Backdrop {
		b.WriteString(` open`)
		if !def.CloseOnEscape {
			b.WriteString(` closedby="none"`)
		}
	}
	if def.LabelledBy != "" {
		b.WriteString(` aria-labelledby="` + render.Escape(def.LabelledBy) + `"`)
	}
	if def.DescribedBy != "" {
		b.WriteString(` aria-describedby="` + render.Escape(def.DescribedBy) + `"`)
	}
	if def.DragDismiss {
		b.WriteString(` data-cui-drag-dismiss="true"`)
	}
	b.WriteString(`>`)

	if def.DragDismiss {
		b.WriteString(`<div class="cui-widget-drag-handle" aria-hidden="true" data-cui-drag-handle="true"></div>`)
	}

	if def.Position == Center {
		b.WriteString(`<div class="cui-panel">`)
	}
	for _, name := range []string{"header", "body", "footer"} {
		if html, ok := slots[name]; ok {
			b.WriteString(`<div class="cui-slot cui-slot-` + name + `">`)
			b.WriteString(string(html))
			b.WriteString(`</div>`)
		}
	}
	canonical := map[string]bool{"header": true, "body": true, "footer": true}
	for _, name := range slices.Sorted(maps.Keys(slots)) {
		if canonical[name] {
			continue
		}
		b.WriteString(`<div class="cui-slot cui-slot-` + render.Escape(name) + `">`)
		b.WriteString(string(slots[name]))
		b.WriteString(`</div>`)
	}
	if def.Position == Center {
		b.WriteString(`</div>`)
	}
	b.WriteString(`</` + tag + `>`)
	return render.HTML(b.String())
}

func widgetCSS(def Definition) string {
	theme := style.DefaultTheme()
	ss := style.NewStyleSheet(theme)

	ss.Rule(".cui-widget").
		Set(
			"position", "fixed",
			//gofastr:allow(GOFASTR1823) dev and agent widgets sit above every app layer, so no theme z step may move them
			"z-index", "2147483600",
			"font-family", "{fonts.body}",
			"color", "{colors.text}",
			"box-sizing", "border-box",
		).
		End()
	ss.Rule(".cui-widget[hidden]").
		Set("display", "none").
		End()
	// A widget the runtime raises into the top layer (the toast stack's,
	// as a manual popover) keeps its mount position: the browser's
	// popover defaults (inset 0, auto margin, border, padding, canvas
	// background) would centre and frame it. :where keeps the reset below
	// every position class.
	ss.Rule(":where(.cui-widget[popover])").
		Set(
			"inset", "auto",
			"margin", "0",
			"padding", "0",
			"border", "0",
			"background", "transparent",
			"overflow", "visible",
			"color", "inherit",
		).
		End()
	ss.Rule(".cui-widget *, .cui-widget *::before, .cui-widget *::after").
		Set("box-sizing", "border-box").
		End()

	for _, p := range []struct{ cls, top, right, bottom, left string }{
		{"cui-pos-bottom-right", "", "calc(var(--spacing-sm, 4px) * 5)", "calc(var(--spacing-sm, 4px) * 5)", ""},
		{"cui-pos-bottom-left", "", "", "calc(var(--spacing-sm, 4px) * 5)", "calc(var(--spacing-sm, 4px) * 5)"},
		{"cui-pos-top-right", "calc(var(--spacing-sm, 4px) * 5)", "calc(var(--spacing-sm, 4px) * 5)", "", ""},
		{"cui-pos-top-left", "calc(var(--spacing-sm, 4px) * 5)", "", "", "calc(var(--spacing-sm, 4px) * 5)"},
		{"cui-pos-top", "0", "0", "", "0"},
		{"cui-pos-bottom", "", "0", "0", "0"},
	} {
		props := []string{}
		if p.top != "" {
			props = append(props, "top", p.top)
		}
		if p.right != "" {
			props = append(props, "inset-inline-end", p.right)
		}
		if p.bottom != "" {
			props = append(props, "bottom", p.bottom)
		}
		if p.left != "" {
			props = append(props, "inset-inline-start", p.left)
		}
		ss.Rule("." + p.cls).Set(props...).End()
	}
	ss.Rule(".cui-pos-top-center").
		Set("top", "calc(var(--spacing-sm, 4px) * 5)", "left", "50%", "transform", "translateX(-50%)").
		End()
	ss.Rule(".cui-pos-bottom-center").
		Set("bottom", "calc(var(--spacing-sm, 4px) * 5)", "left", "50%", "transform", "translateX(-50%)").
		End()

	ss.Rule(".cui-pos-edge-left").
		Set("top", "0", "inset-inline-start", "0", "bottom", "0",
			"width", "min(var(--ui-widget-drawer-width, 360px), 90vw)",
			"background", "{colors.surface}",
			"box-shadow", "{shadows.xl}",
			"overflow", "auto",
		).
		End()
	ss.Rule(".cui-pos-edge-right").
		Set("top", "0", "inset-inline-end", "0", "bottom", "0",
			"width", "min(var(--ui-widget-drawer-width, 360px), 90vw)",
			"background", "{colors.surface}",
			"box-shadow", "{shadows.xl}",
			"overflow", "auto",
		).
		End()

	ss.Rule(".cui-pos-bottom").
		Set(
			"background", "{colors.surface}",
			"box-shadow", "{shadows.xl}",
			"border-radius", "{radii.lg} {radii.lg} 0 0",
			"max-block-size", "75vh",
			"overflow", "auto",
		).
		End()
	ss.Rule(".cui-pos-bottom > .cui-slot").
		Set("padding", "{spacing.md} {spacing.lg} {spacing.lg}").
		End()

	ss.Rule(".cui-pos-center").
		Set(
			"inset", "0",
			"display", "flex", "align-items", "center", "justify-content", "center",
			"padding", "{spacing.lg}",
			"pointer-events", "none",
		).
		End()
	ss.Rule(".cui-pos-center > *").
		Set("pointer-events", "auto").
		End()

	ss.Rule(":where(dialog.cui-widget)").
		Set(
			"margin", "0", "padding", "0", "border", "0",
			"background", "transparent",
			"inset", "auto",
			"inline-size", "auto", "block-size", "auto",
			"max-inline-size", "none", "max-block-size", "none",
			"overflow", "visible",
			"overscroll-behavior", "contain",
		).
		End()
	ss.Rule("dialog.cui-pos-center").Set("pointer-events", "auto").End()
	ss.Rule("dialog.cui-widget::backdrop").
		Set(
			"background", "var(--ui-scrim, rgba(0,0,0,0.45))",
			"animation", "cui-backdrop-in {durations.overlay-enter} {easings.ease-out}",
		).
		End()

	ss.Rule(".cui-backdrop").
		Set(
			"position", "fixed", "inset", "0",
			"background", "var(--ui-scrim, rgba(0,0,0,0.45))",
			//gofastr:allow(GOFASTR1823) one below the widget layer above, outside the theme's z steps for the same reason
			"z-index", "2147483599",
			"animation", "cui-backdrop-in {durations.overlay-enter} {easings.ease-out}",
		).
		End()

	ss.Rule(".cui-pos-center").
		Set("animation", "cui-overlay-scale-in {durations.overlay-enter} {easings.spring}").
		End()
	ss.Rule(".cui-pos-edge-left").
		Set("animation", "cui-edge-left-in {durations.overlay-enter} {easings.ease-out}").
		End()
	ss.Rule(".cui-pos-edge-right").
		Set("animation", "cui-edge-right-in {durations.overlay-enter} {easings.ease-out}").
		End()
	ss.Rule(".cui-pos-edge-left:dir(rtl)").Set("animation-name", "cui-edge-right-in").End()
	ss.Rule(".cui-pos-edge-right:dir(rtl)").Set("animation-name", "cui-edge-left-in").End()
	ss.Rule(".cui-pos-bottom").
		Set("animation", "cui-bottom-in {durations.overlay-enter} {easings.ease-out}").
		End()
	ss.Rule(".cui-pos-top").
		Set("animation", "cui-top-in {durations.overlay-enter} {easings.ease-out}").
		End()

	ss.Rule(".cui-slot").Set("display", "block").End()

	ss.Rule(`.cui-pos-center > .cui-panel:not(:has(> .cui-slot > .cui-slot-bare))`).
		Set(
			"background", "{colors.surface}",
			"border", "{strokes.thin} solid {colors.border}",
			"border-radius", "{radii.lg}",
			"padding", "{spacing.lg}",
			"box-shadow", "{shadows.xl}",
			"min-inline-size", "min(var(--ui-widget-modal-min-width, 20rem), 100%)",
			"max-inline-size", "min(var(--ui-widget-modal-max-width, 36rem), 100%)",
			"max-block-size", "100%",
			"overflow", "auto",
		).
		End()

	ss.Rule(".cui-widget-drag-handle").
		Set(
			"display", "block",
			"width", "var(--ui-widget-drag-handle-width, 40px)",
			"height", "var(--ui-widget-drag-handle-height, 4px)",
			"margin", "var(--spacing-md, 8px) auto var(--spacing-sm, 4px)",
			"border-radius", "calc({radii.sm} / 3)",
			"background", "{colors.border}",
			"touch-action", "none",
			"cursor", "grab",
		).
		End()
	ss.Rule(`[data-cui-widget][data-cui-dragging]`).
		Set(
			"animation", "none",
			"transition", "none",
			"will-change", "transform",
		).
		End()
	ss.Rule(`[data-cui-widget][data-cui-dragging] [data-cui-drag-handle="true"]`).
		Set("cursor", "grabbing").
		End()

	ss.Rule("[data-cui-widget][data-cui-popover-side], .cui-widget.cui-surface").
		Set("border-radius", "{radii.md}",
			"box-shadow", "{shadows.lg}",
			"background", "{colors.surface}",
			"border", "{strokes.thin} solid {colors.border}",
			"max-inline-size", "min(var(--ui-popover-max-width, 360px), calc(100vw - var(--spacing-2xl, 32px)))",
			"max-block-size", "calc(100vh - var(--spacing-2xl, 32px))",
			"display", "flex",
			"flex-direction", "column").
		End()
	ss.Rule(`[data-cui-widget][data-cui-popover-side] > .cui-slot, .cui-widget.cui-surface > .cui-slot`).
		Set("flex", "1 1 auto",
			"min-block-size", "0",
			"max-block-size", "100%",
			"overflow-y", "auto").
		End()
	ss.Rule(`[data-cui-widget][data-cui-popover-side]::before`).
		Set("content", "''",
			"direction", "ltr",
			"position", "absolute",
			"inline-size", "var(--ui-popover-arrow-size, 12px)",
			"block-size", "var(--ui-popover-arrow-size, 12px)",
			"background", "{colors.surface}",
			"border-inline-start", "{strokes.thin} solid {colors.border}",
			"border-block-start", "{strokes.thin} solid {colors.border}").
		End()
	ss.Rule(`[data-cui-widget][data-cui-popover-side="top"]::before`).
		Set("inset-block-end", "calc(-1 * (var(--ui-popover-arrow-size, 12px) / 2 + var(--stroke-thin, 1px)))",
			"inset-inline-start", "var(--ui-popover-arrow-x, 16px)",
			"transform", "translateX(-50%) rotate(225deg)").
		End()
	ss.Rule(`[data-cui-widget][data-cui-popover-side="bottom"]::before`).
		Set("inset-block-start", "calc(-1 * (var(--ui-popover-arrow-size, 12px) / 2 + var(--stroke-thin, 1px)))",
			"inset-inline-start", "var(--ui-popover-arrow-x, 16px)",
			"transform", "translateX(-50%) rotate(45deg)").
		End()
	ss.Rule(`[data-cui-widget][data-cui-popover-side="left"]::before`).
		Set("inset-inline-end", "calc(-1 * (var(--ui-popover-arrow-size, 12px) / 2 + var(--stroke-thin, 1px)))",
			"inset-block-start", "var(--ui-popover-arrow-y, 16px)",
			"transform", "translateY(-50%) rotate(135deg)").
		End()
	ss.Rule(`[data-cui-widget][data-cui-popover-side="right"]::before`).
		Set("inset-inline-start", "calc(-1 * (var(--ui-popover-arrow-size, 12px) / 2 + var(--stroke-thin, 1px)))",
			"inset-block-start", "var(--ui-popover-arrow-y, 16px)",
			"transform", "translateY(-50%) rotate(-45deg)").
		End()

	ss.Rule(".is-popover-trigger-active").
		Set("background", "{colors.primary}",
			"color", "{colors.primary-fg}",
			"border-color", "{colors.primary}",
			"box-shadow", "0 0 0 calc(1.5 * {strokes.focus}) color-mix(in oklab, {colors.primary} 25%, transparent)").
		End()

	const animationCSS = `
@keyframes cui-backdrop-in    { from { opacity: 0; } to { opacity: 1; } }
@keyframes cui-overlay-scale-in { from { opacity: 0; transform: scale(0.96); } to { opacity: 1; transform: scale(1); } }
@keyframes cui-edge-left-in     { from { transform: translateX(-100%); } to { transform: translateX(0); } }
@keyframes cui-edge-right-in    { from { transform: translateX(100%);  } to { transform: translateX(0); } }
@keyframes cui-bottom-in        { from { transform: translateY(100%);  } to { transform: translateY(0); } }
@keyframes cui-top-in           { from { transform: translateY(-100%); } to { transform: translateY(0); } }
@media (prefers-reduced-motion: reduce) {
  .cui-backdrop, .cui-pos-center, .cui-pos-edge-left, .cui-pos-edge-right, .cui-pos-bottom, .cui-pos-top {
    animation: none !important;
  }
}
`
	return ss.CSS() + animationCSS
}
