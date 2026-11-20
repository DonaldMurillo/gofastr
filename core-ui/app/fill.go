package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
)

// ErrNoFill is returned by a fill component's Load to DECLINE the candidate: the outlet falls
// through to the next candidate (a group fill, the Default, the fallback).
var ErrNoFill = errors.New("app: no fill for this route")

// Fill is one resolved non-primary fill on the wire: the outlet or area address and its HTML.
type Fill struct {
	Addr string
	HTML render.HTML
}

type fillDecl struct {
	comp   component.Component
	policy Policy
}

// Fill declares c as the fill for outlet o on this screen.
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

// Fill declares c as the fill for outlet o on every screen of this group (and its sub-groups) that
// does not fill the outlet itself.
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

// WithLoading declares the group's swap-slot loading default (, every member screen without its own
// declaration shows this while a navigation to it is in flight.
func (g *ScreenGroup) WithLoading(ld *Loading) *ScreenGroup {
	g.loading = ld
	return g
}

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

type treeFill struct {
	layer int
	html  render.HTML
}

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

const maxConcurrentFills = 8

type fillSlot struct {
	addr       string
	spec       OutletSpec
	candidates []component.Component
	layer      int
}

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

type fillSlotResult struct {
	html         render.HTML
	containedErr error
	notFound     bool
}

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

func (a *App) resolveFillSlot(ctx context.Context, path string, params map[string]string, slot fillSlot, guards regionGuards) (res fillSlotResult) {
	defer func() {
		if r := recover(); r != nil {
			res = fillSlotResult{
				html:         a.containedFillError(ctx, path, params, slot, 0, nil, errors.New("fill resolution panicked: "+textsafe.Recovered(r))),
				containedErr: fmt.Errorf("fill resolution panicked: %s", textsafe.Recovered(r)),
			}
		}
	}()
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
	if slot.spec.Fallback == FallbackNotFound {
		return fillSlotResult{notFound: true}
	}
	return fillSlotResult{html: ""}
}

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

func (a *App) resolveSlotsConcurrent(ctx context.Context, path string, params map[string]string, slots []fillSlot, guards regionGuards) []fillSlotResult {
	results := make([]fillSlotResult, len(slots))
	ids := render.NewRegionBatch(ctx, len(slots))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentFills)
	for i, slot := range slots {
		wg.Add(1)
		go func(i int, slot fillSlot) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if slot.spec.Deferred {
				results[i] = a.resolveDeferredSlot(ctx, path, params, slot, guards)
				return
			}
			results[i] = a.resolveFillSlot(ids.Context(i), path, params, slot, guards)
		}(i, slot)
	}
	wg.Wait()
	for i := range results {
		ids.ReserveRendered(i, results[i].html)
	}
	for i := range results {
		results[i].html = ids.Settle(i)(results[i].html)
	}
	return results
}

func (a *App) resolveDeferredSlot(ctx context.Context, path string, params map[string]string, slot fillSlot, guards regionGuards) fillSlotResult {
	ids := render.NewRegionBatch(outletIDs(ctx, slot.addr), 1)
	res := a.resolveFillSlot(ids.Context(0), path, params, slot, guards)
	ids.ReserveRendered(0, res.html)
	res.html = ids.Settle(0)(res.html)
	return res
}

func outletIDs(ctx context.Context, addr string) context.Context {
	return render.WithIDNamespaceKind(ctx, 'l', idNamespace(addr))
}

func idNamespace(addr string) string {
	alnum := func(s string) bool {
		for i := 0; i < len(s); i++ {
			c := s[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
				return false
			}
		}
		return s != ""
	}
	if rest, ok := strings.CutPrefix(addr, "l:"); ok {
		if layout, outlet, ok := strings.Cut(rest, "#"); ok && alnum(layout) && alnum(outlet) {
			return layout + "-" + outlet
		}
	}
	return addr
}

func withPageIDs(ctx context.Context, fn func(context.Context) (RenderResult, error)) (RenderResult, error) {
	ids := render.NewRegionBatch(ctx, 1)
	res, err := fn(ids.Context(0))
	ids.ReserveRendered(0, res.HTML)
	for _, f := range res.Fills {
		ids.ReserveRendered(0, f.HTML)
	}
	settle := ids.Settle(0)
	res.HTML = settle(res.HTML)
	for i := range res.Fills {
		res.Fills[i].HTML = settle(res.Fills[i].HTML)
	}
	return res, err
}

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

func containedRenderError(ctx context.Context, eb component.ErrorBoundary, err error) (out render.HTML, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			renderdiag.Report(ctx, "fill fallback", eb, r)
			out, ok = "", false
		}
	}()
	return eb.RenderError(err), true
}

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
