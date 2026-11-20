package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
)

// LayoutFunc builds one layout layer's body per render.
type LayoutFunc func(ctx context.Context, l *LayoutTree) render.HTML

// LayoutSpec declares a tree layout's outlets and route areas.
type LayoutSpec struct {
	// Primary configures the layer's PRIMARY slot (the swap target): what it shows while a swapping
	// navigation is in flight (Loading, and its view transition (Transition, .
	Primary PrimaryConfig
	// Outlets are the layer's non-primary outlets, as typed handles.
	Outlets []*Outlet
	// Areas are route areas: server-re-rendered on every navigation the layer survives, addressed
	// "<layer key>~<name>".
	Areas []AreaSpec
	// Style is the layout's owned style (a generated handle from a <name>.style.css file).
	Style OwnedStyle
}

// PrimaryConfig holds the primary slot's loading content and view transition.
type PrimaryConfig struct {
	// Loading, when set, declares what the layer's PRIMARY slot (the swap target) shows while a
	// navigation that will swap it is in flight..
	Loading *Loading
	// Transition is the primary cell's view transition; the zero value transitions nothing..
	Transition Transition
	// Transitions is the per-request SET TransitionFor names the entry this render picks.
	Transitions map[string]Transition
	// TransitionFor picks this render's entry of Transitions (empty or unknown names pick nothing).
	TransitionFor func(ctx context.Context) string
}

type OutletSpec struct {
	// Name identifies the outlet inside its layout (letters, digits, '-', '_').
	Name string
	// Default is a fill candidate of last resort: when neither the screen nor any group fills the
	// outlet, Default renders.
	Default component.Component
	// Fallback decides the unfilled rendering when Default is nil.
	Fallback OutletFallback
	// Transition (, is this outlet cell's view transition.
	Transition Transition
	// Deferred moves this outlet's fill off the page request: a client navigation that carries
	// X-Gofastr-Defer skips this outlet's loaders and renders its Loading content in place, and the
	// fill arrives as its own request (X-Gofastr-Part, one per deferred address — the route manifest
	// lists them).
	Deferred bool
	// Policy is the outlet's region guard it runs in the policy phase, before any Load, beside the
	// guards of the fills targeting this outlet.
	Policy Policy
	// Loading declares what the outlet shows while a navigation that will change it is in flight.
	Loading *Loading
	// Transitions is the per-request SET, the outlet twin of PrimaryConfig.Transitions; TransitionFor
	// picks the entry this render uses.
	Transitions map[string]Transition
	// TransitionFor picks this render's entry of Transitions.
	TransitionFor func(ctx context.Context) string
}

// AreaSpec declares one route area (LayoutSpec.Areas): its name and, optionally, its view transition ().
type AreaSpec struct {
	// Contents keeps the area addressable for swaps without inserting a layout box.
	Contents bool
	// Name identifies the area inside its layout (letters, digits, '-', '_'); RouteArea addresses it
	// "<layer key>~<name>".
	Name string
	// Transition is the area cell's view transition; the cell renders data-cui-vt="<name>" (the
	// author's raw Name, or a generated vt-<layout>-<area> one) and Layout.TransitionCSS generates the
	// rules.
	Transition Transition
	// Policy is the area's region guard it runs in the policy phase, before any Load.
	Policy Policy
	// Loading declares what the area shows while a navigation that will change it is in flight: the
	// server renders an inert <template data-cui-loading="<addr>"> beside the area cell, and the
	// loading module parks and restores it exactly like an outlet's (2026-09-26,
	// DESIGN-layout-outlets.md Open).
	Loading *Loading
	// Inline renders the area's cell as a span instead of a div, so the area can sit inside phrasing
	// content: a row count inside a nav link, a figure inside a heading.
	Inline bool
}

// OutletFallback is what an outlet shows when no candidate fills it.
type OutletFallback int

const (
	// FallbackNothing renders an empty outlet.
	FallbackNothing OutletFallback = iota
	// FallbackDefault renders the outlet's Default component.
	FallbackDefault
	// FallbackNotFound makes the render outcome not-found: the route registers, but a request where no
	// candidate fills the outlet answers 404 with the not-found page through the root layout (Decided
	// 5).
	FallbackNotFound
)

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

var vtWidthRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?(px|rem|em)$`)

func validVTWidth(w string) bool { return vtWidthRe.MatchString(w) }

// NewLayout creates a layout whose body is built by build on every render (see LayoutSpec).
func NewLayout(name string, spec LayoutSpec, build LayoutFunc) *Layout {
	if !validOutletName(name) {
		panic(fmt.Sprintf("app: layout name %q is invalid (allowed: letters, digits, '-', '_')", name))
	}
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
	seenAreas := make(map[string]bool, len(spec.Areas))
	for _, a := range spec.Areas {
		if !validOutletName(a.Name) {
			panic(fmt.Sprintf("app: layout %q area name %q is invalid (allowed: letters, digits, '-', '_')", name, a.Name))
		}
		if seenAreas[a.Name] {
			panic(fmt.Sprintf("app: layout %q declares area %q twice: an address must name one region", name, a.Name))
		}
		seenAreas[a.Name] = true
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
	l.vtSheet = registerTransitionSheet(l.TransitionCSS())
	return l
}

func (l *Layout) isTree() bool {
	return l != nil && l.build != nil
}

// LayoutTree addresses one tree-layout layer's placement points during a build.
type LayoutTree struct {
	ctx        context.Context
	layer      int // chain index of this layer
	key        string
	layoutName string
	layout     *Layout
	spec       *LayoutSpec
	fills      *fillSet
	primary    render.HTML
	guards     regionGuards
	collect    bool
	outermost  bool
}

func (t *LayoutTree) vtNameAttr(tr Transition, slot string) (string, bool) {
	if tr.isZero() {
		return "", false
	}
	layoutName := ""
	if t.spec != nil {
		layoutName = t.layoutName
	}
	return tr.vtName(layoutName, slot), true
}

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

// VTRegion returns the marker attributes for the narrow half of the primary transition.
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

// Primary renders the layer's primary outlet: today's content cell (data-cui-layout-slot="<layer
// key>"), the runtime's swap target.
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
	var ld *Loading
	if t.spec != nil {
		ld = t.spec.Primary.Loading
	}
	return render.Join(cell, loadingTemplate(t.ctx, t.key, ld))
}

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

func (t *LayoutTree) primaryPickName() string {
	if t.spec == nil || len(t.spec.Primary.Transitions) == 0 {
		return ""
	}
	pick, _ := keyedPick(t.ctx, t.spec.Primary.TransitionFor, t.spec.Primary.Transitions)
	return pick
}

// Place renders the non-primary outlet o, addressed data-cui-outlet="<layer key>#<name>", carrying
// the resolved fill.
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

// RouteArea renders a declared route area, addressed data-cui-area="<layer key>~<name>".
func (t *LayoutTree) RouteArea(name string, fn func(ctx context.Context, m Match) render.HTML) render.HTML {
	addr := t.key + "~" + name
	var area render.HTML
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
	contents := false
	if t.spec != nil {
		for _, a := range t.spec.Areas {
			if a.Name != name {
				continue
			}
			for k, v := range t.vtCellAttrs(a.Transition, a.Name) {
				attrs[k] = v
			}
			ld, inline = a.Loading, a.Inline
			contents = a.Contents
			break
		}
	}
	cell := html.Div(html.DivConfig{ExtraAttrs: attrs}, area)
	if inline {
		cell = html.Span(html.TextConfig{ExtraAttrs: attrs}, area)
	}
	if contents {
		cell = layoutAreaStyle.WrapHTML(cell)
	}
	return render.Join(cell, loadingTemplate(t.ctx, addr, ld))
}

var layoutAreaStyle = registry.RegisterStyle("cui-layout-area", func(_ style.Theme) string {
	return `[data-cui-comp="cui-layout-area"] { display: contents; }`
})

func containedArea(ctx context.Context, fn func(ctx context.Context, m Match) render.HTML, m Match) (out render.HTML) {
	defer func() {
		if r := recover(); r != nil {
			renderdiag.Report(ctx, "layout area", fn, r)
			out = ""
		}
	}()
	return fn(ctx, m)
}

func containedBuild(ctx context.Context, build LayoutFunc, t *LayoutTree) (out render.HTML) {
	defer func() {
		if r := recover(); r != nil {
			renderdiag.Report(ctx, "layout build", build, r)
			out = ""
		}
	}()
	return build(ctx, t)
}

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
	if l.vtSheet != "" {
		attrs["data-cui-comp"] = l.vtSheet
	}
	if i == from {
		for k, v := range docShellAttrs(ctx) {
			attrs[k] = v
		}
	}
	return html.Div(html.DivConfig{Class: "layout-" + l.Name, ExtraAttrs: attrs}, body), nil
}

func checkBuildInventory(l *Layout, key string, i int, body, primary render.HTML) error {
	if l.spec == nil {
		return nil
	}
	count := func(needle string) int {
		return strings.Count(string(body), needle) - strings.Count(string(primary), needle)
	}
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
