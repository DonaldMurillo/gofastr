package app

// The layout primitive: a layout written as a build function over a
// LayoutTree (docs/DESIGN-layout-outlets.md, "The primitive", "API",
// "Render algorithm"). The build composes the layer's body — static
// chrome plus the placements below — instead of a fixed template, so a
// layout's look is the look of the components it is made of.
import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
)

// LayoutFunc builds one layout layer's body per render. It receives the
// request context and a LayoutTree addressing the layer's primary
// outlet, declared outlets, and route areas; everything else in the
// returned HTML is static layout chrome.
type LayoutFunc func(ctx context.Context, l *LayoutTree) render.HTML

// LayoutSpec declares a tree layout's outlets and route areas. The
// primary outlet is implicit (LayoutTree.Primary); Outlets holds the
// layer's declared outlet handles (NewOutlet) — NewLayout claims
// their ownership, so a handle cannot serve two layouts.
type LayoutSpec struct {
	// Primary configures the layer's PRIMARY slot (the swap target):
	// what it shows while a swapping navigation is in flight (Loading,
	// and its view transition (Transition,
	// . The zero value does neither.
	Primary PrimaryConfig
	// Outlets are the layer's non-primary outlets, as typed handles.
	// Each renders a data-cui-outlet cell addressed "<layer
	// key>#<name>"; what it shows when no fill targets it is the
	// handle's fallback.
	Outlets []*Outlet
	// Areas are route areas: server-re-rendered on every navigation the
	// layer survives, addressed "<layer key>~<name>". Each may declare
	// its own view transition (AreaSpec.Transition) and its own
	// loading content (AreaSpec.Loading).
	Areas []AreaSpec
	// Style is the layout's owned style (a generated handle from a
	// <name>.style.css file). The layout root carries
	// data-cui-scope="<name>", so the style covers the build output
	// and stops at the root of any nested owner (a styled screen or
	// component). Nil means no style. The app style panics here: pass
	// it to App.WithStyle.
	Style OwnedStyle
}

// PrimaryConfig holds the primary slot's spike declarations: Loading
// shows loading content in the slot while
// a navigation that will swap it is in flight; Transition
// is the cell's view transition, and the
// name must be knowable at app-build time so TransitionCSS can
// generate the rules, which is why it lives on the spec, not on the
// build fn's Primary() call.
type PrimaryConfig struct {
	// Loading, when set, declares what the layer's PRIMARY slot (the
	// swap target) shows while a navigation that will swap it is in
	// flight..
	Loading *Loading
	// Transition is the primary cell's view transition; the zero value
	// transitions nothing..
	Transition Transition
	// Transitions is the per-request SET
	// TransitionFor names the entry this render picks. Setting it (or
	// TransitionFor) beside the static Transition is a mount error;
	// the names forward/back/reload are refused (they are the
	// direction types). The stylesheet covers every entry, generated
	// once in sorted key order.
	Transitions map[string]Transition
	// TransitionFor picks this render's entry of Transitions (empty or
	// unknown names pick nothing). It may read resolvers and the match.
	TransitionFor func(ctx context.Context) string
}

type OutletSpec struct {
	// Name identifies the outlet inside its layout (letters, digits,
	// '-', '_').
	Name string
	// Default is a fill candidate of last resort: when neither the
	// screen nor any group fills the outlet, Default renders.
	Default component.Component
	// Fallback decides the unfilled rendering when Default is nil.
	// FallbackNothing (the default) renders nothing. FallbackDefault
	// only means something together with a Default component.
	// FallbackNotFound makes the whole render answer 404: the route
	// registers, the request is the not-found page through the root
	// layout, the primary becomes the app's not-found body, and every
	// other outlet renders its Default or nothing (Decided 5). Refused
	// beside a Default (NewLayout panics): the two fallbacks
	// disagree about what an unfilled outlet is.
	Fallback OutletFallback
	// Transition (, is this outlet cell's view
	// transition. The cell renders data-cui-vt="<name>" (the author's
	// raw Name, or a generated one); the runtime mirrors it to a CSSOM
	// view-transition-name before a navigation's snapshots, and
	// Layout.TransitionCSS generates the animation rules. The zero
	// value transitions nothing. A raw Name must be a CSS custom-ident
	// (NewLayout panics on a bad one) and unique per document: two
	// cells sharing a name make the browser skip the whole transition
	// (silent).
	Transition Transition
	// Deferred moves this outlet's fill off the
	// page request: a client navigation that carries X-Gofastr-Defer
	// skips this outlet's loaders and renders its Loading content in
	// place, and the fill arrives as its own request (X-Gofastr-Part,
	// one per deferred address — the route manifest lists them).
	// Deferral belongs to the OUTLET, never to one fill: a candidate
	// can decline at request time (ErrNoFill), so a per-fill flag
	// cannot tell the manifest in advance whether the region will be
	// deferred. First loads and static export ignore it and render the
	// fill inline: nothing depends on JavaScript.
	Deferred bool
	// Policy is the outlet's region guard it
	// runs in the policy phase, before any Load, beside the guards of
	// the fills targeting this outlet. A Redirect moves the whole
	// page; RenderAlt replaces the resolved fill; Block renders the
	// outlet's fallback.
	Policy Policy
	// Loading declares what the outlet shows while a navigation that
	// will change it is in flight. nil (or nil Show) keeps the dim
	// only..
	Loading *Loading
	// Transitions is the per-request SET, the
	// outlet twin of PrimaryConfig.Transitions; TransitionFor picks
	// the entry this render uses. Static Transition plus either form
	// is a mount error, and the direction names are refused.
	Transitions map[string]Transition
	// TransitionFor picks this render's entry of Transitions.
	TransitionFor func(ctx context.Context) string
}

// AreaSpec declares one route area (LayoutSpec.Areas): its name and,
// optionally, its view transition (). The area CELL (the element
// carrying data-cui-area) persists across navigations — fills replace
// its innerHTML — so the transition rides the cell exactly like a
// placed outlet's does.
type AreaSpec struct {
	// Name identifies the area inside its layout (letters, digits,
	// '-', '_'); RouteArea addresses it "<layer key>~<name>".
	Name string
	// Transition is the area cell's view transition; the cell renders
	// data-cui-vt="<name>" (the author's raw Name, or a generated
	// vt-<layout>-<area> one) and Layout.TransitionCSS generates the
	// rules. Instant suits a region that keeps most of its pixels
	// (a breadcrumb trail whose root never changes); FadeThrough suits
	// text that changes whole and must not ghost over itself; the zero
	// value transitions nothing (the area rides the root crossfade).
	Transition Transition
	// Policy is the area's region guard it
	// runs in the policy phase, before any Load. A Redirect moves the
	// whole page; RenderAlt renders the alt component instead of the
	// area's fn; Block renders the area empty.
	Policy Policy
	// Loading declares what the area shows while a navigation that
	// will change it is in flight: the server renders an inert
	// <template data-cui-loading="<addr>"> beside the area cell, and
	// the loading module parks and restores it exactly like an
	// outlet's (2026-09-26, DESIGN-layout-outlets.md Open). nil (or
	// nil Show) keeps the busy dim only, as before.
	Loading *Loading
	// Inline renders the area's cell as a span instead of a div, so
	// the area can sit inside phrasing content: a row count inside a
	// nav link, a figure inside a heading.
	Inline bool
}

// OutletFallback is what an outlet shows when no candidate fills it.
type OutletFallback int

const (
	// FallbackNothing renders an empty outlet.
	FallbackNothing OutletFallback = iota
	// FallbackDefault renders the outlet's Default component.
	FallbackDefault
	// FallbackNotFound makes the render outcome not-found: the route
	// registers, but a request where no candidate fills the outlet
	// answers 404 with the not-found page through the root layout
	// (Decided 5). Only a clean decline (every candidate returned
	// ErrNoFill) decides it; a contained fill failure never does.
	FallbackNotFound
)

// validVTName reports whether name is a usable CSS custom-ident for a
// view-transition-name (): letters, digits, '-' and '_', not
// starting with a digit, and no CSS-wide keyword (the 'none' preset is
// a CSS keyword, an author wanting no animation styles it in CSS).
func validVTName(name string) bool {
	switch name {
	case "", "none", "initial", "inherit", "unset", "revert":
		return false
	}
	if c := name[0]; c >= '0' && c <= '9' {
		return false
	}
	for _, c := range name[1:] {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// validVTWidth reports whether w is a plain CSS length usable as a
// media-query breakpoint (Transition.Narrow): number plus px/rem/em
// unit. Anything else would silently never match the query.
var vtWidthRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?(px|rem|em)$`)

func validVTWidth(w string) bool { return vtWidthRe.MatchString(w) }

// NewLayout creates a layout whose body is built by build on every
// render (see LayoutSpec). The layout keeps the name, layer-key, and
// wrapper-marker duty of a template layout; its Header/Sidebar/Footer
// fields are ignored.
//
// Mount validation (DESIGN "Validation at mount"): outlet names are
// unique per layout, an outlet handle belongs to exactly one layout
// (a handle bound to two layouts panics here, naming both), and a
// FallbackNotFound outlet cannot also declare a Default — the
// outlet's unfilled rendering would be ambiguous (the Default
// renders, so the 404 can never fire; or the 404 fires and the
// Default is dead weight).
func NewLayout(name string, spec LayoutSpec, build LayoutFunc) *Layout {
	if spec.Style != nil {
		scopedSheet(fmt.Sprintf("layout %q Style", name), spec.Style)
	}
	bad := func(slot, n string) {
		panic(fmt.Sprintf("app: layout %q %s: Transition.Name %q is not a valid view-transition name (CSS custom-ident: letters, digits, '-', '_', not starting with a digit, no CSS-wide keyword)", name, slot, n))
	}
	badW := func(slot, w string) {
		panic(fmt.Sprintf("app: layout %q %s: Transition.Narrow %q is not a plain CSS length (e.g. \"920px\", \"48rem\")", name, slot, w))
	}
	validateTransitionSet := func(slot string, tr Transition, set map[string]Transition, tf func(context.Context) string) {
		if (len(set) > 0 || tf != nil) && !tr.isZero() {
			panic(fmt.Sprintf("app: layout %q %s: Transition and Transitions/TransitionFor are both set; declare one form", name, slot))
		}
		if tf != nil && len(set) == 0 {
			panic(fmt.Sprintf("app: layout %q %s: TransitionFor with an empty Transitions set has nothing to pick", name, slot))
		}
		if tr.Name != "" && !validVTName(tr.Name) {
			bad(slot, tr.Name)
		}
		if w := tr.Narrow; w != "" && !validVTWidth(w) {
			badW(slot, w)
		}
		for _, k := range sortedVTKeys(set) {
			if !validVTName(k) {
				bad(slot, k)
			}
			if directionVTNames[k] {
				panic(fmt.Sprintf("app: layout %q %s: transition name %q is a direction type (forward/back/reload) and cannot be reused", name, slot, k))
			}
			e := set[k]
			if e.Name != "" && !validVTName(e.Name) {
				bad(slot+" "+k, e.Name)
			}
			if w := e.Narrow; w != "" && !validVTWidth(w) {
				badW(slot+" "+k, w)
			}
		}
	}
	validateTransitionSet("primary", spec.Primary.Transition, spec.Primary.Transitions, spec.Primary.TransitionFor)
	seen := make(map[string]bool, len(spec.Outlets))
	for _, o := range spec.Outlets {
		if o == nil {
			panic(fmt.Sprintf("app: layout %q declares a nil outlet handle", name))
		}
		if seen[o.Name()] {
			panic(fmt.Sprintf("app: layout %q declares outlet %q twice: an address must name one region", name, o.Name()))
		}
		seen[o.Name()] = true
		if o.owner != nil {
			panic(fmt.Sprintf("app: layout %q cannot declare outlet %q: the handle is already bound to layout %q — an outlet belongs to exactly one layout (declare a second handle)", name, o.Name(), o.owner.Name))
		}
		if o.Fallback == FallbackNotFound && o.Default != nil {
			panic(fmt.Sprintf("app: layout %q outlet %q: FallbackNotFound cannot be declared beside a Default — an unfilled outlet is either the Default's content or the route's 404, never both", name, o.Name()))
		}
		validateTransitionSet("outlet "+o.Name(), o.Transition, o.Transitions, o.TransitionFor)
	}
	for _, a := range spec.Areas {
		if a.Transition.Name != "" && !validVTName(a.Transition.Name) {
			bad("area "+a.Name, a.Transition.Name)
		}
		if w := a.Transition.Narrow; w != "" && !validVTWidth(w) {
			badW("area "+a.Name, w)
		}
	}
	l := &Layout{Name: name, spec: &spec, build: build}
	for _, o := range spec.Outlets {
		o.owner = l
	}
	return l
}

// isTree reports whether the layout renders through a build function.
func (l *Layout) isTree() bool {
	return l != nil && l.build != nil
}

// LayoutTree addresses one tree-layout layer's placement points during
// a build. Everything it renders is collectable: on a subtree partial
// the kept layers' builds run in collect mode, where Primary and Place
// render empty (their markup stays in the client's DOM) and RouteArea
// still runs so the area's fresh HTML travels with the response.
type LayoutTree struct {
	ctx   context.Context
	layer int // chain index of this layer
	key   string
	// layoutName is the owning layout's NAME (not the layer key); the
	// view-transition names TransitionCSS generates key off it.
	layoutName string
	// layout is the layer's owning layout; Place checks handle
	// ownership against it.
	layout  *Layout
	spec    *LayoutSpec
	fills   *fillSet
	primary render.HTML
	// guards carries the region decisions the policy phase recorded
	// RouteArea consults the area's own.
	guards regionGuards
	// collect marks collect mode: markup is discarded, fills recorded.
	collect bool
	// outermost marks the page's layer 0, which owns <main>.
	outermost bool
}

// vtNameAttr returns the data-cui-vt attribute value for a spec
// transition, or ok=false when it configures nothing. Names are
// validated at NewLayout; generated ones are safe by construction.
func (t *LayoutTree) vtNameAttr(tr Transition, slot string) (string, bool) {
	if tr.isZero() {
		return "", false
	}
	layoutName := ""
	if t.spec != nil {
		// The layout's NAME (not layer key) is what TransitionCSS
		// generated the rules with.
		layoutName = t.layoutName
	}
	return tr.vtName(layoutName, slot), true
}

// vtCellAttrs returns the vt marker attributes a placed cell (primary,
// outlet, area) carries for a spec transition: the name, plus — when
// the transition declares Narrow — the media condition under which the
// CELL owns it. Below the breakpoint the name belongs to the region
// instead (VTRegion); the runtime mirror honours the same conditions.
func (t *LayoutTree) vtCellAttrs(tr Transition, slot string) html.Attrs {
	name, ok := t.vtNameAttr(tr, slot)
	if !ok {
		return nil
	}
	attrs := html.Attrs{"data-cui-vt": name}
	if tr.Narrow != "" {
		attrs["data-cui-vt-when"] = "(width >= " + tr.Narrow + ")"
	}
	return attrs
}

// VTRegion returns the marker attributes for the NARROW half of the
// layer's primary transition (PrimaryConfig.Transition.Narrow): spread
// them onto the element the build renders around the layer's whole
// moving panes — e.g. the div holding BOTH the master list and the
// detail slot. Below the breakpoint the two stack into one pane, so
// the move must read as that pane transitioning; a detail-only
// snapshot there would morph its group geometry across the list. nil
// when the primary declares no Narrow.
func (t *LayoutTree) VTRegion() html.Attrs {
	tr := t.primaryTransition()
	if tr.isZero() || tr.Narrow == "" {
		return nil
	}
	name, _ := t.vtNameAttr(tr, keyedSlotName("primary", t.primaryPickName()))
	return html.Attrs{
		"data-cui-vt":      name,
		"data-cui-vt-when": "(width < " + tr.Narrow + ")",
	}
}

// Primary renders the layer's primary outlet: today's content cell
// (data-cui-layout-slot="<layer key>"), the runtime's swap target.
// Layer 0 renders the page's single <main id="main-content">; inner
// layers render the .layout-content div. A spec-declared Transition
// () marks the cell with its view-transition name; a spec
// Loading () carries its inert template as a SIBLING of the cell.
func (t *LayoutTree) Primary() render.HTML {
	if t.collect {
		return ""
	}
	slotAttrs := html.Attrs{}
	if t.key != "" {
		slotAttrs["data-cui-layout-slot"] = t.key
	}
	for k, v := range t.vtCellAttrs(t.primaryTransition(), keyedSlotName("primary", t.primaryPickName())) {
		slotAttrs[k] = v
	}
	var cell render.HTML
	if t.outermost {
		cell = html.Main(html.MainConfig{ExtraAttrs: slotAttrs}, t.primary)
	} else {
		if len(slotAttrs) > 0 {
			slotAttrs["tabindex"] = "-1"
		}
		cell = html.Div(html.DivConfig{Class: "layout-content", ExtraAttrs: slotAttrs}, t.primary)
	}
	// the primary's loading template rides
	// beside the slot cell, addressed by the bare layer key (the same
	// address a partial's primary payload carries).
	var ld *Loading
	if t.spec != nil {
		ld = t.spec.Primary.Loading
	}
	return render.Join(cell, loadingTemplate(t.ctx, t.key, ld))
}

// primaryTransition returns the primary's EFFECTIVE transition: the
// per-request pick when the spec declares a set, else the static one.
func (t *LayoutTree) primaryTransition() Transition {
	if t.spec == nil {
		return Transition{}
	}
	if len(t.spec.Primary.Transitions) > 0 {
		_, tr := keyedPick(t.ctx, t.spec.Primary.TransitionFor, t.spec.Primary.Transitions)
		return tr
	}
	return t.spec.Primary.Transition
}

// primaryPickName is the pick the primary's set resolved to ("" for a
// static or unset transition), for the generated name's slot suffix.
func (t *LayoutTree) primaryPickName() string {
	if t.spec == nil || len(t.spec.Primary.Transitions) == 0 {
		return ""
	}
	pick, _ := keyedPick(t.ctx, t.spec.Primary.TransitionFor, t.spec.Primary.Transitions)
	return pick
}

// Place renders the non-primary outlet o, addressed
// data-cui-outlet="<layer key>#<name>", carrying the resolved fill. A
// spec-declared Transition () marks the cell with its
// view-transition name; a spec-declared Loading () carries its
// inert template as a SIBLING of the cell (fill application replaces
// the cell's innerHTML; a child template would not survive the first
// navigation). A handle this layer's layout does not own renders
// nothing: the per-build inventory then names the outlet that never
// landed.
func (t *LayoutTree) Place(o *Outlet) render.HTML {
	if t.collect || o == nil {
		return ""
	}
	if t.layout != nil && o.owner != t.layout {
		return ""
	}
	addr := t.key + "#" + o.Name()
	attrs := html.Attrs{"data-cui-outlet": addr}
	for k, v := range t.vtCellAttrs(o.Transition, o.Name()) {
		attrs[k] = v
	}
	var inner render.HTML
	if t.fills != nil {
		if tf, ok := t.fills.get(addr); ok {
			inner = tf.html
		}
	}
	return render.Join(
		html.Div(html.DivConfig{ExtraAttrs: attrs}, inner),
		loadingTemplate(t.ctx, addr, o.Loading),
	)
}

// RouteArea renders a declared route area, addressed
// data-cui-area="<layer key>~<name>". fn runs on EVERY render the layer
// is part of, kept or not: on a partial a kept layer's area fn runs in
// collect mode and its HTML travels as a fill instead of markup. A
// spec-declared AreaSpec.Transition () marks the cell with its
// view-transition name — the cell itself persists across navigations
// (fills replace its innerHTML), so the name rides the box the two
// trails render into, not the root crossfade around them. A
// spec-declared AreaSpec.Loading carries its inert template as a
// SIBLING of the cell (same rule as an outlet's).
func (t *LayoutTree) RouteArea(name string, fn func(ctx context.Context, m Match) render.HTML) render.HTML {
	addr := t.key + "~" + name
	var area render.HTML
	// The area's recorded region decision replaces its fn: Block
	// renders the region's fallback — nothing,
	// an area has no Default — and RenderAlt the alt component.
	if d, ok := t.guards[addr]; ok {
		switch d.kind {
		case DecisionBlock:
			area = ""
		case DecisionRenderAlt:
			if d.factory != nil {
				if html, err := component.SafeRenderCtx(t.ctx, d.factory()); err == nil {
					area = html
				}
			}
		default:
			m, _ := MatchFromContext(t.ctx)
			area = containedArea(t.ctx, fn, m)
		}
	} else {
		m, _ := MatchFromContext(t.ctx)
		area = containedArea(t.ctx, fn, m)
	}
	if t.fills != nil {
		t.fills.set(addr, treeFill{layer: t.layer, html: area})
	}
	if t.collect {
		return ""
	}
	attrs := html.Attrs{"data-cui-area": addr}
	var ld *Loading
	inline := false
	if t.spec != nil {
		for _, a := range t.spec.Areas {
			if a.Name != name {
				continue
			}
			for k, v := range t.vtCellAttrs(a.Transition, a.Name) {
				attrs[k] = v
			}
			ld, inline = a.Loading, a.Inline
			break
		}
	}
	// The area's loading template rides BESIDE the cell for the same
	// reason an outlet's does: fill application replaces the cell's
	// innerHTML, so a child template would not survive the first
	// navigation it exists for (2026-09-26, "Areas take loading
	// content").
	cell := html.Div(html.DivConfig{ExtraAttrs: attrs}, area)
	if inline {
		cell = html.Span(html.TextConfig{ExtraAttrs: attrs}, area)
	}
	return render.Join(cell, loadingTemplate(t.ctx, addr, ld))
}

// containedArea runs an area fn under the same panic-to-error
// containment a component render gets: a panicking area renders empty
// instead of killing the page.
func containedArea(ctx context.Context, fn func(ctx context.Context, m Match) render.HTML, m Match) (out render.HTML) {
	defer func() {
		if r := recover(); r != nil {
			renderdiag.Report(ctx, "layout area", fn, r)
			out = ""
		}
	}()
	return fn(ctx, m)
}

// containedBuild runs a layout build under panic-to-error containment.
func containedBuild(ctx context.Context, build LayoutFunc, t *LayoutTree) (out render.HTML) {
	defer func() {
		if r := recover(); r != nil {
			renderdiag.Report(ctx, "layout build", build, r)
			out = ""
		}
	}()
	return build(ctx, t)
}

// wrapTreeLayer renders one tree-layout layer of a chain: it runs the
// build around the content accumulated so far and emits the layer's
// wrapper (data-cui-layout / data-cui-layout-key, doc markers on the
// frame root), so the runtime's chain walk is identical for template
// and tree layers.
func (l *Layout) wrapTreeLayer(ctx context.Context, chain []LayoutLayer, i, from int, content render.HTML, fills *fillSet, guards regionGuards) (render.HTML, error) {
	key := chain[i].Key()
	t := &LayoutTree{
		ctx:        ctx,
		layer:      i,
		key:        key,
		layoutName: l.Name,
		layout:     l,
		spec:       l.spec,
		fills:      fills,
		primary:    content,
		outermost:  i == 0,
		guards:     guards,
	}
	body := containedBuild(ctx, l.build, t)
	if err := checkBuildInventory(l, key, i, body, content); err != nil {
		return "", err
	}
	attrs := html.Attrs{}
	if l.Name != "" {
		attrs["data-cui-layout"] = l.Name
	}
	if key != "" {
		attrs["data-cui-layout-key"] = key
	}
	if l.spec != nil && l.spec.Style != nil {
		attrs["data-cui-scope"] = l.spec.Style.OwnedSheet().Name()
	}
	if i == from {
		for k, v := range docShellAttrs(ctx) {
			attrs[k] = v
		}
	}
	return html.Div(html.DivConfig{Class: "layout-" + l.Name, ExtraAttrs: attrs}, body), nil
}

// checkBuildInventory is the per-render inventory (DESIGN "Inventory
// per render"): a layer's build must place its primary and every
// outlet it owns exactly once, name each route area once, and emit no
// <main> of its own. The counts are taken on MARKERS in the build's
// OUTPUT, not on Place/Primary calls — an outlet's rendered markup
// copied by a component into a second place duplicates the marker and
// is caught here, where a call count would miss it. The inner content
// (t.primary) is subtracted by counting only this layer's own
// addresses: every deeper layer carries a different key, so its
// markers never collide with the expectations.
func checkBuildInventory(l *Layout, key string, i int, body, primary render.HTML) error {
	if l.spec == nil {
		return nil
	}
	count := func(needle string) int {
		return strings.Count(string(body), needle) - strings.Count(string(primary), needle)
	}
	// The markers are attribute VALUES, written through the attribute
	// escaper: a {param} group key holding ' & " or < lands in the
	// output as entities, so the needles must carry the same spelling
	// or a valid /projects/o'neil page fails its own inventory.
	key = render.Escape(key)
	wantMain := 0
	if i == 0 {
		wantMain = 1 // Primary's <main id="main-content">
	}
	if n := count("<main"); n != wantMain {
		return fmt.Errorf("app: layout %q (layer key %q): the build emitted %d <main> elements, want %d — only the outermost layer's Primary owns the document's <main>", l.Name, key, n, wantMain)
	}
	if key != "" {
		if n := count(`data-cui-layout-slot="` + key + `"`); n != 1 {
			return fmt.Errorf("app: layout %q (layer key %q): the build placed its primary slot %d times, want exactly 1", l.Name, key, n)
		}
	}
	for _, o := range l.spec.Outlets {
		if n := count(`data-cui-outlet="` + key + "#" + render.Escape(o.Name()) + `"`); n != 1 {
			return fmt.Errorf("app: layout %q (layer key %q): the build placed outlet %q %d times, want exactly 1 — a duplicated outlet marker makes the region's fill address ambiguous", l.Name, key, o.Name(), n)
		}
	}
	for _, a := range l.spec.Areas {
		if n := count(`data-cui-area="` + key + "~" + render.Escape(a.Name) + `"`); n != 1 {
			return fmt.Errorf("app: layout %q (layer key %q): the build placed route area %q %d times, want exactly 1", l.Name, key, a.Name, n)
		}
	}
	return nil
}

// collectTreeLayer runs a KEPT layer's build in collect mode on a
// subtree partial: the markup is discarded, but every RouteArea fn runs
// with the live context and records its fill, so the response carries
// the kept layers' fresh areas. Outlet fills were already resolved by
// resolveFills; Outlet itself renders empty here.
func (l *Layout) collectTreeLayer(ctx context.Context, chain []LayoutLayer, i int, fills *fillSet, guards regionGuards) {
	t := &LayoutTree{
		ctx:        ctx,
		layer:      i,
		key:        chain[i].Key(),
		layoutName: l.Name,
		layout:     l,
		spec:       l.spec,
		fills:      fills,
		guards:     guards,
		collect:    true,
	}
	_ = containedBuild(ctx, l.build, t)
}
