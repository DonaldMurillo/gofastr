package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func TestMenuRendersTriggerAndItems(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{
		Label: "Actions",
		Items: []ui.MenuItem{
			{Label: "Edit"},
			{Separator: true},
			{Label: "Delete", Danger: true, RPC: "/delete", RPCMethod: "POST"},
		},
	}))
	for _, want := range []string{
		`data-fui-comp="ui-menu"`,
		`data-fui-disclosure`,
		`data-fui-menu="`,
		`<summary`,
		`aria-haspopup="menu"`,
		`role="menu"`,
		`role="menuitem"`,
		`>Edit<`,
		`<hr class="ui-menu__sep" role="separator">`,
		`ui-menu__item--danger`,
		`data-fui-rpc="/delete"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Menu html missing %q\n--\n%s", want, out)
		}
	}
}

func TestMenuHrefRendersAnchor(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{
		Label: "Go",
		Items: []ui.MenuItem{{Label: "Home", Href: "/"}},
	}))
	if !strings.Contains(out, `<a class="ui-menu__item" href="/"`) {
		t.Errorf("Href item should render as <a>:\n%s", out)
	}
}

func TestMenuPositionClass(t *testing.T) {
	cases := map[ui.MenuPosition]string{
		ui.MenuBottomEnd: "ui-menu--bottom-end",
		ui.MenuTopStart:  "ui-menu--top-start",
		ui.MenuTopEnd:    "ui-menu--top-end",
	}
	for pos, cls := range cases {
		out := string(ui.Menu(ui.MenuConfig{
			Label:    "x",
			Position: pos,
			Items:    []ui.MenuItem{{Label: "a"}},
		}))
		if !strings.Contains(out, cls) {
			t.Errorf("position %s should emit class %q\n%s", pos, cls, out)
		}
	}
}

func TestMenuCustomTriggerHTML(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{
		TriggerHTML: render.HTML(`<svg class="icon"></svg>`),
		Items:       []ui.MenuItem{{Label: "Settings"}},
	}))
	if !strings.Contains(out, `<svg class="icon"></svg>`) {
		t.Error("custom TriggerHTML not rendered")
	}
	if strings.Contains(out, `ui-menu__caret`) {
		t.Error("custom TriggerHTML should suppress default caret")
	}
}

func TestMenuBlocksSmuggledAttrKeys(t *testing.T) {
	// ExtraAttrs values are escaped, but render.Escape does not touch spaces
	// so a key like `x onclick` must not render a live onclick attribute.
	// Keys go through the same allow-list as every other ExtraAttrs
	// consumer (core/render.Attr): on* handlers and syntactically
	// invalid names are dropped.
	out := string(ui.Menu(ui.MenuConfig{
		Label: "Actions",
		Items: []ui.MenuItem{{
			Label: "Edit",
			ExtraAttrs: map[string]string{
				`x onclick`:   "alert(1)",
				"onmouseover": "alert(2)",
				"data-ok":     "yes",
			},
		}},
	}))
	if strings.Contains(out, "onclick") || strings.Contains(out, "onmouseover") || strings.Contains(out, "alert(") {
		t.Errorf("smuggled event-handler attr leaked into Menu html:\n%s", out)
	}
	if !strings.Contains(out, `data-ok="yes"`) {
		t.Errorf("legitimate ExtraAttrs key dropped:\n%s", out)
	}
}

func TestMenuPanicsOnEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty Items")
		}
	}()
	_ = ui.Menu(ui.MenuConfig{Label: "x"})
}

// TestMenuEscapesApostrophes pins the 5-char escaper contract on every
// Menu interpolation site (trigger label, panel/trigger classes, item
// labels, data-fui-rpc attributes). menu.go used to ship a 4-char
// attribute escaper (no '), the same reduced shape kiln/chat documents
// as a real attribute-breakout XSS, so an apostrophe must come out
// entity-escaped everywhere, and the raw value must never survive.
func TestMenuEscapesApostrophes(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{
		Label:        `it's here`,
		PanelClass:   `pan'el`,
		TriggerClass: `trig'ger`,
		Items: []ui.MenuItem{
			{Label: `don't`, RPC: "/api/x'y"},
		},
	}))
	for _, want := range []string{
		`it&#39;s here`,
		`pan&#39;el`,
		`trig&#39;ger`,
		`don&#39;t`,
		`/api/x&#39;y`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Menu output missing escaped %q:\n%s", want, out)
		}
	}
	for _, raw := range []string{`it's`, `pan'el`, `trig'ger`, `don't`, `/api/x'y`} {
		if strings.Contains(out, raw) {
			t.Errorf("Menu output leaked raw %q:\n%s", raw, out)
		}
	}
}

func TestMenuExtraAttrsOnRoot(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{
		Label:      "Actions",
		Items:      []ui.MenuItem{{Label: "Edit"}},
		ExtraAttrs: map[string]string{"data-test": "hook"},
	}))
	root := out[:strings.Index(out, ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("menu root missing data-test:\n%s", root)
	}
}

func TestMenuItemConfirmEmitsOnRPCItems(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{
		Label: "Actions",
		Items: []ui.MenuItem{
			{Label: "Delete", RPC: "/api/items/1", RPCMethod: "DELETE",
				Confirm: "Delete this item?", Danger: true},
			{Label: "Open", Href: "/x", Confirm: "ignored without RPC"},
		},
	}))
	if !strings.Contains(out, `data-fui-confirm="Delete this item?"`) {
		t.Errorf("rpc item missing data-fui-confirm:\n%s", out)
	}
	if strings.Contains(out, "ignored without RPC") {
		t.Errorf("Confirm must be inert on non-RPC items:\n%s", out)
	}
}

func TestMenuItemExtraAttrsCannotOverrideOwned(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{
		Label: "Actions",
		Items: []ui.MenuItem{
			{Label: "Open", Href: "/x", ExtraAttrs: map[string]string{
				"data-test": "hook-a", "href": "javascript:evil()", "Class": "evil",
			}},
			{Label: "Delete", ExtraAttrs: map[string]string{
				"data-test": "hook-b", "role": "evil", "tabindex": "evil",
			}},
		},
	}))
	for _, want := range []string{
		`data-test="hook-a"`, `data-test="hook-b"`,
		`href="/x"`, `role="menuitem"`, `tabindex="-1"`, `type="button"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("item output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "evil") {
		t.Errorf("owned item attr overridden by ExtraAttrs:\n%s", out)
	}
}

// TestMenuItemZeroValueByteIdentical pins the zero-value Menu output
// byte-for-byte against bytes captured from main at e7deb99b (clean
// tree, before MenuItem.ID existed): a throwaway module with a
// replace directive rendered these exact configs twice and cmp'd the
// runs for determinism before any edit was made. Every config leaves
// MenuItem.ID unset, so an empty ID must not change a single byte —
// no id attribute, no attribute reordering, no escaping drift.
// Substring asserts cannot see any of that; only full-output
// equality can.
func TestMenuItemZeroValueByteIdentical(t *testing.T) {
	for _, g := range goldenMenus {
		t.Run(g.name, func(t *testing.T) {
			if got := string(ui.Menu(g.cfg)); got != g.want {
				t.Errorf("zero-value Menu output drifted from main bytes:\n--got--\n%s\n--want--\n%s", got, g.want)
			}
		})
	}
}

var goldenMenus = []struct {
	name string
	cfg  ui.MenuConfig
	want string
}{
	{
		name: "plain-button",
		cfg:  ui.MenuConfig{Label: "Actions", Items: []ui.MenuItem{{Label: "Edit"}}},
		want: `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="ui-menu-57ac0b74" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="ui-menu-57ac0b74-panel">Actions<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="ui-menu-57ac0b74-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1"><span class="ui-menu__label">Edit</span></button></div></details>`,
	},
	{
		name: "href-anchor",
		cfg:  ui.MenuConfig{Label: "Go", Items: []ui.MenuItem{{Label: "Home", Href: "/"}}},
		want: `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="ui-menu-96810e5c" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="ui-menu-96810e5c-panel">Go<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="ui-menu-96810e5c-panel" role="menu" data-fui-menu-panel><a class="ui-menu__item" href="/" role="menuitem" tabindex="-1"><span class="ui-menu__label">Home</span></a></div></details>`,
	},
	{
		name: "href-refused-scheme",
		cfg:  ui.MenuConfig{Label: "Go", Items: []ui.MenuItem{{Label: "Evil", Href: "javascript:alert(1)"}}},
		want: `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="ui-menu-90970f37" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="ui-menu-90970f37-panel">Go<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="ui-menu-90970f37-panel" role="menu" data-fui-menu-panel><a class="ui-menu__item" href="#" role="menuitem" tabindex="-1"><span class="ui-menu__label">Evil</span></a></div></details>`,
	},
	{
		name: "danger-disabled-icon-class",
		cfg: ui.MenuConfig{Label: "Actions", Items: []ui.MenuItem{{
			Label: "Delete", Danger: true, Disabled: true, Class: "extra-cls",
			Icon: render.HTML(`<svg width="12"></svg>`),
		}}},
		want: `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="ui-menu-020cb409" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="ui-menu-020cb409-panel">Actions<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="ui-menu-020cb409-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item ui-menu__item--danger ui-menu__item--disabled extra-cls" type="button" role="menuitem" tabindex="-1"aria-disabled="true" disabled><span class="ui-menu__icon" aria-hidden="true"><svg width="12"></svg></span><span class="ui-menu__label">Delete</span></button></div></details>`,
	},
	{
		name: "disabled-anchor",
		cfg:  ui.MenuConfig{Label: "Go", Items: []ui.MenuItem{{Label: "Locked", Href: "/x", Disabled: true}}},
		want: `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="ui-menu-a636a70b" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="ui-menu-a636a70b-panel">Go<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="ui-menu-a636a70b-panel" role="menu" data-fui-menu-panel><a class="ui-menu__item ui-menu__item--disabled" href="/x" role="menuitem" tabindex="-1"aria-disabled="true"><span class="ui-menu__label">Locked</span></a></div></details>`,
	},
	{
		name: "rpc-confirm-method",
		cfg: ui.MenuConfig{Label: "Row", Items: []ui.MenuItem{
			{Label: "Delete", RPC: "/api/items/1", RPCMethod: "DELETE", Confirm: "Delete this item?", Danger: true},
			{Label: "Save", RPC: "/api/save"},
			{Label: "Open", Href: "/x", Confirm: "inert without rpc"},
		}},
		want: `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="ui-menu-af395ced" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="ui-menu-af395ced-panel">Row<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="ui-menu-af395ced-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item ui-menu__item--danger" type="button" role="menuitem" tabindex="-1" data-fui-rpc="/api/items/1" data-fui-rpc-method="DELETE" data-fui-confirm="Delete this item?"><span class="ui-menu__label">Delete</span></button><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1" data-fui-rpc="/api/save" data-fui-rpc-method="POST"><span class="ui-menu__label">Save</span></button><a class="ui-menu__item" href="/x" role="menuitem" tabindex="-1"><span class="ui-menu__label">Open</span></a></div></details>`,
	},
	{
		name: "extraattrs-item",
		cfg: ui.MenuConfig{Label: "Actions", Items: []ui.MenuItem{{
			Label: "Edit",
			ExtraAttrs: map[string]string{
				"data-test": "hook", "id": "smuggled", "ID": "smuggled2", "Class": "evil", "role": "evil", "tabindex": "evil",
				"aria-label": "Edit thing", "x onclick": "alert(1)", "onmouseover": "alert(2)",
			},
		}}},
		want: `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="ui-menu-57ac0b74" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="ui-menu-57ac0b74-panel">Actions<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="ui-menu-57ac0b74-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1" aria-label="Edit thing" data-test="hook"><span class="ui-menu__label">Edit</span></button></div></details>`,
	},
	{
		name: "separator-and-mixed",
		cfg: ui.MenuConfig{ID: "user-menu", Label: "Account", Items: []ui.MenuItem{
			{Label: "Profile", Href: "/me"},
			{Separator: true},
			{Label: "it's <b>escaped</b>", Icon: render.HTML("⚙")},
		}},
		want: `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="user-menu" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="user-menu-panel">Account<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="user-menu-panel" role="menu" data-fui-menu-panel><a class="ui-menu__item" href="/me" role="menuitem" tabindex="-1"><span class="ui-menu__label">Profile</span></a><hr class="ui-menu__sep" role="separator"><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1"><span class="ui-menu__icon" aria-hidden="true">⚙</span><span class="ui-menu__label">it&#39;s &lt;b&gt;escaped&lt;/b&gt;</span></button></div></details>`,
	},
	{
		name: "trigger-html-position-classes",
		cfg: ui.MenuConfig{
			TriggerHTML:  render.HTML(`<svg class="icon"></svg>`),
			Position:     ui.MenuTopEnd,
			TriggerClass: `trig'ger`,
			PanelClass:   `pan'el`,
			ExtraAttrs:   map[string]string{"data-root": "yes", "id": "smuggled-root"},
			Items:        []ui.MenuItem{{Label: "Settings"}},
		},
		want: `<details class="ui-menu ui-menu--top-end pan&#39;el" data-fui-disclosure data-fui-menu="ui-menu-6db4093c" data-root="yes" data-fui-comp="ui-menu"><summary class="ui-menu__trigger trig&#39;ger" aria-haspopup="menu" aria-controls="ui-menu-6db4093c-panel"><svg class="icon"></svg></summary><div class="ui-menu__panel" id="ui-menu-6db4093c-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1"><span class="ui-menu__label">Settings</span></button></div></details>`,
	},
	{
		name: "position-bottom-end",
		cfg:  ui.MenuConfig{Label: "P", Items: []ui.MenuItem{{Label: "a"}}, Position: ui.MenuBottomEnd},
		want: `<details class="ui-menu ui-menu--bottom-end" data-fui-disclosure data-fui-menu="ui-menu-c65b0be2" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="ui-menu-c65b0be2-panel">P<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="ui-menu-c65b0be2-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1"><span class="ui-menu__label">a</span></button></div></details>`,
	},
}

// TestMenuItemIDEmittedOnRows: a set ID lands on the rendered row's
// open tag, right after class (mirroring the panel div's class/id/role
// order), on every row dialect — button, anchor, RPC button — and
// leaves the framework-owned contract untouched.
func TestMenuItemIDEmittedOnRows(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{Label: "Account", Items: []ui.MenuItem{
		{Label: "Help mode", ID: "help-toggle"},
		{Label: "Profile", ID: "profile-link", Href: "/me"},
		{Label: "Delete", ID: "del", RPC: "/api/del", RPCMethod: "DELETE", Danger: true, Disabled: true},
	}}))
	for _, want := range []string{
		`<button class="ui-menu__item" id="help-toggle" type="button" role="menuitem" tabindex="-1">`,
		`<a class="ui-menu__item" id="profile-link" href="/me" role="menuitem" tabindex="-1">`,
		// NOTE: no space before aria-disabled — pre-existing on main
		// (disabledAttr lacks a leading space; browsers parse-error
		// and recover). Pinned as-is; fixing it changes zero-value
		// bytes and is a separate change from MenuItem.ID.
		`<button class="ui-menu__item ui-menu__item--danger ui-menu__item--disabled" id="del" type="button" role="menuitem" tabindex="-1"aria-disabled="true" disabled data-fui-rpc="/api/del" data-fui-rpc-method="DELETE">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("row with ID missing exact open tag %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, `id="`); n != 4 { // 3 rows + the panel div
		t.Errorf("got %d id attributes, want 4 (3 rows + panel):\n%s", n, out)
	}
}

// TestMenuItemIDEscaped: the ID goes through render.Escape like every
// other interpolated value, so quotes/angles cannot break out of the
// attribute.
func TestMenuItemIDEscaped(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{Label: "x", Items: []ui.MenuItem{
		{Label: "Evil", ID: `a'b<c>"d" id="x`},
	}}))
	if !strings.Contains(out, `id="a&#39;b&lt;c&gt;&quot;d&quot; id=&quot;x"`) {
		t.Errorf("ID not escaped into attribute:\n%s", out)
	}
	if strings.Contains(out, `id="a'b`) {
		t.Errorf("raw ID leaked into attribute context:\n%s", out)
	}
}

// TestMenuItemExtraAttrsIDStillDropped: one owner per attribute. ID the
// field is the only way to an id on a row; SafeExtraAttrs keeps
// dropping every case-variant of the key.
func TestMenuItemExtraAttrsIDStillDropped(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{Label: "Actions", Items: []ui.MenuItem{{
		Label: "Edit", ID: "real-id",
		ExtraAttrs: map[string]string{"id": "smuggled", "ID": "smuggled2", "Id": "smuggled3"},
	}}}))
	if !strings.Contains(out, `id="real-id"`) {
		t.Errorf("field-set ID missing from row:\n%s", out)
	}
	if strings.Contains(out, "smuggled") {
		t.Errorf("ExtraAttrs id (some case-variant) survived onto the row:\n%s", out)
	}
	if n := strings.Count(out, `id="`); n != 2 { // the row + the panel div
		t.Errorf("got %d id attributes, want 2 (row + panel):\n%s", n, out)
	}
}

// TestMenuItemIDIgnoredOnSeparator: a separator is an <hr>; an id on it
// is meaningless, and the component's established stance for fields on
// the separator variant is documented ignore (Separator's own doc:
// "Label and other fields are ignored when true").
func TestMenuItemIDIgnoredOnSeparator(t *testing.T) {
	out := string(ui.Menu(ui.MenuConfig{Label: "Actions", Items: []ui.MenuItem{
		{Separator: true, ID: "sep-id", Label: "also ignored"},
	}}))
	if !strings.Contains(out, `<hr class="ui-menu__sep" role="separator">`) {
		t.Errorf("separator row changed shape:\n%s", out)
	}
	if strings.Contains(out, "sep-id") || strings.Contains(out, "also ignored") {
		t.Errorf("separator picked up fields it must ignore:\n%s", out)
	}
}
