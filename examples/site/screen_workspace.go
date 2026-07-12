package main

// A dedicated, full-page example of ui.PaneHost used as a real
// master-detail workspace — the intended use of the primitive, at the
// scale it's designed for (not a component-card demo).
//
// Flow, all without a page navigation:
//   - The primary pane is a support queue (a list of ticket rows).
//   - Clicking a row opens the secondary pane AND fires a GET RPC that
//     returns the ticket's detail HTML, which the runtime swaps into a
//     data-fui-signal="ws-ticket" (mode=html) region inside that pane.
//   - "View customer" inside the detail opens the tertiary pane and
//     loads the customer HTML the same way — a link filling a pane
//     instead of navigating.
//   - Below 768px the open side pane collapses to an overlay drawer
//     (backdrop, focus trap, ESC), handled entirely by the pane-host
//     runtime module.
//
// PaneHost owns only the pane lifecycle; the content-fill is the
// ordinary data-fui-rpc + data-fui-rpc-signal rail (see
// framework/docs/content/pane-host.md). The GET handlers live in
// setupServer (main.go) under /__site/workspace/*.

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// wsTicket / wsCustomer are the in-memory demo records. Package-level
// slices, read both at render time (to build the list) and in the RPC
// handlers (to look one up by id) — the demoCompany placement idiom.
type wsTicket struct {
	ID, Subject, Requester, Priority, Body, CustomerID string
	Status                                             ui.StatusVariant
	StatusLabel                                        string
}

type wsCustomer struct {
	ID, Name, Plan, Note string
	OpenTickets          string
}

var wsTickets = []wsTicket{
	{ID: "4021", Subject: "SSO login loops back to sign-in", Requester: "dana@northwind.io", Priority: "High", Status: ui.StatusDanger, StatusLabel: "Open", CustomerID: "north", Body: "After the Okta redirect the app bounces straight back to /login. Started this morning; other SSO tenants are unaffected. Suspecting the Sec-Fetch-Site cookie guard."},
	{ID: "4019", Subject: "CSV export truncates at 10k rows", Requester: "sam@acme.test", Priority: "Normal", Status: ui.StatusWarning, StatusLabel: "Pending", CustomerID: "acme", Body: "The invoices export stops at exactly 10,000 rows with no error. Likely a default page cap on the cursor stream."},
	{ID: "4008", Subject: "Webhook retries fire twice", Requester: "lee@globex.co", Priority: "Normal", Status: ui.StatusWarning, StatusLabel: "Pending", CustomerID: "globex", Body: "Downstream sees two deliveries per event during a redeploy. Reads like an at-least-once outbox with a consumer that isn't idempotent yet."},
	{ID: "3990", Subject: "Dark-mode chart labels unreadable", Requester: "dana@northwind.io", Priority: "Low", Status: ui.StatusInfo, StatusLabel: "Triaged", CustomerID: "north", Body: "Axis labels render near-black on the dark surface. A token wired to the wrong --color-* variable, most likely."},
	{ID: "3971", Subject: "Add SAML group → role mapping", Requester: "sam@acme.test", Priority: "Low", Status: ui.StatusSuccess, StatusLabel: "Shipped", CustomerID: "acme", Body: "Feature request to map IdP groups onto app roles at login. Shipped in the last release; keeping open until the customer confirms."},
}

var wsCustomers = map[string]wsCustomer{
	"north":  {ID: "north", Name: "Northwind Trading", Plan: "Enterprise", OpenTickets: "2 open tickets", Note: "SSO via Okta. Renewal in 3 months — treat auth issues as priority."},
	"acme":   {ID: "acme", Name: "Acme Corp", Plan: "Growth", OpenTickets: "2 open tickets", Note: "Heavy export/API user. Sensitive to data-completeness bugs."},
	"globex": {ID: "globex", Name: "Globex", Plan: "Growth", OpenTickets: "1 open ticket", Note: "Runs its own webhook consumers; cares about delivery semantics."},
}

func wsTicketByID(id string) (wsTicket, bool) {
	for _, t := range wsTickets {
		if t.ID == id {
			return t, true
		}
	}
	return wsTicket{}, false
}

// WorkspaceScreen renders the /examples/workspace page. It embeds
// component.ContextOnly so implementing RenderCtx alone satisfies the
// Component contract (the Render() shim delegates to RenderCtx).
type WorkspaceScreen struct{ component.ContextOnly }

func (s *WorkspaceScreen) ScreenTitle() string { return "Workspace example" }
func (s *WorkspaceScreen) ScreenDescription() string {
	return "A full-page master-detail workspace built on ui.PaneHost — click a ticket to load its detail into a pane, no navigation."
}
func (s *WorkspaceScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *WorkspaceScreen) RenderCtx(_ context.Context) render.HTML {
	// Primary pane: the support queue.
	rows := make([]render.HTML, 0, len(wsTickets)+1)
	rows = append(rows, html.Heading(html.HeadingConfig{Level: 2, Class: "ws-title"},
		render.Text("Support queue")))
	for _, t := range wsTickets {
		rows = append(rows, workspaceRow(t))
	}
	primary := html.Div(html.DivConfig{Class: "ws-list"}, rows...)

	// Secondary pane: a header + the RPC-filled ticket region.
	secondary := html.Div(html.DivConfig{},
		paneHeader("Ticket", "secondary"),
		render.Tag("div", map[string]string{
			"data-fui-signal":      "ws-ticket",
			"data-fui-signal-mode": "html",
		}, ui.EmptyState(ui.EmptyStateConfig{
			Title:        "Select a ticket",
			Description:  "Choose a row on the left to load its detail here.",
			HeadingLevel: 3,
		})),
	)

	// Tertiary pane: the RPC-filled customer region.
	tertiary := html.Div(html.DivConfig{},
		paneHeader("Customer", "tertiary"),
		render.Tag("div", map[string]string{
			"data-fui-signal":      "ws-customer",
			"data-fui-signal-mode": "html",
		}, ui.EmptyState(ui.EmptyStateConfig{
			Title:        "No customer open",
			Description:  "Open a ticket, then choose “View customer”.",
			HeadingLevel: 3,
		})),
	)

	intro := html.Div(html.DivConfig{Class: "ws-intro"},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Support workspace")),
		html.Paragraph(html.TextConfig{Class: "ws-lede"}, render.Text(
			"A master-detail layout on ui.PaneHost. Clicking a ticket loads its detail into the pane beside the list via an RPC — no page navigation — and “View customer” fills a third pane the same way. Narrow the window to see the pane become an overlay drawer.")),
	)

	return html.Div(html.DivConfig{Class: "ws-page"},
		intro,
		ui.PaneHost(ui.PaneHostConfig{
			ID:             "workspace",
			Class:          "ws-host",
			Primary:        primary,
			Secondary:      secondary,
			Tertiary:       tertiary,
			SecondaryLabel: "Ticket detail",
			TertiaryLabel:  "Customer detail",
		}),
	)
}

// workspaceRow is one clickable ticket row. A semantic <button> (not an
// <a>, so there's no navigation to intercept) carrying two independent
// delegated behaviors: data-fui-pane-open reveals the secondary pane and
// data-fui-rpc GETs the detail into the ws-ticket signal region.
func workspaceRow(t wsTicket) render.HTML {
	return render.Tag("button", map[string]string{
		"type":                "button",
		"class":               "ws-row",
		"aria-label":          "Open ticket " + t.ID + ": " + t.Subject,
		"data-fui-rpc":        "/__site/workspace/ticket?id=" + t.ID,
		"data-fui-rpc-method": "GET",
		"data-fui-rpc-signal": "ws-ticket",
		"data-fui-pane-open":  "secondary",
	},
		render.Tag("span", map[string]string{"class": "ws-row__id"}, render.Text("#"+t.ID)),
		render.Tag("span", map[string]string{"class": "ws-row__subject"}, render.Text(t.Subject)),
		ui.StatusBadge(ui.StatusBadgeConfig{Label: t.StatusLabel, Variant: t.Status}),
	)
}

// paneHeader is the title + Close button strip at the top of a side pane.
func paneHeader(title, pane string) render.HTML {
	return html.Div(html.DivConfig{Class: "ws-pane-head"},
		html.Heading(html.HeadingConfig{Level: 2, Class: "ws-pane-title"}, render.Text(title)),
		ui.Button(ui.ButtonConfig{
			Label:      "Close",
			Variant:    ui.ButtonGhost,
			ExtraAttrs: html.Attrs{"data-fui-pane-close": pane},
		}),
	)
}

// renderTicketDetail builds the HTML fragment the ticket RPC returns. It
// is rendered to a string and swapped into the secondary pane's signal
// region as trusted server HTML.
func renderTicketDetail(t wsTicket) render.HTML {
	cust := wsCustomers[t.CustomerID]
	return html.Div(html.DivConfig{Class: "ws-detail"},
		html.Heading(html.HeadingConfig{Level: 3, Class: "ws-detail__subject"}, render.Text(t.Subject)),
		ui.DetailList(ui.DetailListConfig{Items: []ui.DetailItem{
			{Label: "Ticket", Value: render.Text("#" + t.ID)},
			{Label: "Status", Value: ui.StatusBadge(ui.StatusBadgeConfig{Label: t.StatusLabel, Variant: t.Status})},
			{Label: "Priority", Value: render.Text(t.Priority)},
			{Label: "Requester", Value: render.Text(t.Requester)},
			{Label: "Customer", Value: render.Text(cust.Name)},
		}}),
		html.Paragraph(html.TextConfig{Class: "ws-detail__body"}, render.Text(t.Body)),
		ui.Button(ui.ButtonConfig{
			Label:   "View customer",
			Variant: ui.ButtonSecondary,
			ExtraAttrs: html.Attrs{
				"data-fui-rpc":        "/__site/workspace/customer?id=" + t.CustomerID,
				"data-fui-rpc-method": "GET",
				"data-fui-rpc-signal": "ws-customer",
				"data-fui-pane-open":  "tertiary",
			},
		}),
	)
}

// renderCustomerDetail builds the HTML fragment the customer RPC returns.
func renderCustomerDetail(c wsCustomer) render.HTML {
	return html.Div(html.DivConfig{Class: "ws-detail"},
		html.Heading(html.HeadingConfig{Level: 3, Class: "ws-detail__subject"}, render.Text(c.Name)),
		ui.DetailList(ui.DetailListConfig{Items: []ui.DetailItem{
			{Label: "Plan", Value: render.Text(c.Plan)},
			{Label: "Activity", Value: render.Text(c.OpenTickets)},
		}}),
		html.Paragraph(html.TextConfig{Class: "ws-detail__body"}, render.Text(c.Note)),
	)
}
