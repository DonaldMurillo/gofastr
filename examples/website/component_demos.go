package main

import (
	"encoding/json"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// registerComponentDemos wires the hidden widgets + toast bus that the
// /components/{modal,drawer,toast} screens drive. Called once at app
// startup; no per-request state.
func registerComponentDemos(fwApp *framework.App) {
	r := fwApp.Router

	// --- Modal demos --------------------------------------------------
	// Page-scoped: only available on /components/modal so other
	// pages don't ship references to demo-only widgets. Slot bodies
	// are Components composed from html.* / ui.* — no raw HTML.
	confirm := preset.Modal("components-confirm").
		Hidden().
		Pages("/components/modal").
		LabelledBy("components-confirm-title").
		DescribedBy("components-confirm-desc").
		Slot("body", confirmModalBody{}).
		Build()
	widget.Mount(r, &confirm)

	userEdit := preset.Modal("components-user-edit").
		Hidden().
		Pages("/components/modal").
		DeepLink("modal", "user-edit").
		DeepLinkParam("user_id").
		LabelledBy("components-user-edit-title").
		Signal("user_id", widget.SignalFunc(func() (any, error) { return "", nil })).
		Slot("body", userEditModalBody{}).
		Build()
	widget.Mount(r, &userEdit)

	// --- Drawer demos -------------------------------------------------
	// Page-scoped: only relevant on /components/drawer.
	plainDrawer := preset.Drawer("components-drawer").
		Hidden().
		Pages("/components/drawer").
		LabelledBy("components-drawer-title").
		Slot("body", quickNavDrawerBody{}).
		Build()
	widget.Mount(r, &plainDrawer)

	filters := preset.Drawer("components-filter-drawer").
		Hidden().
		Pages("/components/drawer").
		DeepLink("drawer", "filters").
		DeepLinkParam("status").
		DeepLinkParam("tag").
		LabelledBy("components-filter-drawer-title").
		Signal("status", widget.SignalFunc(func() (any, error) { return "", nil })).
		Signal("tag", widget.SignalFunc(func() (any, error) { return "", nil })).
		Slot("body", filterDrawerBody{}).
		Build()
	widget.Mount(r, &filters)

	// --- Toast stacks (6 positions) -----------------------------------
	// One stack per anchor point: top-left, top-center, top-right,
	// bottom-left, bottom-center, bottom-right. Apps typically only
	// register one (or zero — __gofastr.toast() will auto-create a
	// container when none exists), but the demo registers all six so
	// users can click a corner and see toasts fire there.
	// site-toasts is the catch-all default stack — global so any
	// server-header push reaches a mounted container regardless of
	// which page the user is on.
	siteToasts := preset.ToastStack("site-toasts").Build()
	widget.Mount(r, &siteToasts)

	// The 6 positioned demo stacks are only relevant on the toast
	// demo page; scope them so they don't bloat every other page.
	for _, p := range []struct {
		name string
		pos  widget.Position
	}{
		{"toasts-top-left", widget.TopLeft},
		{"toasts-top-center", widget.TopCenter},
		{"toasts-top-right", widget.TopRight},
		{"toasts-bottom-left", widget.BottomLeft},
		{"toasts-bottom-center", widget.BottomCenter},
		{"toasts-bottom-right", widget.BottomRight},
	} {
		stack := preset.ToastStack(p.name).Mount(p.pos).
			Pages("/components/toast").
			Build()
		widget.Mount(r, &stack)
	}

	// Demo push endpoint hit by the /components/toast demo buttons.
	// Returns the toast on a response header — no body needed.
	r.Post("/components/toast/push", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Variant    string `json:"variant"`
			Title      string `json:"title"`
			Body       string `json:"body"`
			Persistent bool   `json:"persistent"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		variant := ui.StatusInfo
		switch body.Variant {
		case "success":
			variant = ui.StatusSuccess
		case "warning":
			variant = ui.StatusWarning
		case "danger":
			variant = ui.StatusDanger
		}
		ttl := 5000
		if body.Persistent {
			ttl = 0
		}
		ui.AddToast(w, ui.ToastTrigger{
			Variant: variant,
			Title:   body.Title,
			Body:    body.Body,
			TTL:     ttl,
		})
		w.WriteHeader(http.StatusNoContent)
	}))

	// Burst endpoint — shows the runtime fires multiple toasts queued
	// onto a single response header.
	r.Post("/components/toast/push-burst", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ui.AddToastSuccess(w, "Build started", "Worker queued the job.", 4000)
		ui.AddToastWarning(w, "Cache cold", "First request will be slow.", 4000)
		ui.AddToastSuccess(w, "Build complete", "Image pushed to registry.", 4000)
		w.WriteHeader(http.StatusNoContent)
	}))

	// --- Sidebar drawer (responsive companion to the inline column) ---
	sidebarCfg := ui.SidebarConfig{
		Title: "Workspace",
		Items: []ui.SidebarItem{
			{Label: "Dashboard", Href: "/components/sidebar?section=dashboard"},
			{Label: "Components", MatchPath: "/components"},
			{Label: "Customers", Href: "/customers"},
			{Label: "Settings", Children: []ui.SidebarItem{
				{Label: "Profile", Href: "/components/sidebar?section=profile"},
				{Label: "Team", Href: "/components/sidebar?section=team"},
				{Label: "Billing", Href: "/components/sidebar?section=billing"},
			}},
		},
	}
	ui.MountSidebar(routerMounter{r}, sidebarCfg, "/components/sidebar")
}

// routerMounter adapts framework's *router.Router to the
// ui.WidgetMounter interface that MountSidebar expects.
type routerMounter struct{ r *router.Router }

func (m routerMounter) MountWidget(def *widget.Definition) {
	widget.Mount(m.r, def)
}

// ─── widget slot Components ──────────────────────────────────────────
// Each demo widget's body is a typed Component composed from
// core-ui/html + framework/ui. No raw HTML strings — the renderer's
// API is the only entry point so a11y attrs, theme tokens, and the
// CSP linter all apply.

type confirmModalBody struct{}

func (confirmModalBody) Render() render.HTML {
	closeAttr := html.Attrs{"data-fui-action": "close"}
	return html.Div(html.DivConfig{Class: "demo-modal-body"},
		html.Heading(html.HeadingConfig{Level: 2, ID: "components-confirm-title"},
			render.Text("Delete this thing?")),
		html.Paragraph(html.TextConfig{ID: "components-confirm-desc"},
			render.Text("This will free up disk space but cannot be undone.")),
		html.Div(html.DivConfig{Class: "demo-modal-actions"},
			ui.Button(ui.ButtonConfig{Label: "Cancel", Variant: ui.ButtonSecondary, Attrs: closeAttr}),
			ui.Button(ui.ButtonConfig{Label: "Delete", Variant: ui.ButtonDanger, Attrs: closeAttr}),
		),
	)
}

type userEditModalBody struct{}

func (userEditModalBody) Render() render.HTML {
	closeAttr := html.Attrs{"data-fui-action": "close"}
	return html.Div(html.DivConfig{Class: "demo-modal-body"},
		html.Heading(html.HeadingConfig{Level: 2, ID: "components-user-edit-title"},
			render.Text("Edit user "),
			html.Span(html.TextConfig{Attrs: html.Attrs{"data-fui-signal": "user_id"}}),
		),
		html.Paragraph(html.TextConfig{},
			render.Text("Signal user_id was seeded from the URL — refresh and watch it survive.")),
		render.Tag("label", map[string]string{"class": "demo-modal-field"},
			render.Text("Display name"),
			render.Tag("input", map[string]string{"type": "text", "value": ""}),
		),
		html.Div(html.DivConfig{Class: "demo-modal-actions"},
			ui.Button(ui.ButtonConfig{Label: "Cancel", Variant: ui.ButtonSecondary, Attrs: closeAttr}),
			ui.Button(ui.ButtonConfig{Label: "Save", Variant: ui.ButtonPrimary, Attrs: closeAttr}),
		),
	)
}

type quickNavDrawerBody struct{}

func (quickNavDrawerBody) Render() render.HTML {
	link := func(href, label string) render.HTML {
		return render.Tag("li", nil,
			html.LinkHTML(html.LinkHTMLConfig{Href: href, Content: render.Text(label)}),
		)
	}
	return html.Div(html.DivConfig{Class: "demo-drawer-body"},
		html.Heading(html.HeadingConfig{Level: 2, ID: "components-drawer-title"},
			render.Text("Quick nav")),
		html.Nav(html.NavConfig{Label: "Quick navigation"},
			html.UnorderedList(html.ListConfig{},
				link("/", "Home"),
				link("/components/", "Components"),
				link("/docs/", "Docs"),
			),
		),
		ui.Button(ui.ButtonConfig{
			Label: "Close", Variant: ui.ButtonSecondary,
			Class: "demo-drawer-spacer",
			Attrs: html.Attrs{"data-fui-action": "close"},
		}),
	)
}

type filterDrawerBody struct{}

func (filterDrawerBody) Render() render.HTML {
	signalSpan := func(name string) render.HTML {
		return html.Strong(html.TextConfig{Attrs: html.Attrs{"data-fui-signal": name}})
	}
	return html.Div(html.DivConfig{Class: "demo-drawer-body"},
		html.Heading(html.HeadingConfig{Level: 2, ID: "components-filter-drawer-title"},
			render.Text("Filters")),
		render.Tag("dl", nil,
			render.Tag("dt", nil, render.Text("Status")),
			render.Tag("dd", nil, signalSpan("status")),
			render.Tag("dt", nil, render.Text("Tag")),
			render.Tag("dd", nil, signalSpan("tag")),
		),
		html.Paragraph(html.TextConfig{Class: "demo-meta"},
			render.Text("These bound signals were seeded from "),
			render.Tag("code", nil, render.Text("?status=…&tag=…")),
			render.Text(" on the URL."),
		),
		ui.Button(ui.ButtonConfig{
			Label: "Close", Variant: ui.ButtonSecondary,
			Class: "demo-drawer-spacer",
			Attrs: html.Attrs{"data-fui-action": "close"},
		}),
	)
}

// Compile-time guarantees every body satisfies the Component contract.
var (
	_ component.Component = confirmModalBody{}
	_ component.Component = userEditModalBody{}
	_ component.Component = quickNavDrawerBody{}
	_ component.Component = filterDrawerBody{}
)
