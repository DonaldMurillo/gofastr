package ui

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestTabsZeroValueOutputPinned pins the FULL SSR output of a zero-value
// TabsConfig (no StateAttrs, no ID, no VacateHidden). The contract for the
// #320 contract knobs is that opting out keeps the bytes identical, so any
// change to the default render — a stray attribute, reordered markup, new
// wrapper marker — fails here before it ships. If you intended to change the
// default output, that is a compat decision, not a test fix: update the
// golden AND say so in the changelog.
func TestTabsZeroValueOutputPinned(t *testing.T) {
	got := string(Tabs(TabsConfig{
		SignalName: "t",
		Tabs: []TabItem{
			{Label: "A", Content: render.Text("alpha")},
			{Label: "B", Content: render.Text("beta")},
		},
	}))
	want := `<div class="fui-tabs" data-active="0" data-fui-signal="t" data-fui-signal-attr="data-active" data-fui-signal-mode="attr" data-fui-comp="fui-tabs"><nav class="fui-tabs-nav" role="tablist"><button aria-selected="true" class="fui-tab" data-fui-signal-set="t:0" data-fui-tab-index="0" role="tab">A</button><button aria-selected="false" class="fui-tab" data-fui-signal-set="t:1" data-fui-tab-index="1" role="tab">B</button></nav><div class="fui-tabs-content"><div class="fui-tab-panel" data-fui-tab-index="0" role="tabpanel">alpha</div><div class="fui-tab-panel" data-fui-tab-index="1" role="tabpanel">beta</div></div></div>`
	if got != want {
		t.Errorf("zero-value Tabs output changed:\ngot:  %s\nwant: %s", got, want)
	}
}

// TestTabsStateAttrsOnAndOff covers both directions of the StateAttrs knob:
// on → data-state lands on every button with active/inactive tracking the
// initial index, plus the wrapper markers the runtime module keys on; off →
// no data-state anywhere and no wrapper markers (the golden test above pins
// the full zero-value shape; this asserts the specific absence so a failure
// names the regression).
func TestTabsStateAttrsOnAndOff(t *testing.T) {
	on := string(Tabs(TabsConfig{
		SignalName: "s",
		StateAttrs: true,
		Tabs: []TabItem{
			{Label: "A", Content: render.Text("a")},
			{Label: "B", Content: render.Text("b")},
			{Label: "C", Content: render.Text("c")},
		},
	}))
	if !strings.Contains(on, `data-fui-tab-index="0" data-state="active" role="tab"`) {
		t.Errorf("active tab must carry data-state=active:\n%s", on)
	}
	for _, i := range []string{"1", "2"} {
		if !strings.Contains(on, `data-fui-tab-index="`+i+`" data-state="inactive" role="tab"`) {
			t.Errorf("inactive tab %s must carry data-state=inactive:\n%s", i, on)
		}
	}
	if !strings.Contains(on, `data-fui-tabs-state="true"`) {
		t.Errorf("wrapper must carry the data-fui-tabs-state marker:\n%s", on)
	}
	if !strings.Contains(on, `data-fui-prefetch="tabs"`) {
		t.Errorf("wrapper must demand-load the tabs module:\n%s", on)
	}

	off := string(Tabs(TabsConfig{
		SignalName: "s",
		Tabs:       []TabItem{{Label: "A"}, {Label: "B"}},
	}))
	if strings.Contains(off, "data-state") {
		t.Errorf("zero-value Tabs must not emit data-state:\n%s", off)
	}
	if strings.Contains(off, "data-fui-tabs-state") || strings.Contains(off, "data-fui-prefetch") {
		t.Errorf("zero-value Tabs must not emit the tabs module markers:\n%s", off)
	}
}

var ariaControlsRe = regexp.MustCompile(`aria-controls="([^"]+)"`)
var idRe = regexp.MustCompile(` id="([^"]+)"`)

// TestTabsAriaControlsPairsRoundTrip asserts the aria-controls contract: in a
// single render, every aria-controls value names an id that exists on a
// role=tabpanel element, every panel id is named by exactly one tab, and the
// per-index naming pattern holds. This is the pairing a port's AT parity and
// test locators lean on.
func TestTabsAriaControlsPairsRoundTrip(t *testing.T) {
	const idBase = "settings"
	out := string(Tabs(TabsConfig{
		SignalName: "c",
		ID:         idBase,
		Tabs: []TabItem{
			{Label: "A", Content: render.Text("a")},
			{Label: "B", Content: render.Text("b")},
			{Label: "C", Content: render.Text("c")},
		},
	}))

	controls := ariaControlsRe.FindAllStringSubmatch(out, -1)
	if len(controls) != 3 {
		t.Fatalf("expected 3 aria-controls (one per tab), got %d:\n%s", len(controls), out)
	}
	ids := idRe.FindAllStringSubmatch(out, -1)
	if len(ids) != 6 { // 3 buttons + 3 panels
		t.Fatalf("expected 6 id attributes (3 tabs + 3 panels), got %d:\n%s", len(ids), out)
	}
	present := map[string]bool{}
	for _, m := range ids {
		present[m[1]] = true
	}
	for _, m := range controls {
		target := m[1]
		if !present[target] {
			t.Errorf("aria-controls=%q names no id in the same render:\n%s", target, out)
		}
	}
	// Each aria-controls target must be a PANEL id, not a button id:
	// panels are the elements the attribute is contractually allowed
	// to reference.
	for _, m := range controls {
		target := m[1]
		if !strings.HasPrefix(target, idBase+"-panel-") {
			t.Errorf("aria-controls=%q must reference a panel id:\n%s", target, out)
		}
	}
	// Per-index pattern for both families.
	for i := range 3 {
		tabID := idBase + "-tab-" + strconv.Itoa(i)
		panelID := idBase + "-panel-" + strconv.Itoa(i)
		if !strings.Contains(out, `id="`+tabID+`"`) {
			t.Errorf("tab %d id %q missing:\n%s", i, tabID, out)
		}
		if !strings.Contains(out, `id="`+panelID+`"`) {
			t.Errorf("panel %d id %q missing:\n%s", i, panelID, out)
		}
	}
}

var stashBodyRe = regexp.MustCompile(`(?s)data-fui-tabs-stash="true"[^>]*>(.*?)</script>`)

// TestTabsVacateHiddenShipsStashOnly: with VacateHidden the ACTIVE panel's
// content ships in the DOM, every INACTIVE panel ships empty, and their
// content lives only in the adjacent JSON stash keyed by tab index.
func TestTabsVacateHiddenShipsStashOnly(t *testing.T) {
	out := string(Tabs(TabsConfig{
		SignalName:   "v",
		VacateHidden: true,
		Tabs: []TabItem{
			{Label: "A", Content: render.Text("alpha-body")},
			{Label: "B", Content: render.Text("beta-body")},
			{Label: "C", Content: render.Text("gamma-body")},
		},
	}))

	if !strings.Contains(out, `data-fui-tabs-vacate="true"`) {
		t.Errorf("wrapper must carry the vacate marker:\n%s", out)
	}
	if !strings.Contains(out, `data-fui-tab-index="0" role="tabpanel">alpha-body<`) {
		t.Errorf("active panel content must ship in the DOM:\n%s", out)
	}
	if strings.Contains(out, "beta-body") && !strings.Contains(out, `data-fui-tabs-stash`) {
		t.Errorf("inactive content leaked into the DOM:\n%s", out)
	}
	// Inactive panels are EMPTY shells.
	for _, i := range []string{"1", "2"} {
		if !strings.Contains(out, `data-fui-tab-index="`+i+`" role="tabpanel"></div>`) {
			t.Errorf("inactive panel %s must ship empty:\n%s", i, out)
		}
	}

	m := stashBodyRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("stash script missing:\n%s", out)
	}
	var stash map[string]string
	if err := json.Unmarshal([]byte(m[1]), &stash); err != nil {
		t.Fatalf("stash is not valid JSON: %v\n%s", err, m[1])
	}
	want := map[string]string{"1": "beta-body", "2": "gamma-body"}
	if len(stash) != len(want) {
		t.Errorf("stash = %v, want %v", stash, want)
	}
	for k, v := range want {
		if stash[k] != v {
			t.Errorf("stash[%q] = %q, want %q", k, stash[k], v)
		}
	}
	if _, ok := stash["0"]; ok {
		t.Errorf("active panel must not be stashed: %v", stash)
	}
}

// TestTabsVacateStashEscapesScriptTerminator: panel content containing a
// literal </script> must not terminate the inline stash script early. The
// stash body must contain no raw "</" sequence and must JSON-decode back to
// the original content.
func TestTabsVacateStashEscapesScriptTerminator(t *testing.T) {
	raw := `<p>alpha</p><script>alert(1)</script>`
	out := string(Tabs(TabsConfig{
		SignalName:   "e",
		VacateHidden: true,
		Tabs: []TabItem{
			{Label: "A", Content: render.HTML("safe")},
			{Label: "B", Content: render.HTML(raw)},
		},
	}))
	m := stashBodyRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("stash script missing:\n%s", out)
	}
	if strings.Contains(m[1], "</") {
		t.Errorf("stash body must escape every </ sequence, got:\n%s", m[1])
	}
	var stash map[string]string
	if err := json.Unmarshal([]byte(m[1]), &stash); err != nil {
		t.Fatalf("stash is not valid JSON: %v\n%s", err, m[1])
	}
	if stash["1"] != raw {
		t.Errorf("stash[1] = %q, want the original content %q", stash["1"], raw)
	}
}

// TestTabsVacateSingleTabNoStash: with one tab there is no hidden panel, so
// no stash script and no dead marker weight beyond the vacate opt-in.
func TestTabsVacateSingleTabNoStash(t *testing.T) {
	out := string(Tabs(TabsConfig{
		SignalName:   "one",
		VacateHidden: true,
		Tabs:         []TabItem{{Label: "Only", Content: render.Text("solo")}},
	}))
	if strings.Contains(out, "data-fui-tabs-stash") {
		t.Errorf("single-tab strip must not ship a stash:\n%s", out)
	}
	if !strings.Contains(out, `role="tabpanel">solo<`) {
		t.Errorf("the only panel ships its content:\n%s", out)
	}
}

// TestTabsContractKnobsCompose: all three knobs at once produce one wrapper
// carrying both markers and one prefetch attr (not two), ids on every
// button/panel, and a stash that references panels by index only.
func TestTabsContractKnobsCompose(t *testing.T) {
	out := string(Tabs(TabsConfig{
		SignalName:   "x",
		ID:           "allthree",
		StateAttrs:   true,
		VacateHidden: true,
		Tabs: []TabItem{
			{Label: "A", Content: render.Text("a")},
			{Label: "B", Content: render.Text("b")},
		},
	}))
	if strings.Count(out, `data-fui-prefetch="tabs"`) != 1 {
		t.Errorf("prefetch marker must appear exactly once:\n%s", out)
	}
	if !strings.Contains(out, `aria-controls="allthree-panel-1"`) {
		t.Errorf("aria-controls must survive vacate:\n%s", out)
	}
	if !strings.Contains(out, `data-fui-tab-index="1" data-state="inactive" id="allthree-tab-1"`) {
		t.Errorf("data-state must survive vacate:\n%s", out)
	}
}
