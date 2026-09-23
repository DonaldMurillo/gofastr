package headless

// The behaviour module gates. The registration and the module are two
// halves of one contract, and each gate below catches the way one half
// drifts from the other: a marker nothing declares, a hook nothing
// binds, a stylesheet-only list that outlived its hooks, a sentence said in
// a language the caller did not choose, and a kernel contract left
// half-kept. The gates read the module's source rather than executing
// it, so they hold everywhere the package's tests run, browser or not.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
)

// jsWithoutComments strips the // and /* */ comments from the module's
// source, so a name mentioned only in prose cannot count as bound and
// a sentence in a comment cannot count as said. It is a blunt
// instrument on purpose: the file it reads is this package's own,
// carries no URL and no string with a comment marker inside it, and a
// future gate failing loudly on one is cheaper than a JavaScript
// parser that silently passes.
var jsBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
var jsLineComment = regexp.MustCompile(`//[^\n]*`)

func jsWithoutComments() string {
	out := ""
	for _, src := range moduleSources {
		out += jsStripComments(src) + "\n"
	}
	return out
}

func jsStripComments(src string) string {
	return jsLineComment.ReplaceAllString(jsBlockComment.ReplaceAllString(src, " "), " ")
}

// moduleSources is every behaviour module source this package
// registers, one entry per RegisterBehavior. The gates read them all:
// a hook may be declared by one component and bound by any module in
var moduleSources = []string{behaviorJS, controlsJS, collectionsJS, wizardJS, feedbackJS, navigationJS, whenJS, railJS, tocJS, disclosureJS, menuJS, comboboxJS, tabsJS, carouselJS, panehostJS, sidebarJS}

// others' hooks unbound. The list grows as the package registers
// more modules (headless-controls, headless-collections,
// headless-wizard; headless-feedback and headless-navigation land with
// their components' callers).

// moduleHook matches a data-hui-* name written out in the source (a
// selector, an attribute string); datasetHook matches the camel-case
// spelling a dataset access reads the same attribute by.
var moduleHook = regexp.MustCompile(`data-hui-[a-z0-9-]+`)
var datasetHook = regexp.MustCompile(`dataset\.(hui[A-Za-z0-9]*)`)

// kebabHook maps a dataset property to its attribute: huiWhenValue
// back to data-hui-when-value.
func kebabHook(camel string) string {
	var b strings.Builder
	b.WriteString("data-hui")
	for _, r := range strings.TrimPrefix(camel, "hui") {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('-')
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// camelHook is kebabHook backwards: data-hui-when-off to huiWhenOff.
func camelHook(name string) string {
	var b strings.Builder
	b.WriteString("hui")
	for _, seg := range strings.Split(strings.TrimPrefix(name, "data-hui-"), "-") {
		if seg == "" {
			continue
		}
		b.WriteString(strings.ToUpper(seg[:1]))
		b.WriteString(seg[1:])
	}
	return b.String()
}

// moduleBoundHooks is every data-hui-* name the module binds, in
// either spelling.
func moduleBoundHooks(src string) map[string]bool {
	bound := map[string]bool{}
	for _, m := range moduleHook.FindAllString(src, -1) {
		bound[m] = true
	}
	for _, m := range datasetHook.FindAllStringSubmatch(src, -1) {
		bound[kebabHook(m[1])] = true
	}
	return bound
}

// declaredHooks is every hook every Spec declares.
func declaredHooks() map[string]bool {
	out := map[string]bool{}
	for _, sp := range Specs() {
		for _, h := range sp.Hooks {
			out[h] = true
		}
	}
	return out
}

// runtimeOwned reports whether the script WRITES this attribute as
// well as reading it. Detected rather than listed, because a list of
// exceptions is a list that outlives its reasons: a write is a
// setAttribute or removeAttribute of the name, or the dataset property
// assigned or deleted.
func runtimeOwned(src, name string) bool {
	q := regexp.QuoteMeta(name)
	if regexp.MustCompile(`(?:set|remove)Attribute\(\s*['"]` + q + `['"]`).MatchString(src) {
		return true
	}
	camel := camelHook(name)
	if regexp.MustCompile(`dataset\.` + camel + `\s*=[^=]`).MatchString(src) {
		return true
	}
	return regexp.MustCompile(`delete\s+[^;]*dataset\.` + camel + `\b`).MatchString(src)
}

// TestBehaviorIsRegisteredWithItsMarkers catches a registration that
// drifted from the markers it claims: a marker dropped here is a page
// whose controls are dead DOM, because the module never loads, and a
// marker for a hook no Spec declares is the kernel fetching a module
// for markup this package cannot render.
func TestBehaviorIsRegisteredWithItsMarkers(t *testing.T) {
	e, ok := uiregistry.LookupBehavior(BehaviorName)
	if !ok {
		t.Fatalf("%q is not registered: every data-hui-* hook this package renders would be bound to nothing", BehaviorName)
	}
	if len(e.Markers) != len(behaviorMarkers) {
		t.Fatalf("%q registers %d markers, behaviorMarkers lists %d: the two lists have drifted",
			BehaviorName, len(e.Markers), len(behaviorMarkers))
	}
	registered := map[string]bool{}
	for _, m := range e.Markers {
		registered[m] = true
	}
	declared := declaredHooks()
	for _, m := range behaviorMarkers {
		if !registered[m] {
			t.Errorf("marker %s is listed in behaviorMarkers and not registered: a page carrying it loads nothing", m)
		}
		if !declared[strings.Trim(m, "[]")] {
			t.Errorf("marker %s is not a hook any Spec declares: the kernel would load the module for markup this package cannot render", m)
		}
	}
}

// TestEveryHookTheModuleBindsIsDeclared catches the rename that loses
// a behaviour without a single failure: the module looks for an
// attribute no component renders, an attribute selector that matches
// nothing and reports nothing. The only names exempt are the module's
// own writes, detected in the source rather than excused by a list.
func TestEveryHookTheModuleBindsIsDeclared(t *testing.T) {
	src := jsWithoutComments()
	declared := declaredHooks()
	bound := moduleBoundHooks(src)
	names := make([]string, 0, len(bound))
	for n := range bound {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if declared[n] {
			continue
		}
		if runtimeOwned(src, n) {
			continue
		}
		if _, ok := adapterHooks[n]; ok {
			continue
		}
		t.Errorf("%s is bound by the module and declared by no Spec: a rename on either side fails silently", n)
	}
}

// sheetHooks are the declared hooks no script reads, each with the
// reason it needs no module. The reason is load-bearing: the gate
// refuses an empty one, because an unexplained exemption is an
// exemption nobody re-reads.
var sheetHooks = map[string]string{
	"data-hui-grow":          "the spacer's flex factor: the stylesheet sizes the spacer from the number, and no script ever reads it",
	"data-hui-lines":         "the skeleton's line count: the stylesheet draws as many bars as the root says",
	"data-hui-skeleton-last": "the short final line of a multi-line skeleton: a shape decision a stylesheet makes and a script never touches",
	"data-hui-sidebar-group": "the sidebar group's details wrapper: an identity marker for host stylesheets to scope group overrides to, the way the lightbox names its zoom target by attribute so no class-map rename breaks a host selector; the framework's own sheet styles groups by class, and the group's open/close is the browser's (a script never reads it)",
}

// hostHooks are the declared hooks whose binder is not this package's
// module and never will be: the lightbox viewer's anatomy ships for a
// host writing its own viewer behaviour against a zero Wiring, and the
// hooks render exactly there — the wired render carries the
// data-fui-lightbox* family for framework/ui's module instead (a
// ui-owned module binds data-fui-* hooks only). The reasons describe
// the unwired host-direct path and stake no in-tree claim: what the
// framework's binder reads is pinned where it lives, in framework/ui's
// own tests. Same discipline as sheetHooks: the reason is mandatory,
// the list is checked both ways, and a hook this package's own module
// ever grows to read must leave it (the module-bound pass below
// enforces that).
// adapterHooks are the hooks whose declarer is a framework/ui
// adapter, not a headless Spec: the copy control is a Button plus
// behaviour (no copy primitive exists, by the plan's binding
// decision), the theme group is a Button composition, and the retry
// link is the offline SystemBanner's action. This package's modules
// bind them; the markup that renders them is pinned where it lives,
// in framework/ui's own tests. Same discipline as hostHooks: the
// reason is mandatory and the list is checked against the module
var adapterHooks = map[string]string{
	"data-hui-pane-close":             "a trigger that closes a pane, rendered by core-ui/interactive's pane helpers",
	"data-hui-pane-swap":              "a trigger that swaps the open pane, rendered by core-ui/interactive's pane helpers",
	"data-hui-pane-key":               "a pane trigger's deep-link key, rendered by core-ui/interactive's PaneKey",
	"data-hui-sidebar-collapse-label": "the collapse toggle's custom label, rendered by ui.Sidebar",
	"data-hui-sidebar-expand-label":   "the collapse toggle's custom expand label, rendered by ui.Sidebar",
	"data-hui-sidebar-group-toggle":   "the button-dialect group toggle, rendered by ui.Sidebar's SidebarGroupButton markup",
	"data-hui-shortcut-focus":         "a chord that focuses its target, rendered by ui.GlobalSearch / ui.CommandPalette on the search wrapper",
	"data-hui-shortcut-click":         "a chord that clicks its target, rendered by ui.ShortcutHint (BindTarget) and host chrome like the site header",
	"data-hui-shortcut-target":        "the selector naming a chord's focus/click target, rendered by ui.GlobalSearch beside its focus chord",
	"data-hui-shortcut-hint":          "ShortcutHint's own marker when BindTarget is set: names the element the chord will click",
	"data-hui-copy":                   "the copy wrapper framework/ui.CopyButton renders around a headless Button; no copy primitive exists by binding decision",
	"data-hui-copy-target":            "the copied element's id, rendered by ui.CopyButton",
	"data-hui-copy-name":              "the copied thing's name for the status sentence, rendered by ui.CopyButton",
	"data-hui-copy-label":             "the button's idle label span, rendered by ui.CopyButton",
	"data-hui-copy-copied":            "the button's copied label, rendered by ui.CopyButton",
	"data-hui-copy-back":              "the label restored after the copied flash, rendered by ui.CopyButton",
	"data-hui-copy-sentence":          "the status sentence shape with {name}, rendered by ui.CopyButton",
	"data-hui-copy-status":            "the polite status region, rendered by ui.CopyButton",
	"data-hui-theme-toggle":           "the scheme group wrapper, rendered by ui.ThemeToggle (a Button composition, no theme primitive by binding decision)",
	"data-hui-theme-option":           "one scheme option control, rendered by ui.ThemeToggle",
	"data-hui-theme-cycle":            "the single-button cycler, rendered by ui.ThemeToggle",
	"data-hui-network-retry":          "the offline banner's retry link, rendered by ui.NetworkRetryBanner as the SystemBanner's action",
	"data-hui-copy-toast":             "the toast-on-copy config, rendered by ui.CopyButton on the button",
}

var hostHooks = map[string]string{
	"data-hui-lightbox":       "the viewer's identity on an unwired render: what a host's own viewer module resolves the open viewer by",
	"data-hui-lightbox-nav":   "the nav opt-in on an unwired render: for a host module that steps the gallery group itself",
	"data-hui-lightbox-image": "the zoom target on an unwired render: the image a host's own gesture handling owns, named by attribute so no class selector is needed",
	"data-hui-lightbox-prev":  "the previous-image button on an unwired render, for a host module binding its own nav",
	"data-hui-lightbox-next":  "the next-image button on an unwired render, for a host module binding its own nav",
}

// TestEveryDeclaredHookIsBoundOrForTheStylesheet catches the other direction
// of the same drift: a hook every Spec declares that neither the
// module, a stylesheet nor a host's own module reads is an attribute
// the markup carries for no one. The exemption lists are checked both
// ways so they cannot rot: a hook the module grew to read must leave
// its list, and a hook no Spec declares means the list outlived its
// reason.
func TestEveryDeclaredHookIsBoundOrForTheStylesheet(t *testing.T) {
	src := jsWithoutComments()
	read := moduleBoundHooks(src)
	declared := declaredHooks()
	for _, sp := range Specs() {
		for _, h := range sp.Hooks {
			if read[h] {
				continue
			}
			if _, ok := sheetHooks[h]; ok {
				continue
			}
			if _, ok := hostHooks[h]; !ok {
				t.Errorf("%s (declared by %s) is read by neither the module, a stylesheet nor a host's own module: a hook nothing binds is an attribute the markup carries for no one", h, sp.Name)
			}
		}
	}
	for h, reason := range sheetHooks {
		if reason == "" {
			t.Errorf("%s carries no reason: an unexplained exemption is one nobody re-reads", h)
		}

		if !declared[h] {
			t.Errorf("%s is listed as stylesheet-only but no Spec declares it: the list has outlived its hook", h)
		}
		if read[h] {
			t.Errorf("%s is listed as stylesheet-only but the module reads it: the list is wrong today, not just stale", h)
		}
	}
	for h, reason := range hostHooks {
		if reason == "" {
			t.Errorf("%s carries no reason: an unexplained exemption is one nobody re-reads", h)
		}
		if !declared[h] {
			t.Errorf("%s is listed as host-bound but no Spec declares it: the list has outlived its hook", h)
		}
		if read[h] {
			t.Errorf("%s is listed as host-bound but this package's module reads it: the hook has its binder here now, drop the exemption", h)
		}
	}
	for h, reason := range adapterHooks {
		if reason == "" {
			t.Errorf("%s carries no reason: an unexplained exemption is one nobody re-reads", h)
		}
		if declared[h] {
			t.Errorf("%s is listed as adapter-owned but a Spec declares it: the declarer moved here, drop the exemption", h)
		}
		if !read[h] {
			t.Errorf("%s is listed as adapter-owned but no module in this package reads it: the list has outlived its binder", h)
		}
	}
}

func isAsciiLetter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// TestModuleSaysNothingInEnglish catches a sentence the module says
// itself. Every string it writes must have arrived as a data-hui-*
// attribute the component rendered from its Strings, because a sentence
// hardcoded here is a sentence every translated page says in English.
func TestModuleSaysNothingInEnglish(t *testing.T) {
	src := jsWithoutComments()
	text := regexp.MustCompile(`textContent\s*=\s*'([^']*)'|textContent\s*=\s*"([^"]*)"`)
	for _, m := range text.FindAllStringSubmatch(src, -1) {
		lit := m[1]
		if m[2] != "" {
			lit = m[2]
		}
		if strings.ContainsFunc(lit, isAsciiLetter) {
			t.Errorf("the module writes the literal %q as text: a sentence it says itself is a sentence a translated page says in English", lit)
		}
	}
	label := regexp.MustCompile(`setAttribute\(\s*'aria-label'\s*,\s*'([^']*)'|setAttribute\(\s*"aria-label"\s*,\s*"([^"]*)"`)
	for _, m := range label.FindAllStringSubmatch(src, -1) {
		lit := m[1] + m[2]
		if strings.ContainsFunc(lit, isAsciiLetter) {
			t.Errorf("the module sets aria-label to the literal %q: an accessible name it invents is one the caller never chose", lit)
		}
	}
}

// TestModuleKeepsTheKernelContract catches a module that attaches but
// cannot be re-armed: without the loadedModules flag the kernel
// fetches it again on every insertion, and without the registered
// scanner markup that arrives after load is never handed to it, which
// is exactly the arrival the contract exists for.
func TestModuleKeepsTheKernelContract(t *testing.T) {
	modules := []struct{ name, src string }{
		{BehaviorName, behaviorJS},
		{ControlsBehaviorName, controlsJS},
		{CollectionsBehaviorName, collectionsJS},
		{WizardBehaviorName, wizardJS},
	}
	for _, m := range modules {
		if body := strings.TrimSpace(jsStripComments(m.src)); !strings.HasPrefix(body, "(function () {") {
			t.Errorf("%s does not open with the IIFE the kernel contract requires", m.name)
		}
		for _, want := range []string{
			"'use strict';",
			"loadedModules[NAME] = true",
			"_moduleScanners[NAME] = scan",
		} {
			if !strings.Contains(m.src, want) {
				t.Errorf("%s lost %q: half the kernel contract is a module that cannot be re-armed", m.name, want)
			}
		}
	}
}

// TestPageWithAMarkerPreloadsTheModule catches a marker the preload
// scan cannot see: without the preload the module arrives after the
// page instead of with it, so the first click on a reveal button finds
// nothing bound. The reverse is the waste: a page with no marker
// fetching a module it cannot use. It imports core-ui/runtime to prove
// that a headless test can: the runtime sits below this package and
// must never import it.
func TestPageWithAMarkerPreloadsTheModule(t *testing.T) {
	has := func(html string) bool {
		for _, n := range runtime.NeededModules(html) {
			if n == BehaviorName {
				return true
			}
		}
		return false
	}
	if !has(string(Password(PasswordProps{Name: "token", ID: "token"}, nil))) {
		t.Errorf("a rendered Password does not preload %s: the module arrives after the page instead of with it", BehaviorName)
	}
	if has(string(Badge(BadgeProps{Label: "running"}, nil))) {
		t.Errorf("a rendered Badge preloads %s: a page with no marker fetches a module it cannot use", BehaviorName)
	}
}

// TestOfflineBannerReadsTheFieldsSseMirrors pins the field names the
// module reads off window.__gofastr.sseStatus to the ones sse.js
// assigns onto it. sse.js owns that object and this package owns the
// banner that follows it; the two ship from packages that never see
// each other's types, so a rename in sse.js would otherwise land as
// an offline banner that never shows — in production only. The gate
// reads both sources and fails here instead.
func TestOfflineBannerReadsTheFieldsSseMirrors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "core-ui", "runtime", "src", "sse.js"))
	if err != nil {
		t.Fatalf("reading sse.js: %v", err)
	}
	assigned := map[string]bool{}
	// The one place the mirror is built: NS.sseStatus = { ... }. The
	// fields are bare identifiers before a colon, which is the whole
	// shape the banner depends on.
	lit := regexp.MustCompile(`NS\.sseStatus\s*=\s*\{([^}]*)\}`).FindStringSubmatch(string(raw))
	if lit == nil {
		t.Fatal("sse.js no longer assigns window.__gofastr.sseStatus: the offline banner reads a mirror that is never built")
	}
	for _, m := range regexp.MustCompile(`(?:^|,)\s*([A-Za-z_$][\w$]*)\s*:`).FindAllStringSubmatch(lit[1], -1) {
		assigned[m[1]] = true
	}
	if len(assigned) == 0 {
		t.Fatal("the sseStatus mirror in sse.js carries no fields: the mirror changed shape and this gate must follow it")
	}

	// The module reads the mirror in exactly one place, sseLost, so
	// that function's body is the whole surface a rename can break.
	// It is found by name, not line number, so the gate survives
	// edits around it and fails loudly when the read moves.
	body := regexp.MustCompile(`function sseLost[^{]*\{([^}]*)\}`).FindStringSubmatch(behaviorJS)
	if body == nil {
		t.Fatal("behavior.js lost sseLost: the offline banner's read of the connection moved, and this gate must move with it")
	}
	reads := map[string]bool{}
	for _, m := range regexp.MustCompile(`st\.([A-Za-z_$][\w$]*)`).FindAllStringSubmatch(body[1], -1) {
		reads[m[1]] = true
	}
	if len(reads) == 0 {
		t.Fatal("sseLost reads no fields off the status object: the offline banner decides nothing")
	}
	for name := range reads {
		if !assigned[name] {
			t.Errorf("the module reads sseStatus.%s, which sse.js never assigns: a rename there is a banner that never shows, and today it would be found only in production", name)
		}
	}
	// The two fields the banner's lifetime actually turns on must be
	// in the mirror, whatever else the read grows to touch.
	for _, want := range []string{"connected", "retryCount"} {
		if !assigned[want] {
			t.Errorf("sse.js no longer assigns %s on sseStatus: the offline banner cannot read the connection", want)
		}
	}
}

// TestEveryRegisteredBehaviorIsRegisteredWithItsMarkers holds the three
// module registrations to the same contract the headless module's own
// gate holds: the registered markers are exactly the literals on the
// RegisterBehavior call (the hard-rule-5 gate reads that call), and
// every marker names a hook some Spec declares, so the kernel never
// fetches a module for markup this package cannot render.
func TestEveryRegisteredBehaviorIsRegisteredWithItsMarkers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		markers []string
	}{
		{ControlsBehaviorName, []string{"[data-hui-counter-animate]", "[data-hui-number-input-decrement]",
			"[data-hui-slider-output]", "[data-hui-range-slider]"}},
		{CollectionsBehaviorName, []string{"[data-hui-tag-input]", "[data-hui-repeater]"}},
		{WizardBehaviorName, []string{"[data-hui-step-wizard]"}},
		{FeedbackBehaviorName, []string{"[data-hui-copy]", "[data-hui-toast-stack]",
			"[data-fui-toast-stack]", "[data-hui-notification-bell]", "[data-hui-network-retry]"}},
		{NavigationBehaviorName, []string{"[data-hui-back-to-top]", "[data-hui-theme-toggle]",
			"[data-hui-shortcut-focus]", "[data-hui-shortcut-click]"}},
		{ComboboxBehaviorName, []string{"[data-hui-combobox-input]"}},
		{TabsBehaviorName, []string{"[data-hui-tabs]"}},
		{CarouselBehaviorName, []string{"[data-hui-carousel]"}},
		{PaneHostBehaviorName, []string{"[data-hui-panehost]"}},
		{SidebarBehaviorName, []string{"[data-hui-sidebar]"}},
	} {
		e, ok := uiregistry.LookupBehavior(tc.name)
		if !ok {
			t.Fatalf("%q is not registered: every hook its components render would be bound to nothing", tc.name)
		}
		if len(e.Markers) != len(tc.markers) {
			t.Fatalf("%q registers %d markers, the call spells %d: the two lists have drifted", tc.name, len(e.Markers), len(tc.markers))
		}
		registered := map[string]bool{}
		for _, m := range e.Markers {
			registered[m] = true
		}
		for _, m := range tc.markers {
			if !registered[m] {
				t.Errorf("%q lists marker %s in its registration call and the registry does not carry it", tc.name, m)
			}
		}
	}
	declared := declaredHooks()
	src := jsWithoutComments()
	read := moduleBoundHooks(src)
	for _, tc := range []struct {
		name    string
		markers []string
	}{
		{ControlsBehaviorName, []string{"[data-hui-counter-animate]", "[data-hui-number-input-decrement]",
			"[data-hui-slider-output]", "[data-hui-range-slider]"}},
		{CollectionsBehaviorName, []string{"[data-hui-tag-input]", "[data-hui-repeater]"}},
		{WizardBehaviorName, []string{"[data-hui-step-wizard]"}},
		{FeedbackBehaviorName, []string{"[data-hui-copy]", "[data-hui-toast-stack]",
			"[data-fui-toast-stack]", "[data-hui-notification-bell]", "[data-hui-network-retry]"}},
		{NavigationBehaviorName, []string{"[data-hui-back-to-top]", "[data-hui-theme-toggle]",
			"[data-hui-shortcut-focus]", "[data-hui-shortcut-click]"}},
		{ComboboxBehaviorName, []string{"[data-hui-combobox-input]"}},
		{TabsBehaviorName, []string{"[data-hui-tabs]"}},
		{CarouselBehaviorName, []string{"[data-hui-carousel]"}},
		{PaneHostBehaviorName, []string{"[data-hui-panehost]"}},
		{SidebarBehaviorName, []string{"[data-hui-sidebar]"}},
	} {
		for _, m := range tc.markers {
			hook := strings.Trim(m, "[]")
			known := declared[hook]
			if _, ok := adapterHooks[hook]; ok {
				known = true
			}
			if hook == "data-fui-toast-stack" {
				// The kernel's own stack name: documented in
				// core-ui/ARCHITECTURE.md's data-fui-* table, the one
				// data-fui spelling a registered marker may carry.
				known = true
			}
			if !known {
				t.Errorf("%s's marker %s is a hook no Spec declares: the kernel would load it for markup this package cannot render", tc.name, m)
			}
			if !read[hook] && hook != "data-fui-toast-stack" {
				t.Errorf("%s's marker %s is read by no module source in this package: a marker nothing binds is a fetch for a behaviour that does not exist", tc.name, m)
			}
		}
	}
}

// TestEveryRenderedHookInGoIsDeclared catches the hook that ships
// without its contract: a data-hui-* name written into this package's
// Go source — a Mark call, a registration's marker list — that no Spec
// declares and no reasoned exemption owns. The module-bound and
// declared-hook gates above read the Spec list; this one reads the
// source, so a hook rendered by a component whose Spec forgot it (or
// by a helper no Spec backs) fails here rather than shipping as an
// attribute with no declared owner. The exemption classes are the
// same three the declared-hook gate knows, checked the same both ways.
func TestEveryRenderedHookInGoIsDeclared(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	rendered := map[string]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, lit := range stringLiteralsOf(f) {
				for _, m := range moduleHook.FindAllStringSubmatch(lit, -1) {
					rendered[m[0]] = true
				}
			}
		}
	}
	declared := declaredHooks()
	for h := range rendered {
		if declared[h] {
			continue
		}
		if _, ok := adapterHooks[h]; ok {
			continue
		}
		if _, ok := hostHooks[h]; ok {
			continue
		}
		t.Errorf("%s is rendered by this package's Go source and declared by no Spec, adapter or host exemption: a hook with no owner is an attribute the markup carries for no one", h)
	}
}

// stringLiteralsOf collects every string literal in f, comments
// stripped first so a name mentioned in prose cannot count as
// rendered.
func stringLiteralsOf(f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil {
				out = append(out, s)
			}
		}
		return true
	})
	return out
}
