package headless

// The harness: what is asserted about EVERY registered component,
// without anyone writing a test for it.
//
// The contract file next door holds the specific promises — a field
// wires its hint to its control, a pager says which page is current.
// This file
// holds the ones that are true of everything, and it gets them from
// the specs rather than from a hand-kept list, so the sweep grows
// when the system does instead of the day someone remembers.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// drawsClass reports whether html uses class as a whole class token.
//
// strings.Contains is wrong here and was wrong quietly: the probe for
// the part "head" is a prefix of the probe for "header", so a component
// that drew a header counted as drawing a head. That made
// TestEveryDeclaredPartIsActuallyDrawn pass for a part nothing rendered,
// which is the exact failure it exists to prevent.
//
// It reads class attributes and their tokens, and nothing else: a
// probe class carried as a hook's VALUE (the multi-select tells its
// runtime which class to give the chips it builds) is not a drawn
// part.
func drawsClass(html, class string) bool {
	for _, m := range classAttr.FindAllStringSubmatch(html, -1) {
		for _, tok := range strings.Fields(m[1]) {
			if tok == class {
				return true
			}
		}
	}
	return false
}

var classAttr = regexp.MustCompile(`\sclass="([^"]*)"`)

// hasAttr reports whether html carries an attribute NAMED name: not
// the name inside a value, not the name inside text, and not a longer
// attribute that happens to end in it.
func hasAttr(html, name string) bool {
	return regexp.MustCompile(`\s` + regexp.QuoteMeta(name) + `(=|\s|/|>)`).MatchString(html)
}

// probeKit is a kit where this component and every child it composes
// resolve to the same probe class map. A kit that only dressed the component
// itself would let a fixture pass its own class map to a child and look
// correct, which is the defect these sweeps exist to catch.
func probeKit(probe Classes) Kit {
	return NewKit(probe, func(component, variant string) Classes { return probe })
}

// eachCase runs fn over every case of every spec, at the nil Classes.
func eachCase(t *testing.T, fn func(t *testing.T, sp Spec, c Case)) {
	t.Helper()
	for _, sp := range Specs() {
		for _, c := range sp.Cases(Kit{}) {
			t.Run(sp.Name+"/"+c.Name, func(t *testing.T) { fn(t, sp, c) })
		}
	}
}

// A case with no reason to exist is a case that will be rewritten to
// match whatever the code does next, which is the opposite of a test.
func TestEveryCaseSaysWhyItExists(t *testing.T) {
	eachCase(t, func(t *testing.T, sp Spec, c Case) {
		if strings.TrimSpace(c.Why) == "" {
			t.Error("no Why: a fixture nobody can justify is a fixture that will be updated to match a bug")
		}
		if c.HTML == "" {
			t.Error("renders nothing")
		}
	})
}

var emptyAria = regexp.MustCompile(`aria-[a-z]+=""`)

// The universal contract, asserted against every fixture in the
// system rather than the ones somebody remembered to add to a list.
func TestEveryCaseMeetsTheUniversalContract(t *testing.T) {
	eachCase(t, func(t *testing.T, sp Spec, c Case) {
		got := c.HTML
		if strings.Contains(string(got), "class=") {
			t.Error("renders a class at the nil Classes — structure is carrying styling")
		}
		if m := emptyAria.FindString(string(got)); m != "" {
			t.Errorf("emits %s — an aria attribute pointing at nothing", m)
		}
		if strings.Contains(string(got), `style="`) {
			t.Error(`renders a style attribute — the app serves no unsafe-inline, so it is a rule the browser drops`)
		}
		for _, b := range strings.Split(string(got), "<button")[1:] {
			if tag := b[:strings.Index(b, ">")]; !strings.Contains(tag, "type=") {
				t.Errorf("a button with no type: %q — inside a form the default is submit", tag)
			}
		}
		for id, n := range idsIn(got) {
			if n > 1 {
				t.Errorf("id %q rendered %d times — every reference to it now points at the first one", id, n)
			}
		}
	})
}

// Two renders of the same fixture must be the same bytes. Map
// iteration order is random in Go, and an attribute set built from a
// map that escapes into the output makes goldens flap and diffs lie.
func TestRenderingIsDeterministic(t *testing.T) {
	for _, sp := range Specs() {
		for i, c := range sp.Cases(Kit{}) {
			again := sp.Cases(Kit{})[i]
			if c.HTML != again.HTML {
				t.Errorf("%s/%s renders differently each time:\n  %s\n  %s", sp.Name, c.Name, c.HTML, again.HTML)
			}
		}
	}
}

// A part is a promise to a class map: name it and a stylesheet may target
// it. A part that no case renders is a class with nothing to land on,
// and the way to find out is to give every part a class of its own
// and look for it.
func TestEveryDeclaredPartIsActuallyDrawn(t *testing.T) {
	for _, sp := range Specs() {
		t.Run(sp.Name, func(t *testing.T) {
			probe := Classes{}
			for _, p := range sp.Anatomy {
				probe[p] = "probe-" + string(p)
			}
			var all strings.Builder
			for _, c := range sp.Cases(probeKit(probe)) {
				all.WriteString(string(c.HTML))
			}
			for _, p := range sp.Anatomy {
				if !drawsClass(all.String(), "probe-"+string(p)) {
					t.Errorf("part %q is declared but no case draws it", p)
				}
			}
		})
	}
}

// A hook is the contract between the markup and the runtime. One that
// no case renders is a listener bound to nothing — which is exactly
// how a behaviour is lost: the attribute is renamed here, the runtime
// keeps looking for the old one, and nothing fails.
func TestEveryDeclaredHookIsRendered(t *testing.T) {
	for _, sp := range Specs() {
		// At the nil classes AND at a class map where every part has a class,
		// because some hooks carry a class as their value — the
		// multi-select tells its runtime which class to give the chips
		// it builds — and those exist only once something is styled.
		probe := Classes{}
		for _, p := range sp.Anatomy {
			probe[p] = "probe-" + string(p)
		}
		var all strings.Builder
		for _, k := range []Kit{{}, probeKit(probe)} {
			for _, c := range sp.Cases(k) {
				all.WriteString(string(c.HTML))
			}
		}
		for _, h := range sp.Hooks {
			if !hasAttr(all.String(), h) {
				t.Errorf("%s declares hook %q and no case renders it as an attribute", sp.Name, h)
			}
		}
		// And the other direction: every data-hui-* attribute a case
		// renders is declared, so a hook cannot be published by
		// accident and bound to by a runtime nobody told.
		declared := map[string]bool{}
		for _, h := range sp.Hooks {
			declared[h] = true
		}
		for _, m := range dsAttr.FindAllStringSubmatch(all.String(), -1) {
			if !declared[m[1]] && !childHook(sp, m[1]) {
				t.Errorf("%s renders %q and does not declare it", sp.Name, m[1])
			}
		}
		// The namespace rule: a hook this system invents, whether a
		// runtime module or a class map reads it, is data-hui-*, so it cannot
		// collide with anything the platform or the framework owns. A
		// cue that restates a native state the platform already names —
		// data-invalid, data-required, data-state — stays unprefixed
		// and is not declared as a hook; it is the state, not a hook.
		for _, h := range sp.Hooks {
			if !strings.HasPrefix(h, "data-hui-") {
				t.Errorf("%s declares hook %q — runtime hooks are data-hui-* so they cannot collide with anything the platform owns", sp.Name, h)
			}
		}
	}
}

// Every id a fixture references must exist in that same fixture.
//
// The page audit catches this in the browser, and by then the
// component is on a page with a hundred others and the finding says
// which id is dangling, not which component dropped it. Here it says
// both. A component whose reference legitimately points outside
// itself — a trigger and its panel are two fragments — renders both
// halves in its case, which is also the honest fixture.
func TestEveryReferenceResolvesInsideItsFixture(t *testing.T) {
	// The space before the name is load-bearing. Without it, `for=`
	// matched the tail of `data-hui-toggle-for=` and the check passed
	// or failed for a reason it was not claiming to test. The runtime
	// hooks that DO point at an element are named here deliberately
	// instead, because a hook pointing at nothing is the same defect
	// as an aria attribute pointing at nothing — the difference is
	// only which layer notices.
	refs := regexp.MustCompile(`\s(aria-labelledby|aria-describedby|aria-controls|aria-activedescendant|` +
		`popovertarget|commandfor|for)="([^"]+)"`)
	eachCase(t, func(t *testing.T, sp Spec, c Case) {
		ids := idsIn(c.HTML)
		for _, m := range refs.FindAllStringSubmatch(string(c.HTML), -1) {
			for _, want := range strings.Fields(m[2]) {
				if ids[want] == 0 {
					t.Errorf("%s=%q points at nothing in this fixture", m[1], want)
				}
			}
		}
	})
}

// Every control a fixture renders has an accessible name.
//
// A control with no name cannot be operated by anyone who cannot see
// it, and a placeholder is not a name — which is the whole reason to
// check rather than to look. Hidden subtrees are skipped: they are
// out of the tree and out of the tab order, and they are named by the
// same markup that shows them.
func TestEveryControlHasAName(t *testing.T) {
	control := regexp.MustCompile(`(?s)<(button|a|select|textarea)\b([^>]*)>(.*?)</(?:button|a|select|textarea)>`)
	input := regexp.MustCompile(`<input\b([^>]*)>`)
	idOf := regexp.MustCompile(`\bid="([^"]+)"`)
	tags := regexp.MustCompile(`<[^>]+>`)
	eachCase(t, func(t *testing.T, sp Spec, c Case) {
		html := string(c.HTML)
		// An input is named by an aria attribute, by a <label for> that
		// points at its id, or by the <label> it sits inside; a hidden
		// input is not a control. A placeholder is not a name.
		for _, m := range input.FindAllStringSubmatchIndex(html, -1) {
			attrs := html[m[2]:m[3]]
			if strings.Contains(attrs, `type="hidden"`) || strings.Contains(attrs, `aria-hidden="true"`) {
				continue
			}
			named := strings.Contains(attrs, "aria-label=") || strings.Contains(attrs, "aria-labelledby=")
			if id := idOf.FindStringSubmatch(attrs); !named && id != nil {
				named = strings.Contains(html, ` for="`+id[1]+`"`)
			}
			if before := html[:m[0]]; !named {
				named = strings.LastIndex(before, "<label") > strings.LastIndex(before, "</label>")
			}
			if !named {
				t.Errorf("an <input> with no accessible name: %s", strings.TrimSpace(html[m[0]:m[1]]))
			}
		}
		for _, m := range control.FindAllStringSubmatch(html, -1) {
			attrs, inner := m[2], m[3]
			if m[1] == "a" && !strings.Contains(attrs, "href=") && !strings.Contains(attrs, "role=") {
				continue // not a link: an anchor used as a target
			}
			if strings.Contains(attrs, `aria-hidden="true"`) || strings.Contains(attrs, "hidden") {
				continue
			}
			named := strings.Contains(attrs, "aria-label=") ||
				strings.Contains(attrs, "aria-labelledby=") ||
				strings.Contains(attrs, "title=") ||
				strings.TrimSpace(tags.ReplaceAllString(inner, "")) != ""
			if !named {
				t.Errorf("a <%s> with no accessible name: %s", m[1], strings.TrimSpace(m[0]))
			}
		}
	})
}

// ─── parts ──────────────────────────────────────────────────────────

// Filling a slot must not cost the component its contract. The slot
// gets something hostile — a heading, an unnamed control, a duplicate
// id — and everything universal is asserted again on the result.
func TestFilledSlotsKeepTheContract(t *testing.T) {
	hostile := render.HTML(`<h4>slot</h4>`)
	for _, sp := range Specs() {
		if len(sp.Fillable) == 0 {
			continue
		}
		if sp.WithParts == nil {
			t.Errorf("%s offers %d fillable parts and no WithParts, so nothing tests them", sp.Name, len(sp.Fillable))
			continue
		}
		for _, p := range sp.Fillable {
			t.Run(sp.Name+"/"+string(p), func(t *testing.T) {
				got := sp.WithParts(nil, Parts{Slots: Slots{p: hostile}})
				if !strings.Contains(string(got), "<h4>slot</h4>") {
					t.Errorf("part %q is listed as fillable and the content never arrived:\n%s", p, got)
				}
				if strings.Contains(string(got), "class=") {
					t.Error("a filled slot brought a class attribute into the nil class map")
				}
				for id, n := range idsIn(got) {
					if n > 1 {
						t.Errorf("filling %q duplicated id %q", p, id)
					}
				}
			})
		}
	}
}

// What a caller may not do, asserted on every component that lets a
// caller do anything at all. Each of these is a way to break a
// component from the outside and leave no trace at the call site.
func TestOverridesCannotBreakAComponent(t *testing.T) {
	hostile := html.Attrs{
		"id":            "stolen",
		"style":         "display:none",
		"data-hui-copy": "",
		"data-fui-main": "",
		"DATA-FUI-RPC":  "/evil",
		"data-behavior": "/evil.js",
		"Data-Island":   "smuggled",
		"class":         "mine",
		"data-testid":   "card",
		"aria-pressed":  "stuck",
		"aria-busy":     "stuck",
		"aria-live":     "assertive",
	}
	for _, sp := range Specs() {
		if sp.WithParts == nil {
			continue
		}
		t.Run(sp.Name, func(t *testing.T) {
			got := sp.WithParts(Classes{PartRoot: "real"}, Parts{Attrs: PartAttrs{PartRoot: hostile}})
			if strings.Contains(string(got), `"stolen"`) {
				t.Error("a caller renamed the root: ids are how a label finds its control")
			}
			if strings.Contains(string(got), "display:none") {
				t.Error("a caller set an inline style the CSP will drop — it would work in dev and vanish in production")
			}
			if strings.Contains(string(got), "data-hui-copy") {
				t.Error("a caller forged a runtime hook: behaviour is now bound to an element never built for it")
			}
			if strings.Contains(string(got), "data-fui-main") {
				t.Error("a caller forged a framework hook")
			}
			if strings.Contains(string(got), "/evil") || strings.Contains(string(got), "smuggled") {
				t.Errorf("a caller reached the framework runtime through a spelling the browser folds, or through a privileged unprefixed key:\n%s", got)
			}
			if !strings.Contains(string(got), `data-testid="card"`) {
				t.Error("an ordinary attribute was dropped — then the escape hatch is not one and the page forks the component")
			}
			if !strings.Contains(string(got), "real") || !strings.Contains(string(got), "mine") {
				t.Errorf("class must append, never replace — a component that arrives unstyled is worse than one with an extra class:\n%s", got)
			}
			// The mutation lifecycle owns these three on an action's
			// root, whichever way the caller reaches it: a forged
			// aria-pressed makes the module read a one-shot button as
			// a toggle, and a caller-set aria-busy or aria-live
			// announces a state the button is not in. Elsewhere they
			// are inert decoration and no rule refuses them.
			if sp.Name == "OptimisticAction" || sp.Name == "ToggleAction" {
				for _, dead := range []string{`aria-pressed="stuck"`, `aria-busy="stuck"`, `aria-live="assertive"`} {
					if strings.Contains(string(got), dead) {
						t.Errorf("a caller set %s on an action's root: the lifecycle owns it, and a stale value desynchronises the button from its own state machine", dead)
					}
				}
			}
		})
	}
}

// ─── coverage ───────────────────────────────────────────────────────

// unspecified lists the components written before the harness
// existed. It only goes down: a component here that gains a spec must
// leave the list, and a NEW component in neither fails outright. That
// is the whole mechanism — the debt is visible and cannot grow.
var unspecified = map[string]bool{}

// TestEveryComponentHasASpec reads the package's own source for
// exported functions that render, which is a component whatever it
// was called, and insists each one is either specified or admitted to
// be unspecified.
func TestEveryComponentHasASpec(t *testing.T) {
	found := exportedComponents(t)
	var missing []string
	for _, name := range found {
		if _, ok := SpecOf(name); ok {
			if unspecified[name] {
				t.Errorf("%s has a spec and is still on the unspecified list — take it off", name)
			}
			continue
		}
		if !unspecified[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no spec, and not admitted to the unspecified list: %s\n"+
			"A component with no fixture is tested by whoever remembers to.", strings.Join(missing, ", "))
	}
	for name := range unspecified {
		if !contains(found, name) {
			t.Errorf("%s is on the unspecified list and no longer exists", name)
		}
	}
}

// exportedComponents parses this package and returns every exported
// function that returns rendered HTML — the definition of a component
// that does not depend on anyone maintaining a list.
func exportedComponents(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing the package: %v", err)
	}
	helpers := map[string]bool{"El": true}
	var out []string
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || !fn.Name.IsExported() || helpers[fn.Name.Name] {
					continue
				}
				if fn.Type.Results == nil {
					continue
				}
				for _, r := range fn.Type.Results.List {
					if sel, ok := r.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "HTML" {
						out = append(out, fn.Name.Name)
						break
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

var idAttr = regexp.MustCompile(`\sid="([^"]+)"`)

func idsIn(h render.HTML) map[string]int {
	out := map[string]int{}
	for _, m := range idAttr.FindAllStringSubmatch(string(h), -1) {
		out[m[1]]++
	}
	return out
}

// probeFor is the probe class map for one named component: a class on every
// part that component declares, and nothing else. Keying it to the
// component is the whole point — a class map that styled every part of every
// component would let a fixture hand a Form's class map to an Input and
// still look correct.
func probeFor(name string) Classes {
	sp, ok := SpecOf(name)
	if !ok {
		return nil
	}
	s := Classes{}
	for _, p := range sp.Anatomy {
		s[p] = "probe-" + name + "-" + string(p)
	}
	return s
}

// namedProbeKit dresses this component and resolves every child to the
// child's own probe.
func namedProbeKit(own string) Kit {
	return NewKit(probeFor(own), func(component, variant string) Classes {
		return probeFor(component)
	})
}

// controlTag finds interactive elements and whether they carry a class.
var controlTag = regexp.MustCompile(`<(button|input|select|textarea|a)(\s[^>]*)?>`)

// TestEveryControlInAFixtureWearsAClass proves a fixture composes with
// the design system rather than around it.
//
// Two defects look identical on screen and neither was visible to any
// other check. A fixture that needs a button inside it could hand the
// child its own parent's class map — an Input wearing .ds-form, which styles
// none of an input's parts — or give up and write the child as a raw
// HTML string. Both render a naked browser-default control. A part
// check does not see it, because the control is not one of the
// component's parts. A golden does not see it, because the golden
// records whatever is there. The page audit does not see it, because an
// unstyled button is still a named button.
//
// Thirty-one components did one or the other before this existed, and
// a gallery page rendered from the fixtures was a third unstyled
// before anybody noticed.
func TestEveryControlInAFixtureWearsAClass(t *testing.T) {
	for _, sp := range Specs() {
		sp := sp
		t.Run(sp.Name, func(t *testing.T) {
			for _, c := range sp.Cases(namedProbeKit(sp.Name)) {
				for _, m := range controlTag.FindAllStringSubmatch(string(c.HTML), -1) {
					tag, attrs := m[1], m[2]
					if strings.Contains(attrs, `class="`) {
						continue
					}
					// A hidden input is never drawn, never focusable and
					// never reaches an audit. A class on it would style
					// nothing. Requiring one would push fixtures to add
					// decoration to prove a point the element cannot make.
					if strings.Contains(attrs, `type="hidden"`) {
						continue
					}
					t.Errorf("%s/%s: a <%s> with no class — %s\n"+
						"  either it is raw HTML in the fixture, or it was given a class map "+
						"that does not style it. Build it with the component and pass "+
						"k.For(\"…\") for its class map.",
						sp.Name, c.Name, tag, strings.TrimSpace(m[0]))
				}
			}
		})
	}
}

// TestEveryPartDrawnIsDeclared is the other direction of the part
// contract, and the one that was missing.
//
// TestEveryDeclaredPartIsActuallyDrawn catches a part named in a spec
// that nothing renders — a class in the stylesheet landing on nothing.
// This catches the reverse: a part the component draws and the spec
// never mentions. A range slider once called s.Class(PartControl) for
// its <input type="range"> while declaring only root, row, track,
// label and value. The class map happened to define it, so the real page
// was fine and nothing failed — but the spec is what a class-map author
// reads, and by that document the input did not exist. A new class map
// would have left it
// unstyled and no test would have said so.
func TestEveryPartDrawnIsDeclared(t *testing.T) {
	// Every part any component declares, so a part drawn by one and
	// declared by none of them still has a class to be caught by.
	universal := Classes{}
	for _, sp := range Specs() {
		for _, p := range sp.Anatomy {
			universal[p] = "probe-" + string(p)
		}
	}
	for _, sp := range Specs() {
		sp := sp
		t.Run(sp.Name, func(t *testing.T) {
			declared := map[Part]bool{}
			for _, p := range sp.Anatomy {
				declared[p] = true
			}
			// Children resolve to nil, so what is drawn here is this
			// component's own markup and not a child's.
			kit := NewKit(universal, func(component, variant string) Classes { return nil })
			var all strings.Builder
			for _, c := range sp.Cases(kit) {
				all.WriteString(string(c.HTML))
			}
			out := all.String()
			for p := range universal {
				if declared[p] || !drawsClass(out, "probe-"+string(p)) {
					continue
				}
				t.Errorf("%s draws the part %q but does not declare it in Parts.\n"+
					"  A spec is what a class-map author reads. An undeclared part is a "+
					"part nobody knows to style, and it renders naked in every class map "+
					"but the one that happened to guess.", sp.Name, p)
			}
		})
	}
}

// svgTag finds svg elements and captures their attributes.
var svgTag = regexp.MustCompile(`<svg(\s[^>]*)?>`)

// TestEverySVGInAFixtureDeclaresItsSize is the cheapest test here and
// it caught the worst-looking defect in the system.
//
// An <svg> with no width, no height and no viewBox has no intrinsic
// size, so CSS gives it the replaced-element default: 300 by 150
// pixels. Every fixture that needed an icon passed render.HTML("<svg/>")
// because it type-checks and reads like a placeholder. Eighteen of the
// nineteen icons on a gallery page were drawn at 300×150, which turned
// a badge into a 340px slab and opened a 140px hole in an alert's
// header.
//
// Every other check passed the whole time. The classes were right, the
// parts were declared, the controls were named, the audit was clean.
// Markup tests cannot see a box the size of a postcard, so the rule has
// to be stated about the markup instead: an icon says how big it is.
func TestEverySVGInAFixtureDeclaresItsSize(t *testing.T) {
	eachCase(t, func(t *testing.T, sp Spec, c Case) {
		for _, m := range svgTag.FindAllStringSubmatch(string(c.HTML), -1) {
			attrs := m[1]
			if strings.Contains(attrs, "viewBox=") ||
				(strings.Contains(attrs, "width=") && strings.Contains(attrs, "height=")) {
				continue
			}
			t.Errorf("%s/%s: an <svg> with no size — %s\n"+
				"  With no viewBox and no width/height it renders at the CSS "+
				"default of 300×150 and wrecks whatever lays out around it. "+
				"Use SpecimenGlyph.", sp.Name, c.Name, strings.TrimSpace(m[0]))
		}
	})
}

// A part nothing routes is a part that silently drops what it is
// given. The override sweep above proves a hostile override cannot
// break a component; this proves a benign binding on the root ARRIVES,
// for every component that offers its parts — which is the same as
// proving the root is rendered through the Box rather than around it.
// The root of most components is not fillable, and a text Bind there
// is refused (the gates below): those bind an attribute instead,
// which every root may carry.
func TestEveryComponentWithPartsRoutesABindToItsRoot(t *testing.T) {
	for _, sp := range Specs() {
		if sp.WithParts == nil {
			continue
		}
		t.Run(sp.Name, func(t *testing.T) {
			bind := Bind{Signal: "probe", Mode: "attr", Attr: "title"}
			if slices.Contains(sp.Fillable, PartRoot) {
				bind = Bind{Signal: "probe"}
			}
			got := sp.WithParts(nil, Parts{Binds: Binds{PartRoot: bind}})
			if !strings.Contains(string(got), `data-fui-signal="probe"`) {
				t.Errorf("a binding on the root never arrived: the root is rendered around the Box, "+
					"so overrides on it are dropped the same way:\n%s", got)
			}
		})
	}
}

// TestEveryDrawnPartRoutesTheAttrsACallerSets is the sweep the root
// gate above used to stand in for: every part the fixture DRAWS must
// carry what a caller sets on it, not only the root. A part rendered
// around the Box — a pager's links, a banner's tone word — drops
// attrs and binds the same way the root once could, and nothing else
// notices: the class still lands, the contract still passes, and the
// caller's attribute is gone. Drawn-ness is decided by a probe class
// on the part, the same evidence TestEveryDeclaredPartIsActuallyDrawn
// uses, so a part the fixture never renders demands nothing.
func TestEveryDrawnPartRoutesTheAttrsACallerSets(t *testing.T) {
	for _, sp := range Specs() {
		if sp.WithParts == nil {
			continue
		}
		for _, p := range sp.Anatomy {
			probe := "probe-" + string(p)
			if !drawsClass(string(sp.WithParts(Classes{p: probe}, Parts{})), probe) {
				continue
			}
			t.Run(sp.Name+"/"+string(p), func(t *testing.T) {
				got := sp.WithParts(nil, Parts{Attrs: PartAttrs{p: {"data-part-route": string(p)}}})
				if !strings.Contains(string(got), `data-part-route="`+string(p)+`"`) {
					t.Errorf("part %q is drawn and drops the attrs a caller sets on it: it is rendered around the Box\n%s", p, got)
				}
			})
		}
	}
}

// A text or html Bind replaces a part's content, so it is allowed
// exactly where a Slot is: on a part the spec lists as fillable.
// Everywhere else it would gut whatever guarantee the part carries —
// a Password's input and reveal button, an action's two labels — and
// the refusal fires at render, where the mistake is a panic with a
// reason rather than a component that quietly lost its content. Only
// parts the fixture draws are asserted, the same probe-class evidence
// as the routing sweep above.
func TestATextBindNeedsAFillablePart(t *testing.T) {
	for _, sp := range Specs() {
		if sp.WithParts == nil {
			continue
		}
		for _, p := range sp.Anatomy {
			if slices.Contains(sp.Fillable, p) {
				continue
			}
			probe := "probe-" + string(p)
			if !drawsClass(string(sp.WithParts(Classes{p: probe}, Parts{})), probe) {
				continue
			}
			t.Run(sp.Name+"/"+string(p), func(t *testing.T) {
				defer func() {
					if recover() == nil {
						t.Errorf("%s draws part %q and a text Bind on it rendered: it would replace the part's content", sp.Name, p)
					}
				}()
				sp.WithParts(nil, Parts{Binds: Binds{p: {Signal: "probe"}}})
			})
		}
	}
}

// The other direction of the same rule, so the fillable list a
// component hands its Box cannot drift from the one its spec
// declares: a text Bind on every part the spec lists as fillable
// must arrive, or the Box's list is narrower than the spec's and a
// legitimate binding is refused.
func TestEveryFillablePartTakesATextBind(t *testing.T) {
	for _, sp := range Specs() {
		if sp.WithParts == nil {
			continue
		}
		for _, p := range sp.Fillable {
			t.Run(sp.Name+"/"+string(p), func(t *testing.T) {
				got := sp.WithParts(nil, Parts{Binds: Binds{p: {Signal: "probe"}}})
				if !strings.Contains(string(got), `data-fui-signal="probe"`) {
					t.Errorf("part %q is fillable and a text Bind on it never arrived: the Box's fillable list is narrower than the spec's\n%s", p, got)
				}
			})
		}
	}
}

// A props type that embeds Parts offers them, and Spec.WithParts is
// the only thing that proves they arrive. Five components once carried
// the field for Slots alone and dropped Attrs and Binds on the
// floor, with nothing failing, because every parts gate above skips a
// spec with no WithParts. This reads the source instead: a struct
// with an embedded Parts field names a component, and that
// component's spec must render with its parts.
func TestEveryPropsTypeWithPartsHasAFixture(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				for _, spec := range gd.Specs {
					ts := spec.(*ast.TypeSpec)
					st, ok := ts.Type.(*ast.StructType)
					if !ok || !hasParts(st) {
						continue
					}
					name := strings.TrimSuffix(ts.Name.Name, "Props")
					sp, ok := SpecOf(name)
					if !ok {
						t.Errorf("%s embeds Parts and no spec is named %q", ts.Name.Name, name)
						continue
					}
					if sp.WithParts == nil {
						t.Errorf("%s embeds Parts and its spec has no WithParts: the parts gates skip it, "+
							"so an override or a bind it drops fails nothing", ts.Name.Name)
					}
				}
			}
		}
	}
}

func hasParts(st *ast.StructType) bool {
	for _, f := range st.Fields.List {
		if len(f.Names) != 1 || f.Names[0].Name != "Parts" {
			continue
		}
		if id, ok := f.Type.(*ast.Ident); ok && id.Name == "Parts" {
			return true
		}
	}
	return false
}

var dsAttr = regexp.MustCompile(`\s(data-hui-[a-z0-9-]+)(=|\s|/|>)`)

// childHook reports whether a hook rendered inside sp's cases belongs
// to a component the fixture composes: a Form case renders an Input,
// a Card case renders a Button, and their hooks are theirs to
// declare. The hook must be declared by SOME spec; an undeclared one
// fails either way.
func childHook(sp Spec, hook string) bool {
	for _, other := range Specs() {
		if other.Name == sp.Name {
			continue
		}
		for _, h := range other.Hooks {
			if h == hook {
				return true
			}
		}
	}
	return false
}
