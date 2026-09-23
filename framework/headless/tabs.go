package headless

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The tabs: a tablist of anchors and a stack of panels. Selection is
// the kernel's signal contract — each tab carries data-fui-signal-set
// beside its fragment href, the wrapper mirrors the signal into
// data-active, and CSS lights the panel — so with script the switch is
// client-side and without script the fragment href lands on the panel
// the server rendered selected. The module owns the keyboard contract:
// one tab is roving-tabindex 0, ArrowLeft/ArrowRight are RTL-aware
// and select on focus, Home/End jump, and Tab leaves the strip
// naturally.

// Tabs parts.
const (
	PartTab       Part = "tab"
	PartTabPanel  Part = "tabpanel"
	PartTabsNav   Part = "tabs-nav"
	PartTabsPanel Part = "tabs-panels"
)

// Tab is one tab: its label and its panel's content.
type Tab struct {
	// Label is the tab's visible text. Required.
	Label string
	// Panel is the tab's panel content.
	Panel render.HTML
	// Href overrides the fragment href (a same-origin deep link).
	Href string
	// Disabled removes the tab from the keyboard rotation.
	Disabled bool
}

// TabsProps configures a tab strip.
type TabsProps struct {
	// Name is the signal the selection lives in. Required: the strip's
	// whole contract is that clicking a tab writes the signal and the
	// wrapper mirrors it.
	Name string
	// Tabs, in order. Required, at least one.
	Tabs []Tab
	// Active is the selected tab's index. Out-of-range takes 0.
	Active int
	// VacateHidden ships hidden panels EMPTY with their content in an
	// adjacent JSON stash the module restores on first show, so
	// page-scoped locators cannot match hidden text.
	VacateHidden bool
	// StateAttrs mirrors data-state="active"/"inactive" onto every tab
	// after client-side switches, the attribute Radix-style ports pin
	// their locators to.
	StateAttrs bool
	// ID is the prefix the tab and panel ids derive from ("<ID>-tab-<n>"
	// and "<ID>-panel-<n>"). Empty takes Name; a page with two strips
	// on one signal-distinct name can still disambiguate ids with it.
	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the root, the tabs and the panels.
	Parts Parts
}

// tabsMaxPanels bounds the generated per-index CSS the styled layer
// emits; refuse beyond it loudly rather than hiding panels.
const tabsMaxPanels = 24

// TabsMaxPanels exposes the ceiling to the styled layer, whose
// generated CSS covers exactly this many indices.
func TabsMaxPanels() int { return tabsMaxPanels }

// Tabs renders the tab strip.
func Tabs(p TabsProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: Tabs requires Name — the selection lives in the signal; without it clicking a tab does nothing")
	}
	checkSignalName(p.Name)
	if len(p.Tabs) == 0 {
		panic("headless: Tabs requires at least one tab — an empty tablist is a landmark with nothing in it")
	}
	if len(p.Tabs) > tabsMaxPanels {
		panic("headless: Tabs supports at most " + strconv.Itoa(tabsMaxPanels) + " tabs, got " + strconv.Itoa(len(p.Tabs)))
	}
	if p.Active < 0 || p.Active >= len(p.Tabs) {
		p.Active = 0
	}
	for i, tab := range p.Tabs {
		if scrubControlBytes(tab.Label) == "" {
			panic("headless: Tab " + strconv.Itoa(i) + " has no Label — a tab nobody can read is a control with no name")
		}
	}

	if p.ID == "" {
		p.ID = p.Name
	}
	b := p.Parts.Box(s)
	// Roving tabindex: the active tab is in the tab order, the rest
	// are reached by the arrows.
	var tabs []render.HTML
	for i, tab := range p.Tabs {
		own := html.Attrs{
			"role":                "tab",
			"id":                  p.ID + "-tab-" + strconv.Itoa(i),
			"aria-selected":       strconv.FormatBool(i == p.Active),
			"tabindex":            tabTabIndex(i == p.Active),
			"data-fui-signal-set": p.Name + ":" + strconv.Itoa(i),
			// The per-index pair the styled layer's generated CSS keys
			// the active highlight and the visible panel on.
			"data-fui-tab-index": strconv.Itoa(i),
		}
		own["aria-controls"] = p.ID + "-panel-" + strconv.Itoa(i)
		if tab.Disabled {
			own["aria-disabled"] = "true"
		}
		href := tab.Href
		fragment := "#" + p.ID + "-panel-" + strconv.Itoa(i)
		if href == "" {
			href = fragment
		} else if !strings.HasPrefix(href, "#") {
			// A same-origin deep link is fine; an unsafe one degrades
			// to the FRAGMENT href the tab would have had — never to
			// empty, which would point the anchor at the current page.
			href = cleanTabHref(href)
			if href == "" || href == "#" {
				href = fragment
			}
		}
		own["href"] = href
		if p.StateAttrs {
			if i == p.Active {
				own["data-state"] = "active"
			} else {
				own["data-state"] = "inactive"
			}
		}
		tabs = append(tabs, b.El("a", PartTab, own, render.Text(scrubControlBytes(tab.Label))))
	}

	// VacateHidden parks every inactive panel's content in the stash.
	stash := map[string]string{}
	var panels []render.HTML
	for i, tab := range p.Tabs {
		own := html.Attrs{
			"role":               "tabpanel",
			"id":                 p.ID + "-panel-" + strconv.Itoa(i),
			"aria-labelledby":    p.ID + "-tab-" + strconv.Itoa(i),
			"tabindex":           "0",
			"data-fui-tab-index": strconv.Itoa(i),
		}
		if p.VacateHidden && i != p.Active {
			stash[strconv.Itoa(i)] = string(tab.Panel)
			panels = append(panels, b.El("div", PartTabPanel, own))
			continue
		}
		panels = append(panels, b.El("div", PartTabPanel, own, tab.Panel))
	}
	panelChildren := panels
	if len(stash) > 0 {
		if buf, err := json.Marshal(stash); err == nil {
			enc := strings.ReplaceAll(string(buf), `</`, `<\/`)
			panelChildren = append(panelChildren, render.Tag("script", html.Attrs{
				"type":                "application/json",
				"data-hui-tabs-stash": "true",
			}, render.HTML(enc)))
		}
	}

	rootAttrs := Merge(Safe(p.ExtraAttrs, "data-active"), html.Attrs{
		"data-fui-signal":      p.Name,
		"data-fui-signal-mode": "attr",
		"data-fui-signal-attr": "data-active",
		"data-active":          strconv.Itoa(p.Active),
	})
	Mark(rootAttrs, "data-hui-tabs")
	if p.StateAttrs {
		Mark(rootAttrs, "data-hui-tabs-state")
	}
	if p.VacateHidden {
		Mark(rootAttrs, "data-hui-tabs-vacate")
	}

	return b.El("div", PartRoot, rootAttrs,
		b.El("nav", PartTabsNav, html.Attrs{"role": "tablist"}, tabs...),
		b.El("div", PartTabsPanel, nil, panelChildren...),
	)
}

// tabTabIndex spells the roving contract.
func tabTabIndex(active bool) string {
	if active {
		return "0"
	}
	return "-1"
}

// cleanTabHref routes a tab's deep-link href through the anchor policy,
// degrading to the fragment.
func cleanTabHref(href string) string {
	return urlsafe.CleanAnchor(href)
}

func init() {
	Register(Spec{
		Name:    "Tabs",
		Anatomy: []Part{PartRoot, PartTabsNav, PartTab, PartTabsPanel, PartTabPanel},
		Hooks:   []string{"data-hui-tabs", "data-hui-tabs-state", "data-hui-tabs-vacate", "data-hui-tabs-stash"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Tabs(TabsProps{Name: "demo", Tabs: []Tab{
				{Label: "Overview", Panel: render.Text("One")},
				{Label: "Details", Panel: render.Text("Two")},
			}, Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a signal tab strip",
				Why:  "one tab is in the tab order and the rest are reached by the arrows; the panels are real content and the fragment hrefs land on them without script",
				HTML: Tabs(TabsProps{Name: "cfg", Tabs: []Tab{
					{Label: "General", Panel: render.Text("The general panel.")},
					{Label: "Secrets", Panel: render.Text("The secrets panel.")},
				}}, s),
			}, {
				Name: "vacated and stateful",
				Why:  "hidden panels ship empty with their content in the stash the module restores on first show, and every tab carries the state attribute ports pin locators to",
				HTML: Tabs(TabsProps{Name: "vac", Active: 1, VacateHidden: true, StateAttrs: true, Tabs: []Tab{
					{Label: "One", Panel: render.Text("First panel.")},
					{Label: "Two", Panel: render.Text("Second panel.")},
				}}, s),
			}}
		},
	})
}
