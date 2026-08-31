package ui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// TabItem is a single tab with a label and content.
type TabItem struct {
	Label   string
	Content render.HTML
}

// TabsConfig configures a signal-driven tab strip.
type TabsConfig struct {
	SignalName string            // required unless Slice is set
	Slice      *store.Slice[int] // optional; supplies the signal name + initial active index, takes precedence
	Tabs       []TabItem         // required, at least 1
	Class      string            // optional extra CSS class

	// StateAttrs adds data-state="active"/"inactive" to every tab
	// button and data-fui-tabs-state to the wrapper, the attribute
	// contract Radix-style ports pin their test locators to. The
	// demand-loaded tabs runtime module (pulled in by the wrapper's
	// data-fui-prefetch marker) keeps data-state in step with
	// data-active after client-side switches, mirroring what core does
	// for aria-selected. Zero value: no data-state anywhere, output
	// byte-identical to a config without the knob.
	StateAttrs bool

	// ID wires tab↔panel semantics: each button gets id
	// "<ID>-tab-<i>" plus aria-controls "<ID>-panel-<i>", each panel
	// gets id "<ID>-panel-<i>". Empty (zero value): no ids, no
	// aria-controls, output byte-identical to a config without the
	// knob. The caller owns cross-page uniqueness of ID, same as any
	// other HTML id.
	ID string

	// VacateHidden ships hidden panels EMPTY, their server-rendered
	// content parked in an adjacent <script type="application/json"
	// data-fui-tabs-stash> (tab index → HTML), so page-scoped test
	// locators cannot match text inside hidden panels — the DOM parity
	// of a port whose source component unmounts inactive panels. The
	// tabs runtime module restores a panel's content on first show
	// (from the stash) and from then on moves the live nodes out and
	// back on every switch, so anything the runtime swapped into the
	// panel (island updates, form state) survives re-show intact.
	// Trade-offs: while a panel is vacated its content is detached, so
	// document-scoped updates targeting it (SSE island pushes, RPC
	// responses for controls it owns) are dropped permanently — nothing
	// is queued for replay; re-show resurrects the panel's pre-vacate
	// nodes, and only updates that arrive after re-show land. Focus
	// inside a vacated panel escapes to <body>.
	//
	// One timing caveat worth knowing before you switch this on: the
	// module that restores content loads on first hover/focus of the
	// strip, so a PROGRAMMATIC switch — any signal write that reaches
	// the strip before the module loads: an SSE, poll, or RPC-driven
	// update, or a hydration-time signal value differing from what SSR
	// rendered — on a strip nobody has touched yet shows an empty panel
	// until the first interaction heals it. SSR is unaffected — the
	// initially-active panel always ships with its content, so server-
	// rendered deep links, anchors, restored scroll, and autofocus are
	// fine — this only reaches apps that move tabs without the user
	// touching them. If that is your shape, leave VacateHidden off.
	//
	// Zero value: all panels ship in the DOM as today, output
	// byte-identical to a config without the knob.
	VacateHidden bool

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the tab strip's root
	// wrapper div. Keys the component owns are dropped: class and id
	// (use Class), data-fui-*, and data-active (the runtime mirrors
	// the active tab index there).
	ExtraAttrs html.Attrs
}

var tabsStyle = registry.RegisterStyle("fui-tabs", tabsCSS)

// tabsMaxPanels bounds how many tab indices the generated CSS covers.
// Both the active-button highlight and the visible-panel rule are
// emitted per index (the registered CSS is global: it can't know a
// given strip's tab count), so we cover a generous fixed ceiling and
// reject anything beyond it loudly rather than silently hiding panels.
const tabsMaxPanels = 24

// Tabs renders a signal-driven tab strip. Clicking a tab sets the signal;
// the runtime mirrors it to data-active on the wrapper, and CSS lights up
// both the matching button and panel, so the highlight moves with the
// selection.
//
// Panics if SignalName is empty, Tabs is empty, or there are more than
// tabsMaxPanels tabs.
func Tabs(cfg TabsConfig) render.HTML {
	name := cfg.SignalName
	active := 0
	if cfg.Slice != nil {
		name = cfg.Slice.Name()
		active = cfg.Slice.Default()
	}
	if name == "" {
		panic("ui: Tabs requires SignalName or Slice")
	}
	if len(cfg.Tabs) == 0 {
		panic("ui: Tabs requires at least one TabItem")
	}
	if len(cfg.Tabs) > tabsMaxPanels {
		panic(fmt.Sprintf("ui: Tabs supports at most %d tabs, got %d", tabsMaxPanels, len(cfg.Tabs)))
	}
	if active < 0 || active >= len(cfg.Tabs) {
		active = 0
	}

	// Contract knobs. Every one is opt-in and additive; with all three
	// at their zero value the attribute maps below are exactly what
	// they were before these knobs existed (pinned by
	// TestTabsZeroValueOutputPinned).
	wireIDs := cfg.ID != ""

	var buttons []render.HTML
	for i, tab := range cfg.Tabs {
		btnAttrs := map[string]string{
			"class":               "fui-tab",
			"data-fui-signal-set": name + ":" + strconv.Itoa(i),
			"role":                "tab",
			"aria-selected":       strconv.FormatBool(i == active),
			"data-fui-tab-index":  strconv.Itoa(i),
		}
		if cfg.StateAttrs {
			if i == active {
				btnAttrs["data-state"] = "active"
			} else {
				btnAttrs["data-state"] = "inactive"
			}
		}
		if wireIDs {
			btnAttrs["id"] = cfg.ID + "-tab-" + strconv.Itoa(i)
			btnAttrs["aria-controls"] = cfg.ID + "-panel-" + strconv.Itoa(i)
		}
		buttons = append(buttons, render.Tag("button", btnAttrs, render.Text(tab.Label)))
	}

	// VacateHidden parks every inactive panel's content in the stash
	// script below and ships the panel itself empty; the tabs module
	// restores it on first show. The active panel ships in the DOM
	// exactly as a non-vacate strip would.
	stash := map[string]string{}
	var panels []render.HTML
	for i, tab := range cfg.Tabs {
		panelAttrs := map[string]string{
			"class":              "fui-tab-panel",
			"role":               "tabpanel",
			"data-fui-tab-index": strconv.Itoa(i),
		}
		if wireIDs {
			panelAttrs["id"] = cfg.ID + "-panel-" + strconv.Itoa(i)
		}
		if cfg.VacateHidden && i != active {
			stash[strconv.Itoa(i)] = string(tab.Content)
			panels = append(panels, render.Tag("div", panelAttrs))
			continue
		}
		panels = append(panels, render.Tag("div", panelAttrs, tab.Content))
	}

	contentChildren := panels
	if len(stash) > 0 {
		// Same stash idiom as Carousel's deferred-slide manifest:
		// json.Marshal escapes <, >, & (so no entity surprises) and the
		// </ → <\/ rewrite stops an embedded "</script>" in panel
		// content from terminating this inline script early.
		if buf, err := json.Marshal(stash); err == nil {
			s := strings.ReplaceAll(string(buf), `</`, `<\/`)
			contentChildren = append(contentChildren, render.Tag("script", map[string]string{
				"type":                "application/json",
				"data-fui-tabs-stash": "true",
			}, render.HTML(s)))
		}
	}

	nav := render.Tag("nav", map[string]string{
		"class": "fui-tabs-nav",
		"role":  "tablist",
	}, buttons...)

	content := render.Tag("div", map[string]string{
		"class": "fui-tabs-content",
	}, contentChildren...)

	cls := "fui-tabs"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	// The signal binding lives on the outer wrapper, the common ancestor
	// of the nav buttons and the panels, so one data-active drives both.
	wrapperAttrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "data-active")
	if wrapperAttrs == nil {
		wrapperAttrs = html.Attrs{}
	}
	wrapperAttrs["class"] = cls
	wrapperAttrs["data-fui-signal"] = name
	wrapperAttrs["data-fui-signal-mode"] = "attr"
	wrapperAttrs["data-fui-signal-attr"] = "data-active"
	wrapperAttrs["data-active"] = strconv.Itoa(active)
	if cfg.StateAttrs {
		wrapperAttrs["data-fui-tabs-state"] = "true"
	}
	if cfg.VacateHidden {
		wrapperAttrs["data-fui-tabs-vacate"] = "true"
	}
	if cfg.StateAttrs || cfg.VacateHidden {
		// The tabs module is interaction-time behavior (mirroring
		// data-state after a switch, restoring vacated content). The
		// kernel's prefetch bridge demand-loads it on the first
		// pointerover/focusin of the strip, so it is in place by the
		// first click without costing the core bundle a scanner entry
		// (core gzip headroom is ~13 bytes at its binding level-1 budget
		// line; see core-ui/runtime/budget_test.go).
		wrapperAttrs["data-fui-prefetch"] = "tabs"
	}
	wrapper := render.Tag("div", wrapperAttrs, nav, content)

	return tabsStyle.WrapHTML(wrapper)
}

func tabsCSS(_ style.Theme) string {
	var b strings.Builder
	// WrapHTML stamps data-fui-comp onto the wrapper itself, so the
	// wrapper-targeting rules are compound (no descendant combinator).
	b.WriteString(`[data-fui-comp="fui-tabs"].fui-tabs{margin:0}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tabs-nav{display:flex;gap:0;border-bottom:1px solid var(--fui-border, var(--color-border, #e2e8f0));margin-bottom:0}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tab{padding:var(--spacing-md, .5rem) var(--spacing-lg, 1rem);background:none;border:none;border-bottom:2px solid transparent;cursor:pointer;font-size:var(--text-sm, .875rem);font-weight:500;color:var(--fui-muted, var(--color-text-muted, #64748b));transition:color .15s,border-color .15s}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tab:hover{color:var(--fui-foreground, var(--color-text, #0f172a))}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tab:focus-visible{outline:2px solid var(--fui-primary, var(--color-primary, #3b82f6));outline-offset:-2px;border-radius:2px}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tabs-content{padding-top:1rem}`)
	b.WriteString(`[data-fui-comp="fui-tabs"] .fui-tab-panel{display:none}`)
	// Active button + visible panel both keyed off the wrapper's
	// data-active so the highlight follows the selected tab.
	for i := range tabsMaxPanels {
		b.WriteString(fmt.Sprintf(`[data-fui-comp="fui-tabs"][data-active="%d"] .fui-tab[data-fui-tab-index="%d"]{color:var(--fui-primary, var(--color-primary, #3b82f6));border-bottom-color:var(--fui-primary, var(--color-primary, #3b82f6))}`, i, i))
		b.WriteString(fmt.Sprintf(`[data-fui-comp="fui-tabs"][data-active="%d"] .fui-tab-panel[data-fui-tab-index="%d"]{display:block}`, i, i))
	}
	return b.String()
}
