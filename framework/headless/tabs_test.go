package headless

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func renderTabs(p TabsProps) string { return string(Tabs(p, nil)) }

func TestTabsRendersRovingTabindexAndPanels(t *testing.T) {
	h := renderTabs(TabsProps{Name: "cfg", Tabs: []Tab{
		{Label: "General", Panel: render.Text("The general panel.")},
		{Label: "Secrets", Panel: render.Text("The secrets panel.")},
	}})
	for _, want := range []string{
		`<nav role="tablist">`,
		`<a aria-controls="cfg-panel-0" aria-selected="true" data-fui-signal-set="cfg:0" data-fui-tab-index="0" href="#cfg-panel-0" id="cfg-tab-0" role="tab" tabindex="0">General</a>`,
		`<a aria-controls="cfg-panel-1" aria-selected="false" data-fui-signal-set="cfg:1" data-fui-tab-index="1" href="#cfg-panel-1" id="cfg-tab-1" role="tab" tabindex="-1">Secrets</a>`,
		`<div aria-labelledby="cfg-tab-0" data-fui-tab-index="0" id="cfg-panel-0" role="tabpanel" tabindex="0">The general panel.</div>`,
		`<div aria-labelledby="cfg-tab-1" data-fui-tab-index="1" id="cfg-panel-1" role="tabpanel" tabindex="0">The secrets panel.</div>`,
		`data-active="0" data-fui-signal="cfg" data-fui-signal-attr="data-active" data-fui-signal-mode="attr" data-hui-tabs=""`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("tabs missing %q:\n%s", want, h)
		}
	}
	// Exactly one tab is in the tab order.
	if n := strings.Count(h, `tabindex="0"`); n != 3 { // active tab + two panels
		t.Errorf("%d tabindex=0 attrs, want 3 (one tab + the panels):\n%s", n, h)
	}
}

func TestTabsVacateStashAndState(t *testing.T) {
	h := renderTabs(TabsProps{Name: "vac", Active: 1, VacateHidden: true, StateAttrs: true, Tabs: []Tab{
		{Label: "One", Panel: render.Text("First")},
		{Label: "Two", Panel: render.Text("Second")},
	}})
	for _, want := range []string{
		`data-hui-tabs-state="" data-hui-tabs-vacate=""`,
		`data-state="inactive"`,
		`data-state="active"`,
		`<div aria-labelledby="vac-tab-0" data-fui-tab-index="0" id="vac-panel-0" role="tabpanel" tabindex="0"></div>`,
		`<div aria-labelledby="vac-tab-1" data-fui-tab-index="1" id="vac-panel-1" role="tabpanel" tabindex="0">Second</div>`,
		`<script data-hui-tabs-stash="true" type="application/json">`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("vacated tabs missing %q:\n%s", want, h)
		}
	}
	// The stash is JSON: index → HTML, with </ escaped.
	i := strings.Index(h, `data-hui-tabs-stash="true" type="application/json">`)
	rest := h[i+len(`data-hui-tabs-stash="true" type="application/json">`):]
	body := rest[:strings.Index(rest, "</script>")]
	var m map[string]string
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("the stash is not JSON: %v\n%s", err, body)
	}
	if m["0"] != "First" {
		t.Fatalf("stash[0] = %q, want First", m["0"])
	}
}

func TestTabsUnsafeHrefDegradesToFragment(t *testing.T) {
	h := renderTabs(TabsProps{Name: "ev", Tabs: []Tab{
		{Label: "A", Panel: render.Text("a"), Href: "javascript:alert(1)"},
	}})
	if strings.Contains(h, `href=""`) {
		t.Errorf("an unsafe href degraded to an empty anchor (a link to the current page):\n%s", h)
	}
	if !strings.Contains(h, `href="#ev-panel-0"`) {
		t.Errorf("an unsafe href must degrade to the fragment the tab would have had:\n%s", h)
	}
}

func TestTabsRefusesBrokenConfiguration(t *testing.T) {
	many := make([]Tab, 25)
	for i := range many {
		many[i] = Tab{Label: string(rune('A' + i)), Panel: render.Text("p")}
	}
	cases := []struct {
		name string
		p    TabsProps
	}{
		{"no name", TabsProps{Tabs: []Tab{{Label: "A", Panel: render.Text("a")}}}},
		{"no tabs", TabsProps{Name: "x"}},
		{"labelless tab", TabsProps{Name: "x", Tabs: []Tab{{Panel: render.Text("a")}}}},
		{"reserved signal name", TabsProps{Name: "__proto__", Tabs: []Tab{{Label: "A", Panel: render.Text("a")}}}},
		{"too many tabs", TabsProps{Name: "x", Tabs: many}},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: rendering should have been refused", tc.name)
				}
			}()
			Tabs(tc.p, nil)
		}()
	}
	// Active out of range takes 0, not a refusal (carried state).
	h := renderTabs(TabsProps{Name: "x", Active: 9, Tabs: []Tab{{Label: "A", Panel: render.Text("a")}}})
	if !strings.Contains(h, `aria-selected="true"`) {
		t.Errorf("an out-of-range Active left no tab selected:\n%s", h)
	}
}

func TestTabsScrubbedCarriedLabels(t *testing.T) {
	h := renderTabs(TabsProps{Name: "x", Tabs: []Tab{{Label: "Set\r\ntings", Panel: render.Text("p")}}})
	if !strings.Contains(h, ">Settings<") {
		t.Errorf("a CR LF in a carried label reached the reader:\n%s", h)
	}
}
