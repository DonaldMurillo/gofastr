package main

import (
	"encoding/json"
	stdhtml "html"
	"net/http"
	"strconv"
	"strings"

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

	// --- FileUpload echo demo ---------------------------------------
	// POST handler reads the multipart form, returns an HTML fragment
	// listing the received files. The fragment lands in the
	// data-fui-signal="fileupload-echo" island on /components/fileupload.
	r.Post("/components/fileupload/echo", http.HandlerFunc(fileUploadEchoHandler))

	// --- Popover demo -------------------------------------------------
	pop := preset.Popover("components-popover").
		Pages("/components/popover").
		LabelledBy("components-popover-title").
		// `from` signal carries the trigger's identifier so the body
		// can show "Opened from: X". Triggers seed it via
		// data-fui-deeplink="from=<key>".
		Signal("from", widget.SignalFunc(func() (any, error) { return "(default trigger)", nil })).
		Slot("body", popoverDemoBody{}).
		Build()
	pop.DeepLinkParams = append(pop.DeepLinkParams, "from")
	widget.Mount(r, &pop)

	// --- Wave 4: Lightbox modal --------------------------------------
	// Standalone Lightbox modal — Gallery thumbs on /components/lightbox
	// trigger it via data-fui-open + data-fui-deeplink.
	lightboxModal := ui.Lightbox(ui.LightboxConfig{
		Name:          "components-lightbox-demo",
		Label:         "Photo viewer",
		Pages:         []string{"/components/lightbox"},
		NavArrows:     true,
		ShowCaption:   true,
		AllowDownload: true,
	})
	lbBuilt := lightboxModal.Build()
	widget.Mount(r, &lbBuilt)

	// --- Wave 4: NotificationBell popover ----------------------------
	_, bellPop := ui.NotificationBell(ui.NotificationBellConfig{
		Name:  "components-bell-demo",
		Label: "Notifications",
		Pages: []string{"/components/notificationbell"},
		Items: []ui.NotificationItem{{Title: "x"}},
	})
	bellBuilt := bellPop.Build()
	widget.Mount(r, &bellBuilt)
	_, bellEmptyPop := ui.NotificationBell(ui.NotificationBellConfig{
		Name:  "components-bell-empty",
		Label: "Notifications",
		Pages: []string{"/components/notificationbell"},
	})
	bellEmptyBuilt := bellEmptyPop.Build()
	widget.Mount(r, &bellEmptyBuilt)

	// --- Wave 4: BottomSheet demo ------------------------------------
	bottomSheet := preset.BottomSheet("components-bottomsheet-demo").
		Hidden().
		Pages("/components/bottomsheet").
		LabelledBy("components-bottomsheet-title").
		Slot("body", bottomSheetBody{}).
		Build()
	widget.Mount(r, &bottomSheet)

	// --- Wave 4: SortableList reorder RPC ----------------------------
	// Accepts POST `order=<comma-keys>`; the demo just echoes 200 OK so
	// the runtime's commit/revert pipeline can be observed without
	// persisting state.
	r.Post("/components/sortablelist/reorder", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		// Accept any non-empty order string. Real apps would persist.
		w.WriteHeader(http.StatusNoContent)
	}))

	// --- Wave 4: GlobalSearch results RPC ----------------------------
	// Returns a static <li> list filtered by the query — keeps the demo
	// hermetic. The signal-bound listbox swaps to this HTML.
	r.Post("/components/globalsearch/results", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		q := strings.ToLower(req.FormValue("q"))
		items := []string{
			"Accordion", "Banner", "Button", "CodeBlock", "Combobox",
			"DiffViewer", "Drawer", "InfiniteScroll", "LineChart", "Modal",
			"MultiSelect", "Pagination", "PieChart", "Popover", "Rating",
			"Sidebar", "Slider", "TextArea", "Tooltip", "TreeView",
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if q == "" {
			_, _ = w.Write([]byte(`<li class="combobox__opt">Type to search…</li>`))
			return
		}
		var b strings.Builder
		for _, it := range items {
			if strings.Contains(strings.ToLower(it), q) {
				b.WriteString(`<li class="combobox__opt"><a href="/components/`)
				b.WriteString(strings.ToLower(it))
				b.WriteString(`">`)
				b.WriteString(it)
				b.WriteString(`</a></li>`)
			}
		}
		if b.Len() == 0 {
			b.WriteString(`<li class="combobox__opt">No matches.</li>`)
		}
		_, _ = w.Write([]byte(b.String()))
	}))
}

// bottomSheetBody renders the /components/bottomsheet demo content.
type bottomSheetBody struct{}

func (bottomSheetBody) Render() render.HTML {
	return render.Tag("div", map[string]string{"class": "demo-modal-body"},
		html.Heading(html.HeadingConfig{Level: 2, ID: "components-bottomsheet-title"},
			render.Text("Share this post")),
		html.Paragraph(html.TextConfig{},
			render.Text("Pick a destination. Standard ESC + backdrop-click dismiss; mobile drag-to-dismiss is on the deferred list.")),
		render.Tag("div", map[string]string{"class": "demo-button-row"},
			ui.Button(ui.ButtonConfig{Label: "Copy link", Variant: ui.ButtonSecondary}),
			ui.Button(ui.ButtonConfig{Label: "Send via email", Variant: ui.ButtonSecondary}),
			ui.Button(ui.ButtonConfig{Label: "Share to chat"}),
		),
	)
}

// fileUploadEchoHandler reads the multipart form, returns an HTML
// fragment listing the received files. The fragment is HTML-mode
// signal content for the data-fui-signal="fileupload-echo" island.
// Cap at 10MB total so the demo can't OOM the dev server.
func fileUploadEchoHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<p class="demo-upload-result__error">Upload failed: ` + stdhtml.EscapeString(err.Error()) + `</p>`))
		return
	}
	files := r.MultipartForm.File["files"]
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(files) == 0 {
		_, _ = w.Write([]byte(`<p class="demo-upload-result__empty">No files in the request. Pick or drop something, then click Upload.</p>`))
		return
	}
	// Build a tiny rows list. No persistence — the demo server
	// discards the payload after echoing its metadata.
	body := `<p class="demo-upload-result__title">Received ` + strconv.Itoa(len(files)) + ` file(s):</p><ul class="demo-upload-result__list">`
	for _, fh := range files {
		body += `<li><strong>` + stdhtml.EscapeString(fh.Filename) + `</strong> — ` + uploadFormatBytes(fh.Size) + ` (` + stdhtml.EscapeString(fh.Header.Get("Content-Type")) + `)</li>`
	}
	body += `</ul>`
	_, _ = w.Write([]byte(body))
}

func uploadFormatBytes(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}
	if n < 1024*1024 {
		return strconv.FormatInt(n/1024, 10) + " KB"
	}
	return strconv.FormatInt(n/(1024*1024), 10) + " MB"
}

type popoverDemoBody struct{}

func (popoverDemoBody) Render() render.HTML {
	closeAttr := html.Attrs{"data-fui-action": "close"}
	return html.Div(html.DivConfig{Class: "demo-modal-body popover-demo-body"},
		html.Heading(html.HeadingConfig{Level: 2, ID: "components-popover-title"},
			render.Text("Share this page")),
		html.Paragraph(html.TextConfig{Class: "popover-demo-body__from"},
			render.Text("Opened from: "),
			html.Strong(html.TextConfig{Attrs: html.Attrs{"data-fui-signal": "from"}}),
		),
		html.Paragraph(html.TextConfig{},
			render.Text("Anchored, dismiss-on-outside, no backdrop dim. Tab moves out naturally — there's no focus trap.")),
		html.Div(html.DivConfig{Class: "demo-modal-actions"},
			ui.Button(ui.ButtonConfig{Label: "Copy link", Variant: ui.ButtonSecondary, Attrs: closeAttr}),
			ui.Button(ui.ButtonConfig{Label: "Done", Variant: ui.ButtonPrimary, Attrs: closeAttr}),
		),
	)
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
