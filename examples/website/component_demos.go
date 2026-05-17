package main

import (
	"encoding/json"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
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
	// pages don't ship references to demo-only widgets.
	confirm := preset.Modal("components-confirm").
		Hidden().
		Pages("/components/modal").
		LabelledBy("components-confirm-title").
		DescribedBy("components-confirm-desc").
		Slot("body", htmlSlot{html: `<div class="demo-modal-body">
<h2 id="components-confirm-title">Delete this thing?</h2>
<p id="components-confirm-desc">This will free up disk space but cannot be undone.</p>
<div class="demo-modal-actions">
<button class="ui-button" type="button" data-fui-action="close">Cancel</button>
<button class="ui-button ui-button--danger" type="button" data-fui-action="close">Delete</button>
</div></div>`}).
		Build()
	widget.Mount(r, &confirm)

	userEdit := preset.Modal("components-user-edit").
		Hidden().
		Pages("/components/modal").
		DeepLink("modal", "user-edit").
		DeepLinkParam("user_id").
		LabelledBy("components-user-edit-title").
		Signal("user_id", widget.SignalFunc(func() (any, error) { return "", nil })).
		Slot("body", htmlSlot{html: `<div class="demo-modal-body">
<h2 id="components-user-edit-title">Edit user <span data-fui-signal="user_id"></span></h2>
<p>Signal user_id was seeded from the URL — refresh and watch it survive.</p>
<label class="demo-modal-field">Display name
<input type="text" value="">
</label>
<div class="demo-modal-actions">
<button class="ui-button" type="button" data-fui-action="close">Cancel</button>
<button class="ui-button ui-button--primary" type="button" data-fui-action="close">Save</button>
</div></div>`}).
		Build()
	widget.Mount(r, &userEdit)

	// --- Drawer demos -------------------------------------------------
	// Page-scoped: only relevant on /components/drawer.
	plainDrawer := preset.Drawer("components-drawer").
		Hidden().
		Pages("/components/drawer").
		LabelledBy("components-drawer-title").
		Slot("body", htmlSlot{html: `<div class="demo-drawer-body">
<h2 id="components-drawer-title">Quick nav</h2>
<nav><ul>
<li><a href="/">Home</a></li>
<li><a href="/components/">Components</a></li>
<li><a href="/docs/">Docs</a></li>
</ul></nav>
<button class="ui-button demo-drawer-spacer" type="button" data-fui-action="close">Close</button>
</div>`}).
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
		Slot("body", htmlSlot{html: `<div class="demo-drawer-body">
<h2 id="components-filter-drawer-title">Filters</h2>
<dl>
<dt>Status</dt><dd><strong data-fui-signal="status"></strong></dd>
<dt>Tag</dt><dd><strong data-fui-signal="tag"></strong></dd>
</dl>
<p class="demo-meta">These bound signals were seeded from <code>?status=…&amp;tag=…</code> on the URL.</p>
<button class="ui-button demo-drawer-spacer" type="button" data-fui-action="close">Close</button>
</div>`}).
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

// htmlSlot wraps a literal HTML string as a Component for use as a widget Slot.
type htmlSlot struct{ html string }

func (h htmlSlot) Render() render.HTML { return render.HTML(h.html) }

var _ component.Component = htmlSlot{}
