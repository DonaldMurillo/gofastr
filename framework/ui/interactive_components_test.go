package ui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

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

func TestTabsContentWrapperHasSignalAttr(t *testing.T) {
	cfg := TabsConfig{
		SignalName: "tab",
		Tabs:       []TabItem{{Label: "A"}},
	}
	html := Tabs(cfg)

	mustContain(t, html, `data-fui-signal="tab"`)
	mustContain(t, html, `data-fui-signal-mode="attr"`)
	mustContain(t, html, `data-fui-signal-attr="data-active"`)
	mustContain(t, html, `data-active="0"`)
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

	// First button has active class and aria-selected=true.
	mustContain(t, html, `fui-tab--active`)
	mustContain(t, html, `aria-selected="true"`)

	count := strings.Count(string(html), `aria-selected="true"`)
	if count != 1 {
		t.Errorf("expected 1 aria-selected=\"true\", got %d", count)
	}
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
	mustContain(t, html, `fui-tab--active`)
}
