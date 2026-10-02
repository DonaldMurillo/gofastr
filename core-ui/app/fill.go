package app

// Outlet fills: the resolution side of the tree-layout primitive
// (docs/DESIGN-layout-outlets.md, "Fills" and the "Render algorithm"
// loop). A screen or a screen group declares a component for a
// (layout, outlet) address; the renderer resolves, per outlet of every
// tree layer in the screen's chain, the FIRST candidate that loads:
// the screen's fill, then group fills innermost to outermost, then the
// outlet's Default.
//
// The decided policies only (DESIGN "Fills"): candidates load
// concurrently, one goroutine per outlet, bounded; a failing fill is
// contained to its outlet. The P2/ prototype switches that once
// selected page-error and sequential variants are gone — the losing
// variants were deleted, not flagged.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
)

// ErrNoFill is returned by a fill component's Load to DECLINE the
// candidate: the outlet falls through to the next candidate (a group
// fill, the Default, the fallback). Any other Load error is the page's
// error.
var ErrNoFill = errors.New("app: no fill for this route")

// Fill is one resolved non-primary fill on the wire: the outlet or area
// address, its HTML, and the FNV-64a hash of that HTML (hex). The hash
// lets the client skip re-applying an unchanged fill.
type Fill struct {
	Addr string
	HTML render.HTML
}

// fillDecl is one recorded fill declaration: its component and,
// optionally, the region guard attached to it (app.FillPolicy,
// .
type fillDecl struct {
	comp   component.Component
	policy Policy
}

// Fill declares c as the fill for outlet o on this screen. o must be
// a handle of a layout in the screen's resolved chain (the render
// panics otherwise, naming the layout and the outlet). The fill
// component is instantiated and loaded per request exactly like a
// screen component (fresh instance, route params, DI, Load). opts
// attach a region guard (FillPolicy) to this declaration.
func (s *Screen) Fill(o *Outlet, c component.Component, opts ...FillOption) *Screen {
	if s.fills == nil {
		s.fills = make(map[*Outlet]fillDecl)
	}
	d := fillDecl{comp: c}
	for _, opt := range opts {
		opt(&d)
	}
	s.fills[o] = d
	return s
}

// Fill declares c as the fill for outlet o on every screen of this
// group (and its sub-groups) that does not fill the outlet itself. o
// must be a handle of a layout in every member screen's resolved chain
// (the render panics otherwise, naming the layout and the outlet).
// Group fills are consulted innermost group first, after the screen's
// own fill and before the outlet's Default. opts attach a region guard
// (FillPolicy) to this declaration.
func (g *ScreenGroup) Fill(o *Outlet, c component.Component, opts ...FillOption) *ScreenGroup {
	if g.fills == nil {
		g.fills = make(map[*Outlet]fillDecl)
	}
	d := fillDecl{comp: c}
	for _, opt := range opts {
		opt(&d)
	}
	g.fills[o] = d
	return g
}

// WithLoading declares the group's swap-slot loading default (,
// every member screen without its own
// declaration shows this while a navigation to it is in flight.
func (g *ScreenGroup) WithLoading(ld *Loading) *ScreenGroup {
	g.loading = ld
	return g
}

// screenLoading resolves a screen's effective swap-slot loading
// declaration (): the screen's own, else the innermost group's,
// else nil.
func screenLoading(s *Screen) *Loading {
	if s.Loading != nil {
		return s.Loading
	}
	for g := s.group; g != nil; g = g.parent {
		if g.loading != nil {
			return g.loading
		}
	}
	return nil
}

// treeFill is one recorded fill during a render.
type treeFill struct {
	layer int
	html  render.HTML
}

// fillSet records the resolved fills of one render, by wire address.
// Lookups are by exact address; output order comes from walking the
// chain (exportFillsFor), never from map iteration.
//
// failures records the outlets whose fill FAILED and was contained
// ( variant B, and the streaming resolver's forced containment):
// the render degrades the outlet and marches on, and the record is
// how a static export can refuse to bake the degraded bytes in
// (RenderResult.FillFailures) while a live server keeps containing.
//
// notFoundOutlet records the 404-outlet outcome: some FallbackNotFound
// outlet of the chain had no candidate that renders (every candidate
// declined with ErrNoFill; NewLayout refuses a Default beside
// FallbackNotFound). Deferred outlets never decide it on the page
// request — their candidates never run there; the part request owns
// that decision (RenderPartResult's whole-page reset).
type fillSet struct {
	entries        map[string]treeFill
	failures       []FillFailure
	notFoundOutlet bool
}

func (f *fillSet) addFailure(addr string, err error) {
	f.failures = append(f.failures, FillFailure{Addr: addr, Err: err})
}

func (f *fillSet) get(addr string) (treeFill, bool) {
	if f == nil {
		return treeFill{}, false
	}
	tf, ok := f.entries[addr]
	return tf, ok
}

func (f *fillSet) set(addr string, tf treeFill) {
	if f.entries == nil {
		f.entries = make(map[string]treeFill)
	}
	f.entries[addr] = tf
}

// newComponentInstance returns a per-request shallow copy of a
// registered component (the same contract Screen.newInstance gives
// screen components): construction-time configuration survives, Load
// mutations land on request-private storage.
func newComponentInstance(c component.Component) component.Component {
	if c == nil {
		return nil
	}
	v := reflect.ValueOf(c)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return c
	}
	fresh := reflect.New(v.Elem().Type())
	fresh.Elem().Set(v.Elem())
	return fresh.Interface().(component.Component)
}

// maxConcurrentFills bounds the fills' goroutines. Pages carry a
// handful of outlets; the bound exists so a pathological chain cannot
// fan out unbounded (DESIGN "Fills load concurrently", bounded).
const maxConcurrentFills = 8

// fillSlot is one outlet to resolve: its wire address, its spec, and
// its candidates in resolution order.
type fillSlot struct {
	addr       string
	spec       OutletSpec
	candidates []component.Component
	// layer is the chain index of the owning layer; slots are listed in
	// chain order, so a subtree partial's KEPT layers (0..shared-1)
	// contribute exactly the first slots.
	//.
	layer int
}

// fillSlots lists every outlet of every tree layout in chain, in chain
// order — the declaration order results are re-assembled in.
func fillSlots(screen *Screen, chain []LayoutLayer) []fillSlot {
	var slots []fillSlot
	for i, layer := range chain {
		if !layer.Layout.isTree() {
			continue
		}
		key := layer.Key()
		for _, o := range layer.Layout.spec.Outlets {
			slots = append(slots, fillSlot{
				addr:       key + "#" + o.Name(),
				spec:       o.OutletSpec,
				candidates: fillCandidates(screen, o),
				layer:      i,
			})
		}
	}
	return slots
}

// fillCandidates lists an outlet's fill candidates in resolution order:
// the screen's own fill, group fills innermost to outermost, then the
// outlet's Default.
func fillCandidates(screen *Screen, o *Outlet) []component.Component {
	var out []component.Component
	if screen != nil {
		if d, ok := screen.fills[o]; ok {
			out = append(out, d.comp)
		}
	}
	for g := screen.group; g != nil; g = g.parent {
		if d, ok := g.fills[o]; ok {
			out = append(out, d.comp)
		}
	}
	if o.Default != nil {
		out = append(out, o.Default)
	}
	return out
}

// validateFills is the render-time half of "Validation at mount"
// (DESIGN): every fill the screen or one of its groups declares must
// name an outlet handle whose owning layout is in the screen's
// resolved chain — pointer identity, the way app.go compares layers.
// A fill for a layout outside the chain (or a handle no layout ever
// declared) is a wiring mistake whose only silent alternative is an
// outlet that never fills, so it panics here, naming the layout and
// the outlet, in the voice of router.go's registration checks.
func validateFills(screen *Screen, chain []LayoutLayer) {
	owners := make(map[*Layout]bool, len(chain))
	for _, layer := range chain {
		if layer.Layout != nil {
			owners[layer.Layout] = true
		}
	}
	check := func(o *Outlet, who string) {
		if o == nil {
			return
		}
		if o.owner == nil {
			panic(fmt.Sprintf("app: %s fills outlet %q, which no layout declares — pass the handle to NewLayout", who, o.Name()))
		}
		if !owners[o.owner] {
			panic(fmt.Sprintf("app: %s fills outlet %q of layout %q, which is not in the screen's layout chain", who, o.Name(), o.owner.Name))
		}
	}
	if screen != nil {
		for o := range screen.fills {
			check(o, fmt.Sprintf("screen %q", screen.Path))
		}
		for g := screen.group; g != nil; g = g.parent {
			for o := range g.fills {
				check(o, fmt.Sprintf("screen group %q", g.prefix))
			}
		}
	}
}

// fillSlotResult is one slot's outcome: the resolved fill HTML (empty
// for an unfilled outlet) and, when the fill FAILED and was contained,
// the error that failed it (containedErr) so the render can record the
// degraded outlet (RenderResult.FillFailures).
//
// notFound marks the 404-outlet outcome (FallbackNotFound, DESIGN
// "404 outlet"): every candidate declined (ErrNoFill) and the spec has
// no Default (NewLayout refuses the pair), so the render outcome
// is not-found, not an empty region. A contained failure is NOT a
// decline: it never sets notFound.
type fillSlotResult struct {
	html         render.HTML
	containedErr error
	notFound     bool
}

// resolveFills resolves the fill HTML of every outlet of every tree
// layout in chain. Per outlet, candidates load in resolution order:
// screen fill, then group fills innermost to outermost, then the
// outlet's Default; a candidate whose Load returns ErrNoFill (or
// wraps it) declines to the next. An outlet nothing fills renders
// empty (FallbackNothing) or — for FallbackNotFound — records the
// 404-outlet outcome on the fill set (fills.notFoundOutlet), which the
// render turns into the not-found page with status 404 (Decided 5).
//
// Outlets load concurrently (DESIGN "Fills load concurrently"): one
// goroutine per outlet, bounded by maxConcurrentFills, results kept
// in declaration order. A failing fill is contained to its outlet
// (DESIGN "A failing fill is contained to its outlet"): the error is
// logged scrubbed, the outlet renders the failing fill's
// ErrorBoundary fallback, else the outlet's Default, else nothing,
// and the page keeps the screen's status.
//
// deferOutlets (X-Gofastr-Defer, skips every
// DEFERRED outlet's candidates entirely - no loader runs - and records
// the outlet's loading content as its fill, so the payload carries the
// region's placeholder in place (kept layers as an envelope fill,
// rendered layers inline in the cell). The fill itself arrives as its
// own part request.
//
// Areas are NOT resolved here: they are recorded by the build itself
// (LayoutTree.RouteArea runs on every render, kept layers included).
func (a *App) resolveFills(ctx context.Context, path string, params map[string]string, screen *Screen, chain []LayoutLayer, deferOutlets bool, guards regionGuards) *fillSet {
	anyTree := false
	for _, layer := range chain {
		if layer.Layout.isTree() {
			anyTree = true
			break
		}
	}
	if !anyTree {
		return nil
	}
	fills := &fillSet{}
	slots := fillSlots(screen, chain)
	if deferOutlets {
		// Splice the deferred slots out of the resolution batch: they
		// resolve to their loading content without touching a single
		// candidate (the part request owns the real fill).
		live := slots[:0]
		for _, slot := range slots {
			if slot.spec.Deferred {
				fills.set(slot.addr, treeFill{html: deferredFillHTML(ctx, slot.spec)})
				continue
			}
			live = append(live, slot)
		}
		slots = live
	}
	if len(slots) == 0 {
		return fills
	}
	results := a.resolveSlotsConcurrent(ctx, path, params, slots, guards)
	for i, slot := range slots {
		if results[i].containedErr != nil {
			fills.addFailure(slot.addr, results[i].containedErr)
		}
		if results[i].notFound {
			fills.notFoundOutlet = true
		}
		fills.set(slot.addr, treeFill{html: results[i].html})
	}
	return fills
}

// deferredFillHTML renders a deferred outlet's placeholder: the spec's
// Loading content, rendered once under the same containment every
// presentational render gets (no Load, no DI, no params - loading
// content must not fetch). An outlet without a Loading declaration
// renders nothing in place; the dim alone covers the wait.
func deferredFillHTML(ctx context.Context, spec OutletSpec) render.HTML {
	if spec.Loading == nil || spec.Loading.Show == nil {
		return ""
	}
	html, err := component.SafeRenderCtx(ctx, spec.Loading.Show)
	if err != nil {
		return ""
	}
	return html
}

// deferredAddrs lists the wire addresses of every deferred outlet in
// chain, in chain order - the per-route list the manifest carries so
// the client knows, before any request, which regions travel as parts.
func deferredAddrs(chain []LayoutLayer) []string {
	var out []string
	for _, layer := range chain {
		if !layer.Layout.isTree() {
			continue
		}
		key := layer.Key()
		for _, o := range layer.Layout.spec.Outlets {
			if o.Deferred {
				out = append(out, key+"#"+o.Name())
			}
		}
	}
	return out
}

// resolveFillSlot resolves ONE outlet: the first candidate that loads
// renders, ErrNoFill declines to the next, and a non-ErrNoFill error
// is contained to the outlet (DESIGN "A failing fill is contained to
// its outlet"): logged scrubbed, the region degrades to the failing
// fill's ErrorBoundary fallback, else the outlet's Default, else
// nothing. The catch-all recover converts an escaped panic (a
// panicking SetParams, a panicking DI walk) into the same error
// channel a Load panic already takes: every resolution runs inside a
// goroutine, where an escaped panic would kill the process.
func (a *App) resolveFillSlot(ctx context.Context, path string, params map[string]string, slot fillSlot, guards regionGuards) (res fillSlotResult) {
	defer func() {
		if r := recover(); r != nil {
			res = fillSlotResult{
				html:         a.containedFillError(ctx, path, params, slot, 0, nil, errors.New("fill resolution panicked: "+textsafe.Recovered(r))),
				containedErr: fmt.Errorf("fill resolution panicked: %s", textsafe.Recovered(r)),
			}
		}
	}()
	// A region guard's recorded decision replaces the candidate walk
	//. Block renders the region's fallback (the
	// outlet's Default, else nothing) without loading a single
	// candidate; RenderAlt prepends the alt component, which outranks
	// the screen fill.
	candidates := slot.candidates
	if d, ok := guards[slot.addr]; ok {
		if d.kind == DecisionBlock {
			if slot.spec.Default != nil {
				if _, html, err := a.loadFillComponent(ctx, path, params, slot.spec.Default); err == nil {
					return fillSlotResult{html: html}
				}
			}
			return fillSlotResult{html: ""}
		}
		if alt := altCandidates(guards, slot); alt != nil {
			candidates = alt
		}
	}
	for i, c := range candidates {
		inst, html, err := a.loadFillComponent(ctx, path, params, c)
		if errors.Is(err, ErrNoFill) {
			continue
		}
		if err != nil {
			return fillSlotResult{html: a.containedFillError(ctx, path, params, slot, i, inst, err), containedErr: err}
		}
		return fillSlotResult{html: html}
	}
	// Fallback: FallbackDefault with a Default component was already a
	// candidate above, so an unfilled outlet renders nothing — except
	// FallbackNotFound (no Default can be declared beside it,
	// NewLayout refuses the pair), whose clean fall-through is the
	// 404-outlet outcome, not an empty region. Only a full decline
	// reaches here: a contained failure returned above and never set
	// notFound, and a guard's Block rendered Default-or-nothing without
	// consulting a single candidate.
	if slot.spec.Fallback == FallbackNotFound {
		return fillSlotResult{notFound: true}
	}
	return fillSlotResult{html: ""}
}

// rebuildFillsFromDefaults rewrites the fill set for the 404-outlet
// outcome: every outlet of every tree layer in chain — the one that
// 404'd included — renders its Default, if it has one and the Default
// renders, else nothing. The route's own fills belong to the route's
// screen, and this outcome is not that screen; the not-found page's
// regions are the spec's fallbacks (DESIGN "404 outlet"). Areas are
// untouched: they are recorded later by the builds themselves.
func (a *App) rebuildFillsFromDefaults(ctx context.Context, path string, params map[string]string, fills *fillSet, chain []LayoutLayer) {
	if fills == nil {
		return
	}
	for _, layer := range chain {
		if !layer.Layout.isTree() {
			continue
		}
		key := layer.Key()
		for _, o := range layer.Layout.spec.Outlets {
			var html render.HTML
			if o.Default != nil {
				if _, h, err := a.loadFillComponent(ctx, path, params, o.Default); err == nil {
					html = h
				} else {
					slog.Default().Error("app: 404-outlet default failed; outlet empty",
						"addr", key+"#"+o.Name(),
						"path", textsafe.StripUnsafe(path))
				}
			}
			fills.set(key+"#"+o.Name(), treeFill{html: html})
		}
	}
}

// resolveSlotsConcurrent resolves every slot in parallel (DESIGN
// "Fills load concurrently"): one goroutine per outlet, bounded by
// maxConcurrentFills, hand-rolled (WaitGroup + semaphore channel;
// golang.org/x/sync sits in go.mod as an indirect require only). The
// request context flows into every goroutine unchanged:
// client-disconnect cancellation reaches every Load, but a failing
// sibling does NOT cancel the rest — an early spike derived a
// cancelling context here and the cancellation error then masqueraded
// as the declaration-order-first failure (and races whichever
// goroutine recorded first), so the page error was neither
// deterministic nor the real cause. Siblings run to completion instead
// (the decided containment needs every outlet's own outcome anyway);
// results land by index — the slice is never appended under
// concurrency — so which goroutine finished first is unobservable.
func (a *App) resolveSlotsConcurrent(ctx context.Context, path string, params map[string]string, slots []fillSlot, guards regionGuards) []fillSlotResult {
	results := make([]fillSlotResult, len(slots))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentFills)
	for i, slot := range slots {
		wg.Add(1)
		go func(i int, slot fillSlot) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = a.resolveFillSlot(ctx, path, params, slot, guards)
		}(i, slot)
	}
	wg.Wait()
	return results
}

// containedFillError renders the degraded outlet of a contained fill
// failure. The failure is logged with the screen path's own scrubbing
// (slog.Default, every request-derived value through textsafe); then
// the outlet renders, in order: the failing fill's own ErrorBoundary
// fallback, else the outlet's Default, else nothing. The error text
// reaches the HTML only through the component's own RenderError,
// whose escaping is the component's business exactly like any Render.
// failed is nil when the panic escaped before any candidate existed.
func (a *App) containedFillError(ctx context.Context, path string, params map[string]string, slot fillSlot, failedIdx int, failed component.Component, err error) render.HTML {
	slog.Default().Error("app: fill failed; outlet degraded to fallback",
		"addr", slot.addr,
		"path", textsafe.StripUnsafe(path),
		"err", textsafe.Recovered(err))
	if eb, ok := failed.(component.ErrorBoundary); ok {
		if html, ok := containedRenderError(ctx, eb, err); ok {
			return html
		}
		return "" // RenderError itself panicked: nothing, not a broken box
	}
	// The failing candidate was the Default itself: nothing is left to
	// try for this outlet.
	if slot.spec.Default != nil && failedIdx == len(slot.candidates)-1 {
		return ""
	}
	if slot.spec.Default != nil {
		if _, html, derr := a.loadFillComponent(ctx, path, params, slot.spec.Default); derr == nil {
			return html
		}
		slog.Default().Error("app: fill outlet default failed; outlet empty",
			"addr", slot.addr,
			"path", textsafe.StripUnsafe(path))
	}
	return ""
}

// containedRenderError runs an ErrorBoundary's RenderError under the
// same panic-to-error containment a component render gets: a
// panicking fallback renders nothing rather than killing the page.
func containedRenderError(ctx context.Context, eb component.ErrorBoundary, err error) (out render.HTML, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			renderdiag.Report(ctx, "fill fallback", eb, r)
			out, ok = "", false
		}
	}()
	return eb.RenderError(err), true
}

// loadFillComponent instantiates and loads ONE fill candidate exactly
// the way RenderPageResult treats a screen component: fresh instance,
// route params via SetParams, DI injection, Load under containment,
// render under containment. It returns the per-request instance (so
// the caller can consult its ErrorBoundary on failure), the rendered
// HTML, and the error. ErrNoFill (declined) propagates to the caller;
// every other error is the page's error via fillFailure under variant
// A, or contained per outlet under variant B.
func (a *App) loadFillComponent(ctx context.Context, path string, params map[string]string, c component.Component) (component.Component, render.HTML, error) {
	comp := newComponentInstance(c)
	if ps, ok := comp.(ParamSetter); ok && len(params) > 0 {
		ps.SetParams(params)
	}
	if err := a.injectComponent(comp, path); err != nil {
		return comp, "", err
	}
	if loader, ok := comp.(ScreenLoader); ok {
		if err := safeScreenLoad(loader, ctx); err != nil {
			return comp, "", err
		}
	}
	html, renderErr := component.SafeRenderCtx(ctx, comp)
	if renderErr != nil {
		return comp, html, renderErr
	}
	return comp, html, nil
}

// exportFillsFor walks the chain outermost to the given limit (the
// shared depth of a subtree partial) and emits the recorded fills of
// layers 0..limit-1 in chain order — deterministic, no map iteration.
// Layers at or below the limit render inline in the payload, their
// fills are not exported.
func exportFillsFor(chain []LayoutLayer, fills *fillSet, limit int) []Fill {
	if fills == nil || limit <= 0 {
		return nil
	}
	var out []Fill
	for i := 0; i < limit && i < len(chain); i++ {
		layer := chain[i]
		if !layer.Layout.isTree() {
			continue
		}
		key := layer.Key()
		for _, o := range layer.Layout.spec.Outlets {
			addr := key + "#" + o.Name()
			if tf, ok := fills.get(addr); ok {
				out = append(out, Fill{Addr: addr, HTML: tf.html})
			}
		}
		for _, area := range layer.Layout.spec.Areas {
			addr := key + "~" + area.Name
			if tf, ok := fills.get(addr); ok {
				out = append(out, Fill{Addr: addr, HTML: tf.html})
			}
		}
	}
	return out
}
