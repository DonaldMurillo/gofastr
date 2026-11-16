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
	// Href overrides the fragment href with a same-origin deep link.
	Href string
	// Disabled renders the tab with aria-disabled and without its signal write or href.
	Disabled bool
}

// TabsProps configures a tab strip.
type TabsProps struct {
	// Name is the signal the selection lives in. Required.
	Name string
	// Tabs, in order. Required, at least one.
	Tabs []Tab
	// Active is the selected tab's index; out of range takes 0.
	Active int
	// VacateHidden ships hidden panels empty, their content in a JSON stash the module restores on first show.
	VacateHidden bool
	// StateAttrs mirrors data-state active or inactive onto every tab.
	StateAttrs bool
	// ID prefixes the tab and panel ids (<ID>-tab-<n>, <ID>-panel-<n>); empty takes Name.
	ID         string
	ExtraAttrs html.Attrs

	Parts Parts

	// Ctx is the render context whose id scope suffixes a derived id; an explicit ID is never changed.
	Ctx context.Context
}

const tabsMaxPanels = 24

// TabsMaxPanels is the most tabs a strip renders, the count the styled layer's CSS covers.
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
	var tabs []render.HTML
	for i, tab := range p.Tabs {
		own := html.Attrs{
			"role":                "tab",
			"id":                  p.ID + "-tab-" + strconv.Itoa(i),
			"aria-selected":       strconv.FormatBool(i == p.Active),
			"tabindex":            tabTabIndex(i == p.Active),
			"data-cui-signal-set": p.Name + ":" + strconv.Itoa(i),
			"data-cui-tab-index":  strconv.Itoa(i),
		}
		own["aria-controls"] = p.ID + "-panel-" + strconv.Itoa(i)
		if tab.Disabled {
			own["aria-disabled"] = "true"
			delete(own, "data-cui-signal-set")
		}
		href := tab.Href
		fragment := "#" + p.ID + "-panel-" + strconv.Itoa(i)
		if href == "" {
			href = fragment
		} else if !strings.HasPrefix(href, "#") {
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

	panelsRenderContent := false
	for i, t := range p.Tabs {
		if t.Panel != "" && !(p.VacateHidden && i != p.Active) {
			panelsRenderContent = true
			break
		}
	}

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

	panelsOwn := html.Attrs(nil)
	if !panelsRenderContent {
		panelsOwn = Internal(nil)
	}
	return b.El("div", PartRoot, rootAttrs,
		b.El("nav", PartTabsNav, Internal(html.Attrs{"role": "tablist"}), tabs...),
		b.El("div", PartTabsPanel, panelsOwn, panelChildren...),
	)
}

func tabTabIndex(active bool) string {
	if active {
		return "0"
	}
	return "-1"
}

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
