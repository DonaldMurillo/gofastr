package ui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestTabsRendersSignalSetOnButtons(t *testing.T) {
	cfg := TabsConfig{
		SignalName: "activeTab",
		Tabs: []TabItem{
			{Label: "First"},
			{Label: "Second"},
			{Label: "Third"},
		},
	}
	html := Tabs(cfg)

	for i, tab := range cfg.Tabs {
		want := `data-fui-signal-set="activeTab:` + strconv.Itoa(i) + `"`
		if !strings.Contains(string(html), want) {
			t.Errorf("button %d (%q): expected %q in HTML\n%s", i, tab.Label, want, html)
		}
	}
}

func TestTabsWrapperHasSignalAttr(t *testing.T) {
	cfg := TabsConfig{
		SignalName: "tab",
		Tabs:       []TabItem{{Label: "A"}},
	}
	html := Tabs(cfg)

	// The signal binding must live on the OUTER wrapper (the common
	// ancestor of nav + panels) so a single data-active attribute drives
	// BOTH the active button highlight and the visible panel.
	mustContain(t, html, `data-fui-signal="tab"`)
	mustContain(t, html, `data-fui-signal-mode="attr"`)
	mustContain(t, html, `data-fui-signal-attr="data-active"`)
	mustContain(t, html, `data-active="0"`)

	// The content wrapper must NOT carry the signal binding — if it did,
	// the binding couldn't reach the sibling nav buttons.
	if !strings.Contains(string(html), `<div class="fui-tabs-content">`) {
		t.Errorf("content wrapper should be bare (no signal binding), got:\n%s", html)
	}
}

func TestTabsRendersAllPanels(t *testing.T) {
	cfg := TabsConfig{
		SignalName: "p",
		Tabs: []TabItem{
			{Label: "A", Content: render.Text("Content A")},
			{Label: "B", Content: render.Text("Content B")},
		},
	}
	html := Tabs(cfg)

	mustContain(t, html, "Content A")
	mustContain(t, html, "Content B")
	mustContain(t, html, `data-fui-tab-index="0"`)
	mustContain(t, html, `data-fui-tab-index="1"`)
}

func TestTabsFirstButtonActive(t *testing.T) {
	cfg := TabsConfig{
		SignalName: "t",
		Tabs: []TabItem{
			{Label: "First"},
			{Label: "Second"},
		},
	}
	html := Tabs(cfg)

	// The initial active tab is expressed by the wrapper's data-active="0"
	// (CSS highlights the matching button), NOT a static class that would
	// stay stuck on the first tab after switching.
	mustContain(t, html, `data-active="0"`)
	mustContain(t, html, `aria-selected="true"`)

	count := strings.Count(string(html), `aria-selected="true"`)
	if count != 1 {
		t.Errorf("expected 1 aria-selected=\"true\", got %d", count)
	}

	// Regression: no always-on static active class — that was the bug
	// that froze the highlight on tab 0 after switching.
	if strings.Contains(string(html), `fui-tab--active`) {
		t.Errorf("static fui-tab--active class would freeze the highlight; active state must be data-driven:\n%s", html)
	}
}

// TestTabsActiveHighlightFollowsSignal pins the fix for the frozen
// highlight: the CSS must light up the active BUTTON (not just the
// panel) for each tab index, keyed off the wrapper's data-active.
func TestTabsActiveHighlightFollowsSignal(t *testing.T) {
	css := tabsStyle.Entry().CSSFor(style.Theme{})
	for i := 0; i < 3; i++ {
		btnRule := fmt.Sprintf(`[data-fui-comp="fui-tabs"][data-active="%d"] .fui-tab[data-fui-tab-index="%d"]`, i, i)
		if !strings.Contains(css, btnRule) {
			t.Errorf("missing active-button rule for tab %d (highlight won't move):\n%s", i, btnRule)
		}
		panelRule := fmt.Sprintf(`[data-fui-comp="fui-tabs"][data-active="%d"] .fui-tab-panel[data-fui-tab-index="%d"]`, i, i)
		if !strings.Contains(css, panelRule) {
			t.Errorf("missing visible-panel rule for tab %d:\n%s", i, panelRule)
		}
	}
}

// TestTabsPanelRuleCoversManyTabs pins the fix for the silent 6-tab cap:
// the CSS must cover every supported index, not a hardcoded 0..5.
func TestTabsPanelRuleCoversManyTabs(t *testing.T) {
	css := tabsStyle.Entry().CSSFor(style.Theme{})
	last := tabsMaxPanels - 1
	rule := fmt.Sprintf(`[data-fui-comp="fui-tabs"][data-active="%d"] .fui-tab-panel[data-fui-tab-index="%d"]`, last, last)
	if !strings.Contains(css, rule) {
		t.Errorf("CSS does not cover tab index %d (panels beyond the cap silently stay hidden):\n%s", last, css)
	}
}

// TestTabsRejectsTooManyTabs guards the cap loudly instead of silently
// hiding panels past the CSS rule count.
func TestTabsRejectsTooManyTabs(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for too many tabs")
		}
		if !strings.Contains(fmt.Sprintf("%v", r), "at most") {
			t.Fatalf("panic should explain the cap: %v", r)
		}
	}()
	tabs := make([]TabItem, tabsMaxPanels+1)
	for i := range tabs {
		tabs[i] = TabItem{Label: strconv.Itoa(i)}
	}
	Tabs(TabsConfig{SignalName: "t", Tabs: tabs})
}

func TestTabsSecondButtonNotActive(t *testing.T) {
	cfg := TabsConfig{
		SignalName: "t",
		Tabs: []TabItem{
			{Label: "A"},
			{Label: "B"},
		},
	}
	html := Tabs(cfg)

	count := strings.Count(string(html), `aria-selected="false"`)
	if count != 1 {
		t.Errorf("expected 1 aria-selected=\"false\", got %d", count)
	}
}

func TestTabsPanicMissingSignalName(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for empty SignalName")
		}
		if !strings.Contains(fmt.Sprintf("%v", r), "SignalName") {
			t.Fatalf("unexpected panic message: %v", r)
		}
	}()
	Tabs(TabsConfig{Tabs: []TabItem{{Label: "X"}}})
}

func TestTabsPanicEmptyTabs(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for empty Tabs")
		}
		if !strings.Contains(fmt.Sprintf("%v", r), "TabItem") {
			t.Fatalf("unexpected panic message: %v", r)
		}
	}()
	Tabs(TabsConfig{SignalName: "x"})
}

func TestTabsCustomClass(t *testing.T) {
	cfg := TabsConfig{
		SignalName: "t",
		Tabs:       []TabItem{{Label: "A"}},
		Class:      "my-extra",
	}
	html := Tabs(cfg)

	mustContain(t, html, `class="fui-tabs my-extra"`)
}

func TestTabsHasCompMarker(t *testing.T) {
	cfg := TabsConfig{
		SignalName: "t",
		Tabs:       []TabItem{{Label: "A"}},
	}
	html := Tabs(cfg)

	mustContain(t, html, `data-fui-comp="fui-tabs"`)
}

func TestTabsRoleSemantics(t *testing.T) {
	cfg := TabsConfig{
		SignalName: "t",
		Tabs: []TabItem{
			{Label: "A"},
			{Label: "B"},
		},
	}
	html := Tabs(cfg)

	mustContain(t, html, `role="tablist"`)
	if strings.Count(string(html), `role="tab"`) != 2 {
		t.Errorf("expected 2 role=tab, got %d", strings.Count(string(html), `role="tab"`))
	}
	if strings.Count(string(html), `role="tabpanel"`) != 2 {
		t.Errorf("expected 2 role=tabpanel, got %d", strings.Count(string(html), `role="tabpanel"`))
	}
}

func TestTabsSingleTab(t *testing.T) {
	cfg := TabsConfig{
		SignalName: "t",
		Tabs:       []TabItem{{Label: "Only"}},
	}
	html := Tabs(cfg)

	mustContain(t, html, `data-fui-signal-set="t:0"`)
	mustContain(t, html, "Only")
	// The single tab is active via data-active="0" on the wrapper, not a
	// static class.
	mustContain(t, html, `data-active="0"`)
}
