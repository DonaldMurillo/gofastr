package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// registerNewComponentsDemos wires the runtime backing for the
// /components/new screen — the ConfirmAction modal, the CommandPalette
// modal, and tiny RPC handlers that drive Combobox search, infinite
// scroll, tree lazy-load, and filter dismiss.
//
// All widgets are page-scoped to /components/new via .Pages so other
// pages don't pay their registration weight.
func registerNewComponentsDemos(fwApp *framework.App) {
	r := fwApp.Router

	// ConfirmAction modal — page-scoped to /components/confirmaction.
	_, confirmBuilder := ui.ConfirmAction(ui.ConfirmActionConfig{
		Name:           "demo-confirm-delete",
		TriggerLabel:   "Delete account",
		Title:          "Delete account?",
		Body:           "This action cannot be undone. Your data will be permanently removed.",
		ConfirmLabel:   "Delete",
		CancelLabel:    "Keep account",
		RPCPath:        "/islands/new-components/confirm-delete",
		TriggerVariant: "danger",
	})
	confirmBuilder = confirmBuilder.Pages("/components/confirmaction")
	confirmDef := confirmBuilder.Build()
	widget.Mount(r, &confirmDef)
	r.Post("/islands/new-components/confirm-delete", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Demo only — pretend we deleted something.
		ui.AddToastSuccess(w, "Deleted", "Account has been removed.", 5000)
		w.WriteHeader(http.StatusNoContent)
	}))

	// AvatarGroup demo Popover — page-scoped to /components/avatargroup.
	// Anchored to the "View team" trigger button on that demo page.
	teamPopover := preset.Popover("demo-team-popover").
		Pages("/components/avatargroup").
		LabelledBy("demo-team-popover-title").
		Slot("body", teamPopoverBody{}).
		Build()
	widget.Mount(r, &teamPopover)

	// CommandPalette — page-scoped to /components/commandpalette.
	_, paletteBuilder := ui.CommandPalette(ui.CommandPaletteConfig{
		Name:    "demo-command-palette",
		RPCPath: "/islands/new-components/palette-search",
	})
	paletteBuilder = paletteBuilder.Pages("/components/commandpalette")
	paletteDef := paletteBuilder.Build()
	widget.Mount(r, &paletteDef)
	r.Post("/islands/new-components/palette-search", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		q := strings.ToLower(formField(req, "q"))
		all := []string{"Open settings", "Toggle theme", "Sign out", "Customers", "Documentation", "Help & support"}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		matched := 0
		for i, cmd := range all {
			if q == "" || strings.Contains(strings.ToLower(cmd), q) {
				_, _ = w.Write([]byte(`<li role="option" id="palette-opt-` + strconv.Itoa(i) + `" data-value="` + cmd + `">` + cmd + `</li>`))
				matched++
			}
		}
		if matched == 0 {
			_, _ = w.Write([]byte(`<li role="option" aria-disabled="true">No commands match</li>`))
		}
	}))

	// Combobox: cities search.
	r.Post("/islands/new-components/cities-search", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		q := strings.ToLower(formField(req, "q"))
		cities := []string{"Amsterdam", "Berlin", "Copenhagen", "Dublin", "Edinburgh", "Florence", "Geneva", "Helsinki", "Istanbul", "Jakarta"}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		matched := 0
		for i, c := range cities {
			if q == "" || strings.HasPrefix(strings.ToLower(c), q) {
				_, _ = w.Write([]byte(`<li role="option" id="city-opt-` + strconv.Itoa(i) + `" data-value="` + c + `">` + c + `</li>`))
				matched++
			}
		}
		if matched == 0 {
			_, _ = w.Write([]byte(`<li role="option" aria-disabled="true">No cities match</li>`))
		}
	}))

	// InfiniteScroll: feed page handler. 100 posts total so the user
	// actually has something to scroll through; the demo's scroll
	// container is fixed-height so each fetch lands when the sentinel
	// scrolls into the container's viewport.
	const feedTotal = 100
	const feedPageSize = 10
	r.Post("/islands/new-components/feed-page", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		cursor, _ := strconv.Atoi(formField(req, "cursor"))
		if cursor <= 0 {
			cursor = 5
		}
		// Headers MUST be set BEFORE the first Write — once Go's HTTP
		// stack auto-sends the header block, any subsequent
		// header.Set() is silently dropped.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		next := cursor + feedPageSize
		atEnd := cursor >= feedTotal
		if !atEnd && next <= feedTotal {
			w.Header().Set("X-Gofastr-Infinite-Cursor", strconv.Itoa(next))
		}
		if atEnd {
			w.WriteHeader(http.StatusOK)
			return
		}
		for i := cursor + 1; i <= cursor+feedPageSize && i <= feedTotal; i++ {
			_, _ = w.Write([]byte(`<article class="demo-feed-item"><h3>Post ` + strconv.Itoa(i) + `</h3><p>Lazy-loaded entry — fetched when the sentinel scrolled into the container.</p></article>`))
		}
	}))

	// Tree lazy-load handler.
	r.Post("/islands/new-components/tree-load", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Return three child treeitems for the lazy "vendor" node.
		for _, name := range []string{"library-a", "library-b", "library-c"} {
			_, _ = w.Write([]byte(`<li id="vendor-` + name + `" role="treeitem" aria-level="2" tabindex="-1" class="tree__item"><div class="tree__row"><span class="tree__label">` + name + `</span></div></li>`))
		}
	}))

	// FilterChipBar: remove + clear-all backed by per-process state so
	// the bar actually re-renders after each dismiss. Demo only — in a
	// real app you'd key off the session / current user / query state.
	r.Post("/islands/new-components/filter-remove", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		key := formField(req, "key")
		filterDemoState.remove(key)
		writeFilterBarHTML(w)
	}))
	r.Post("/islands/new-components/filter-clear", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		filterDemoState.clear()
		writeFilterBarHTML(w)
	}))
	r.Post("/islands/new-components/filter-reset", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		filterDemoState.reset()
		writeFilterBarHTML(w)
	}))
}

// filterDemoState is the in-memory active-filter set the demo uses to
// drive the FilterChipBar dismiss + Clear all round-trip. It's reset
// to a default seed via /islands/new-components/filter-reset so users
// can play with it repeatedly.
var filterDemoState = newDemoFilters()

type demoFilters struct {
	mu     sync.Mutex
	active map[string]demoFilterEntry
	order  []string
}

type demoFilterEntry struct {
	Label   string
	Variant ui.StatusVariant
}

func newDemoFilters() *demoFilters {
	d := &demoFilters{}
	d.reset()
	return d
}

func (d *demoFilters) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.active = map[string]demoFilterEntry{
		"status": {"Status: Active", ui.StatusSuccess},
		"tag":    {"Tag: urgent", ui.StatusWarning},
		"owner":  {"Owner: Alice", ui.StatusNeutral},
	}
	d.order = []string{"status", "tag", "owner"}
}

func (d *demoFilters) remove(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.active[key]; !ok {
		return
	}
	delete(d.active, key)
	next := d.order[:0]
	for _, k := range d.order {
		if k != key {
			next = append(next, k)
		}
	}
	d.order = next
}

func (d *demoFilters) clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.active = map[string]demoFilterEntry{}
	d.order = nil
}

func (d *demoFilters) snapshot() []ui.FilterChip {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]ui.FilterChip, 0, len(d.order))
	for _, k := range d.order {
		e := d.active[k]
		out = append(out, ui.FilterChip{
			Label:       e.Label,
			Variant:     e.Variant,
			DismissPath: "/islands/new-components/filter-remove?key=" + k,
		})
	}
	return out
}

func writeFilterBarHTML(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	bar := ui.FilterChipBar(ui.FilterChipBarConfig{
		ID:           "filter-bar-demo",
		Label:        "Active filters",
		Filters:      filterDemoState.snapshot(),
		ClearAllPath: "/islands/new-components/filter-clear",
		RPCSignal:    "filter-bar-demo",
		SignalName:   "filter-bar-demo",
	})
	_, _ = w.Write([]byte(bar))
}

// teamPopoverBody renders the slot HTML for the AvatarGroup page's
// Popover demo — a small "team roster" list showcasing how Tooltips
// + Popovers compose with the avatar stack.
type teamPopoverBody struct{}

func (teamPopoverBody) Render() render.HTML {
	team := []struct{ Name, Role string }{
		{"Ada Lovelace", "Compiler engineer"},
		{"Grace Hopper", "Compiler engineer"},
		{"Alan Turing", "Architect"},
		{"Edsger Dijkstra", "Algorithms"},
		{"Margaret Hamilton", "Reliability"},
		{"Linus Torvalds", "Kernel maintainer"},
	}
	rows := make([]render.HTML, 0, len(team)+1)
	rows = append(rows, html.Heading(html.HeadingConfig{
		Level: 3, ID: "demo-team-popover-title", Class: "demo-team-title",
	}, render.Text("Project team")))
	listItems := make([]render.HTML, 0, len(team))
	for _, m := range team {
		listItems = append(listItems, render.Tag("li", map[string]string{"class": "demo-team-row"},
			ui.Avatar(ui.AvatarConfig{Name: m.Name, Size: ui.AvatarSm}),
			html.Span(html.TextConfig{Class: "demo-team-name"}, render.Text(m.Name)),
			html.Span(html.TextConfig{Class: "demo-team-role"}, render.Text(m.Role)),
		))
	}
	rows = append(rows, render.Tag("ul", map[string]string{"class": "demo-team-list"}, listItems...))
	return html.Div(html.DivConfig{Class: "demo-team-popover"}, rows...)
}

var _ component.Component = teamPopoverBody{}

// formField returns the first value of `name` regardless of how the
// runtime POSTed it: application/json (default dispatchRPC for
// non-multipart forms), application/x-www-form-urlencoded (InfiniteScroll
// and other manual form-encoded POSTs), or query-string. Tries each
// source in turn so a single handler works with every transport the
// runtime might use.
func formField(req *http.Request, name string) string {
	contentType := req.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		var body map[string]any
		dec := json.NewDecoder(req.Body)
		if err := dec.Decode(&body); err == nil {
			if v, ok := body[name]; ok {
				switch t := v.(type) {
				case string:
					if t != "" {
						return t
					}
				case float64:
					return strconv.FormatFloat(t, 'f', -1, 64)
				case bool:
					if t {
						return "true"
					}
					return "false"
				}
			}
		}
	}
	if err := req.ParseForm(); err == nil {
		if v := req.PostForm.Get(name); v != "" {
			return v
		}
	}
	if v := req.URL.Query().Get(name); v != "" {
		return v
	}
	body, _ := url.ParseQuery(req.URL.RawQuery)
	return body.Get(name)
}
