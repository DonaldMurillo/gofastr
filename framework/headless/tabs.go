package headless

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The tabs: a tablist of anchors and a stack of panels. Selection is
// the kernel's signal contract — each tab carries data-cui-signal-set
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
	// Disabled removes the tab from the keyboard rotation and makes a
	// click do nothing: the tab renders with aria-disabled and without
	// its signal write or its href (Href is ignored).
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
	// Active is the selected tab's index. Out-of-range (negative, or
	// past the last tab) takes 0, and the headless-tabs module writes 0
	// back into the signal when the store holds no tab of this strip (a
	// seed of 5 on a three-tab strip), so the paint and the store agree.
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

	// Ctx is the render context. An id this component derives (no ID
	// given) is handed out through its id scope (render.UniqueID), so a
	// repeat on the page gets a -2 suffix; an explicit ID is never
	// changed. With no Ctx the derived id is used as is.
	Ctx context.Context
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

	// The family: every tab's and panel's id is built from p.ID.
	family := make([]string, 0, 2*len(p.Tabs))
	for i := range p.Tabs {
		family = append(family, "-tab-"+strconv.Itoa(i), "-panel-"+strconv.Itoa(i))
	}
	if p.ID == "" {
		p.ID = render.UniqueID(p.Ctx, p.Name, family...)
	} else {
		render.ReserveID(p.Ctx, p.ID, family...)
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
			"data-cui-signal-set": p.Name + ":" + strconv.Itoa(i),
			// The per-index pair the styled layer's generated CSS keys
			// the active highlight and the visible panel on.
			"data-cui-tab-index": strconv.Itoa(i),
		}
		own["aria-controls"] = p.ID + "-panel-" + strconv.Itoa(i)
		if tab.Disabled {
			// A disabled anchor is not a thing in HTML: the signal write
			// and the href go, so a click neither selects the tab nor
			// follows its link, and aria-disabled says why.
			own["aria-disabled"] = "true"
			delete(own, "data-cui-signal-set")
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
		if !tab.Disabled {
			own["href"] = href
		}
		if p.StateAttrs {
			if i == p.Active {
				own["data-state"] = "active"
			} else {
				own["data-state"] = "inactive"
			}
		}
		tabs = append(tabs, b.El("a", PartTab, own, render.Text(scrubControlBytes(tab.Label))))
	}

	// panelsRenderContent says whether any panel actually renders its
	// Panel as a DOM child — the only caller content a panel can hold.
	// A vacated panel never does (its content moves to the stash
	// below, inert JSON text, not markup). When no panel renders any,
	// the whole panels wrapper is marked as a unit below; otherwise a
	// panel that itself renders nothing marks itself instead, since
	// its siblings may still hold the caller's.
	panelsRenderContent := false
	for i, t := range p.Tabs {
		if t.Panel != "" && !(p.VacateHidden && i != p.Active) {
			panelsRenderContent = true
			break
		}
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
			"data-cui-tab-index": strconv.Itoa(i),
		}
		vacated := p.VacateHidden && i != p.Active
		rendersContent := tab.Panel != "" && !vacated
		if panelsRenderContent && !rendersContent {
			own = Internal(own)
		}
		if vacated {
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
			// The stash is inert JSON, none of it markup: it is marked
			// here unless the wrapper's own mark below already covers it.
			// Each branch keeps its attrs a literal at the call so the
			// inline-script lint sees the inert type.
			el := render.Tag("script", html.Attrs{
				"type":                "application/json",
				"data-hui-tabs-stash": "true",
			}, render.HTML(enc))
			if panelsRenderContent {
				el = render.Tag("script", html.Attrs{
					"type":                "application/json",
					"data-hui-tabs-stash": "true",
					"data-cui-internal":   "",
				}, render.HTML(enc))
			}
			panelChildren = append(panelChildren, el)
		}
	}

	rootAttrs := Merge(Safe(p.ExtraAttrs, "data-active"), html.Attrs{
		"data-cui-signal":      p.Name,
		"data-cui-signal-mode": "attr",
		"data-cui-signal-attr": "data-active",
		"data-active":          strconv.Itoa(p.Active),
	})
	Mark(rootAttrs, "data-hui-tabs")
	if p.StateAttrs {
		Mark(rootAttrs, "data-hui-tabs-state")
	}
	if p.VacateHidden {
		Mark(rootAttrs, "data-hui-tabs-vacate")
	}

	// Every tab is built from Label (a string) plus generated attrs, so
	// the whole tablist is always this component's own.
	panelsOwn := html.Attrs(nil)
	if !panelsRenderContent {
		panelsOwn = Internal(nil)
	}
	return b.El("div", PartRoot, rootAttrs,
		b.El("nav", PartTabsNav, Internal(html.Attrs{"role": "tablist"}), tabs...),
		b.El("div", PartTabsPanel, panelsOwn, panelChildren...),
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
