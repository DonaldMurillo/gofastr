package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// triggerBtn is the pinned host shape from the metacollector port: a
// real <button> whose classes belong to the page, addressable both by
// role+name and by a class-scoped tag selector.
const triggerBtn = `<button type="button" class="rounded-full">Open user menu</button>`

func triggerMenu(cfg ui.MenuConfig) render.HTML {
	if cfg.TriggerElement == "" {
		cfg.TriggerElement = render.HTML(triggerBtn)
	}
	if len(cfg.Items) == 0 {
		cfg.Items = []ui.MenuItem{{Label: "Profile", Href: "/me"}}
	}
	return ui.Menu(cfg)
}

// TestMenuTriggerElementMarkup: the caller's element lands verbatim in
// a presentation wrapper carrying data-fui-menu-trigger="<menu id>",
// BESIDE a summary-less <details data-fui-disclosure data-fui-menu>
// that holds the byte-identical panel markup. No <summary> anywhere at
// the top level — an interactive element inside one is axe
// nested-interactive, the violation this path exists to avoid.
func TestMenuTriggerElementMarkup(t *testing.T) {
	out := string(triggerMenu(ui.MenuConfig{ID: "um"}))
	for _, want := range []string{
		`<div class="ui-menu ui-menu--bottom-start" data-fui-comp="ui-menu">`,
		`<div data-fui-menu-trigger="um" role="presentation">` + triggerBtn + `</div>`,
		`<details data-fui-disclosure data-fui-menu="um">`,
		`<div class="ui-menu__panel" id="um-panel" role="menu" data-fui-menu-panel>`,
		`role="menuitem"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("trigger menu missing %q\n--\n%s", want, out)
		}
	}
	if strings.Contains(out, "<summary class=\"ui-menu__trigger\"") {
		t.Errorf("trigger menu must not render the framework summary:\n%s", out)
	}
	// The wrapper precedes the details (both children of the root) and
	// the panel sits INSIDE the details, so the disclosure machinery
	// (Escape, SPA-nav close, focus-on-open) scopes to the panel.
	wrapperAt := strings.Index(out, `data-fui-menu-trigger="um"`)
	detailsAt := strings.Index(out, `<details data-fui-disclosure data-fui-menu="um">`)
	if wrapperAt < 0 || detailsAt < 0 || wrapperAt > detailsAt {
		t.Errorf("wrapper must precede the details sibling:\n%s", out)
	}
}

// TestMenuTriggerElementNoNesting: the caller's <button> must have no
// interactive ancestor markup — the structural fact behind axe's
// nested-interactive rule. The root is a div, the wrapper a
// role=presentation div, and the button's own tags bracket no summary
// or details.
func TestMenuTriggerElementNoNesting(t *testing.T) {
	out := string(triggerMenu(ui.MenuConfig{ID: "um"}))
	btnAt := strings.Index(out, `<button type="button" class="rounded-full">`)
	endAt := strings.Index(out, `</button>`)
	if btnAt < 0 || endAt < 0 {
		t.Fatalf("caller button not rendered verbatim:\n%s", out)
	}
	if slice := out[btnAt:endAt]; strings.Contains(slice, "<summary") || strings.Contains(slice, "<details") {
		t.Errorf("caller button nested inside another element's markup: %q", slice)
	}
}

// TestMenuTriggerElementOverridesLabelAndHTML: TriggerElement replaces
// the summary entirely; Label and TriggerHTML are inert beside it (the
// precedence chain is Label < TriggerHTML < TriggerElement).
func TestMenuTriggerElementOverridesLabelAndHTML(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{
		ID:             "um",
		Label:          "Actions",
		TriggerHTML:    render.HTML(`<em>old</em>`),
		TriggerElement: render.HTML(triggerBtn),
		Items:          []ui.MenuItem{{Label: "Profile"}},
	}))
	if !strings.Contains(out, triggerBtn) {
		t.Errorf("TriggerElement not rendered:\n%s", out)
	}
	for _, stale := range []string{">Actions<", "ui-menu__trigger", "<em>old</em>", "ui-menu__caret"} {
		if strings.Contains(out, stale) {
			t.Errorf("summary-path %q leaked into trigger menu:\n%s", stale, out)
		}
	}
}

// TestMenuTriggerIDEscaped: the menu id feeds data-fui-menu-trigger and
// data-fui-menu raw, so it passes through render.Escape like every
// other interpolated value — a quote in the id cannot break out of the
// attribute.
func TestMenuTriggerIDEscaped(t *testing.T) {
	out := string(triggerMenu(ui.MenuConfig{ID: `it's "x"`}))
	for _, want := range []string{
		`data-fui-menu-trigger="it&#39;s &quot;x&quot;"`,
		`data-fui-menu="it&#39;s &quot;x&quot;"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("trigger menu missing escaped %q:\n%s", want, out)
		}
	}
	for _, raw := range []string{`it's`, `"x"`} {
		if strings.Contains(out, raw) {
			t.Errorf("trigger menu leaked raw %q:\n%s", raw, out)
		}
	}
}

// TestMenuTriggerAutoIDIncludesTrigger: the runtime resolves a trigger
// wrapper's details BY the shared data-fui-menu value, so two
// structurally identical trigger menus must not collide on the
// auto-generated id — the second trigger would toggle the first menu.
// Folding the caller markup into the hash input keeps them apart; two
// identical menus remain a caller bug (duplicate accessible names),
// documented on MenuConfig.TriggerElement.
func TestMenuTriggerAutoIDIncludesTrigger(t *testing.T) {
	items := []ui.MenuItem{{Label: "x"}}
	a := string(ui.Menu(ui.MenuConfig{TriggerElement: render.HTML(`<button type="button" class="a">A</button>`), Items: items}))
	b := string(ui.Menu(ui.MenuConfig{TriggerElement: render.HTML(`<button type="button" class="b">B</button>`), Items: items}))
	idA := menuTriggerID(t, a)
	idB := menuTriggerID(t, b)
	if idA == "" || idB == "" {
		t.Fatalf("auto id not emitted:\n%s\n%s", a, b)
	}
	if idA == idB {
		t.Fatalf("identical items + different triggers collided on %q — the second trigger would toggle the first menu", idA)
	}
	// The wrapper and the details must agree on the value: that is the
	// runtime's pairing contract.
	for _, out := range []string{a, b} {
		if !strings.Contains(out, `data-fui-menu="`+menuTriggerID(t, out)+`"`) {
			t.Errorf("details data-fui-menu does not match wrapper value:\n%s", out)
		}
	}
}

func menuTriggerID(t *testing.T, out string) string {
	t.Helper()
	const marker = `data-fui-menu-trigger="`
	i := strings.Index(out, marker)
	if i < 0 {
		return ""
	}
	rest := out[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatalf("unterminated data-fui-menu-trigger in:\n%s", out)
	}
	return rest[:j]
}

// TestMenuTriggerExtraAttrsOnRoot: ExtraAttrs land on the trigger
// path's root div (the summary path's root is the details; the
// ownership contract — class, id, data-fui-* dropped — is the same).
func TestMenuTriggerExtraAttrsOnRoot(t *testing.T) {
	out := string(triggerMenu(ui.MenuConfig{
		ID:         "um",
		ExtraAttrs: map[string]string{"data-test": "hook", "data-fui-menu": "smuggled", "id": "smuggled"},
	}))
	root := out[:strings.Index(out, ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("trigger root missing data-test:\n%s", root)
	}
	if strings.Contains(root, "smuggled") {
		t.Errorf("owned keys must be dropped from the trigger root:\n%s", root)
	}
}

// TestMenuTriggerGoldenBytes pins the trigger-element output
// byte-for-byte (same discipline as TestMenuItemZeroValueByteIdentical:
// substring asserts cannot see attribute reordering or escaping drift).
// Captured from the renderer at the branch's introduction: a caller
// button, an href row, and a radio submenu exercising the nested
// details id chain.
func TestMenuTriggerGoldenBytes(t *testing.T) {
	got := string(ui.Menu(ui.MenuConfig{
		ID:             "um",
		TriggerElement: render.HTML(`<button type="button" class="rounded-full">Open user menu</button>`),
		Items: []ui.MenuItem{
			{Label: "Profile", Href: "/me"},
			{Label: "Palette", Children: []ui.MenuItem{
				{Label: "Dark", Radio: "theme", Checked: true},
			}},
		},
	}))
	want := `<div class="ui-menu ui-menu--bottom-start" data-fui-comp="ui-menu"><div data-fui-menu-trigger="um" role="presentation"><button type="button" class="rounded-full">Open user menu</button></div><details data-fui-disclosure data-fui-menu="um"><div class="ui-menu__panel" id="um-panel" role="menu" data-fui-menu-panel><a class="ui-menu__item" href="/me" role="menuitem" tabindex="-1"><span class="ui-menu__label">Profile</span></a><details class="ui-menu__sub" data-fui-disclosure data-fui-menu="um-panel-sub-1"><summary class="ui-menu__item ui-menu__item--hassub" aria-haspopup="menu" aria-controls="um-panel-sub-1-panel" role="menuitem" tabindex="-1"><span class="ui-menu__label">Palette</span></summary><div class="ui-menu__panel ui-menu__panel--sub" id="um-panel-sub-1-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="true" data-fui-menu-radio="theme"><span class="ui-menu__label">Dark</span></button></div></details></div></details></div>`
	if got != want {
		t.Errorf("trigger menu bytes drifted:\n--got--\n%s\n--want--\n%s", got, want)
	}
}
