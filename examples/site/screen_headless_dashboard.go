package main

// /examples/headless/{theme}/dashboard — the form family's first cut
// of a real surface: a settings page whose form submits both ways
// (island RPC with the runtime, plain POST without), validates on the
// server, moves focus to the summary on a failed submit, carries a
// password field and an upload, and nests conditional regions. Every
// control on it is one this stack rebuilt: the typed fields, the
// password affix shell, the upload on the headless drop hooks, and
// the when-regions the headless module hides.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// ── Route ───────────────────────────────────────────────────────────

// dashboardRoutePath is the canonical URL of one dashboard route.
func dashboardRoutePath(segment string) string {
	return "/examples/headless/" + segment + "/dashboard"
}

// ── State ───────────────────────────────────────────────────────────

// dashboardSettingsState is the settings form's render state. The
// typed values never travel in the URL: the no-script redirect
// carries the outcome alone, and the error re-render starts the form
// over — the same trade the landing's newsletter makes, so a refused
// password or webhook URL lands in no history and no referrer.
type dashboardSettingsState struct {
	Errors ui.FieldErrors
	Done   bool
}

// dashboardSettingsOutcome is what the server decided about a submit.
type dashboardSettingsOutcome string

const (
	dashboardSettingsOK            dashboardSettingsOutcome = "ok"
	dashboardSettingsInvalidName   dashboardSettingsOutcome = "invalid-name"
	dashboardSettingsShortPassword dashboardSettingsOutcome = "short-password"
	dashboardSettingsMissingHook   dashboardSettingsOutcome = "missing-hook"
	dashboardSettingsBadHook       dashboardSettingsOutcome = "bad-hook"
)

// dashboardValidate applies the server-side checks both paths share.
// The form is novalidate on purpose: the server owns validation so the
// island and the no-script round trip answer identically.
func dashboardValidate(form url.Values) (dashboardSettingsOutcome, ui.FieldErrors) {
	errs := ui.FieldErrors{}
	if strings.TrimSpace(form.Get("display")) == "" {
		errs["display"] = "A display name is required."
	}
	if pw := form.Get("password"); pw != "" && len(pw) < 8 {
		errs["password"] = "A new password needs at least 8 characters."
	}
	if form.Get("notify") == "webhook" {
		hook := strings.TrimSpace(form.Get("webhook"))
		switch {
		case hook == "":
			errs["webhook"] = "A webhook endpoint is required when notifications are webhook."
		case !strings.HasPrefix(hook, "/"):
			errs["webhook"] = "The webhook endpoint must be a path on this origin."
		}
	}
	if len(errs) == 0 {
		return dashboardSettingsOK, nil
	}
	// One outcome for the redirect, chosen in field order so the
	// query names the first thing to fix.
	switch {
	case errs["display"] != "":
		return dashboardSettingsInvalidName, errs
	case errs["password"] != "":
		return dashboardSettingsShortPassword, errs
	case errs["webhook"] == "A webhook endpoint is required when notifications are webhook.":
		return dashboardSettingsMissingHook, errs
	default:
		return dashboardSettingsBadHook, errs
	}
}

// ── The form ────────────────────────────────────────────────────────

const (
	dashboardSettingsPath   = "/__site/headless/settings"
	dashboardSettingsSignal = "hd-settings"
)

// renderDashboardSettings renders the form (or, after a success, the
// success callout). The SSR page, the query-rendered no-script answer
// and every island response go through this one function, so the round
// trip is stateless: the answer is the re-rendered region.
func renderDashboardSettings(ctx context.Context, r landingRoute, state dashboardSettingsState) render.HTML {
	if state.Done {
		return ui.Callout(ui.CalloutConfig{
			Variant: ui.StatusSuccess,
			ID:      "hd-settings-done",
			Title:   "Settings saved",
		}, render.Text("This demo persists nothing: the round trip — validation, focus transfer, the announcement — is the point. Reload for the form again."))
	}
	// A failed submit renders through FormConfig.Errors: the form
	// derives its summary's id from its own, marks itself so the
	// headless behaviour module moves focus to the summary after the
	// island swap, and maps each error to its control's real id so
	// the summary's link lands on the input.
	return ui.Form(ui.FormConfig{
		Action:      dashboardSettingsPath,
		Method:      "POST",
		ID:          "hd-settings",
		SubmitLabel: "Save settings",
		Errors:      state.Errors,
		FieldIDs: map[string]string{
			"display": "hd-display", "password": "hd-password", "webhook": "hd-webhook",
		},
		FieldLabels: map[string]string{
			"display": "Display name", "password": "New password", "webhook": "Webhook endpoint",
		},
		FieldOrder: []string{"display", "password", "webhook"},
		NoValidate: true,
		// The island wiring rides the request seam: with the runtime
		// on the page a submit is an RPC whose 200 body is this
		// region, re-rendered; without the runtime the same POST
		// navigates and the handler answers 303 back to this page.
		ExtraAttrs: interactive.Post(dashboardSettingsPath).
			OnSuccess(interactive.SetSignal(dashboardSettingsSignal)).Attrs(),
	},
		ui.TextField(ui.TextFieldConfig{
			Name: "display", Label: "Display name", ID: "hd-display",
			Required: true, AutoComplete: "nickname",
			Help:  "Server-validated on both paths: the island answers the region, the no-script POST redirects back.",
			Error: state.Errors["display"],
		}),
		ui.FormField(ui.FormFieldConfig{
			Label: "New password", For: "hd-password",
			Help: "Leave blank to keep the current one. Eight characters minimum when set.",
			Input: func(c headless.FieldControl) render.HTML {
				return ui.PasswordInput(ui.PasswordInputConfig{
					Name: "password", Autocomplete: "new-password", Field: c,
				})
			},
		}),
		ui.FormField(ui.FormFieldConfig{
			Label: "Avatar", For: "hd-avatar",
			Help: "The drop zone is a label for the input: the whole target opens the picker with no script. Chosen names are listed and announced.",
			Input: func(c headless.FieldControl) render.HTML {
				return ui.FileUpload(ui.FileUploadConfig{
					Name: "avatar", ID: c.ID, Label: "Drop an image here, or", Accept: "image/*",
					MaxSizeMB: 2,
					// The announcement sentences and the size hint
					// resolve per request: this page exists to show
					// the bridge, so it must actually use it.
					Ctx: ctx,
				})
			},
		}),
		ui.RadioGroup(ui.RadioGroupConfig{
			Legend: "Notifications", Name: "notify",
			Options: []ui.RadioGroupOption{
				{Label: "None", Value: "none", Checked: true},
				{Label: "Email digest", Value: "email"},
				{Label: "Webhook", Value: "webhook"},
			},
		}),
		// Nested conditionals: the outer region watches the radio;
		// the inner one watches a checkbox that only exists inside
		// the outer region. Both render visible and the headless
		// module hides what does not match — and disables what it
		// hides, so a hidden webhook never submits.
		ui.ConditionalField(ui.ConditionalFieldConfig{
			WhenName: "notify", WhenValue: "webhook",
			Children: []render.HTML{
				ui.TextField(ui.TextFieldConfig{
					Name: "webhook", Label: "Webhook endpoint", ID: "hd-webhook",
					Placeholder: "/hooks/deploy-events",
					Help:        "A path on this origin; the demo validates shape only.",
					Error:       state.Errors["webhook"],
				}),
				ui.Checkbox(ui.ToggleConfig{
					Name: "retries", Label: "Retry failed deliveries", Value: "on", ID: "hd-retries",
				}),
				ui.ConditionalField(ui.ConditionalFieldConfig{
					WhenName: "retries", WhenValue: "on",
					Children: []render.HTML{
						ui.NumberField(ui.NumberFieldConfig{
							Name: "retry-count", Label: "Retry count", ID: "hd-retry-count",
							Help: "Attempts before the delivery is dropped.",
						}),
					},
				}),
			},
		}),
		// The no-script round trip needs the theme segment
		// server-side; the island response ignores it.
		html.Input(html.InputConfig{Type: "hidden", Name: "theme", Value: r.Segment}),
	)
}

// dashboardSettingsRegion is the signal-bound region the island
// response replaces.
func dashboardSettingsRegion(ctx context.Context, r landingRoute, state dashboardSettingsState) render.HTML {
	return interactive.BindHTML(
		html.Div(html.DivConfig{ID: "hd-settings-region"}, renderDashboardSettings(ctx, r, state)),
		dashboardSettingsSignal)
}

// ── The screen ──────────────────────────────────────────────────────

// HeadlessDashboardScreen is /examples/headless/:theme/dashboard.
type HeadlessDashboardScreen struct {
	Route landingRoute
	// Settings carries the no-script round trip's outcome, read from
	// the route's query (see Load).
	Settings dashboardSettingsState
	// Sort carries the invoice table's active sort, read from the
	// query the sort anchors write: column key and direction. Empty
	// key means the default (newest-first) order.
	SortBy  string
	SortDir ui.SortDir
}

func (s *HeadlessDashboardScreen) ScreenTitle() string {
	return "Billing & usage · " + s.Route.Name + " theme"
}

func (s *HeadlessDashboardScreen) ScreenDescription() string {
	return "A basic dashboard on the rebuilt components: a record summary with live signals, a chart with a text alternative, an invoice table that sorts without script, and the settings form unchanged."
}

func (s *HeadlessDashboardScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// SetParams resolves the theme segment. An unknown segment leaves the
// route zero so Load rejects it.
func (s *HeadlessDashboardScreen) SetParams(p map[string]string) {
	if r, ok := landingRouteFor(p["theme"]); ok {
		s.Route = r
	}
}

// Load rejects unknown theme segments so the site's 404 screen answers
// them, reads the no-script round trip's query (the 303 the native
// POST answers leaves the outcome in the URL — ok, or the first thing
// to fix — and never a typed value), and reads the invoice table's
// sort state: a sort anchor without script navigates to this same path
// carrying ?sort=&dir=, and the page re-renders the table in that
// order.
func (s *HeadlessDashboardScreen) Load(ctx context.Context) error {
	if _, ok := landingRouteFor(s.Route.Segment); !ok {
		return errors.New("headless dashboard: unknown theme " + s.Route.Segment)
	}
	q := app.QueryFromContext(ctx)
	if key := q.Get("sort"); key == "issued" || key == "amount" {
		s.SortBy = key
	}
	switch ui.SortDir(q.Get("dir")) {
	case ui.SortAsc, ui.SortDesc:
		s.SortDir = ui.SortDir(q.Get("dir"))
	}
	switch dashboardSettingsOutcome(q.Get("settings")) {
	case dashboardSettingsOK:
		s.Settings = dashboardSettingsState{Done: true}
	case dashboardSettingsInvalidName, dashboardSettingsShortPassword,
		dashboardSettingsMissingHook, dashboardSettingsBadHook:
		// The outcome names the first thing to fix; the re-rendered
		// form starts over (values never travel in the URL). The
		// synthetic form below reproduces exactly the named error
		// through the one validator, so the sentence the query
		// promised is the sentence the page shows and no other.
		var probe url.Values
		switch dashboardSettingsOutcome(app.QueryFromContext(ctx).Get("settings")) {
		case dashboardSettingsShortPassword:
			probe = url.Values{"display": {"x"}, "password": {"short"}}
		case dashboardSettingsMissingHook:
			probe = url.Values{"display": {"x"}, "notify": {"webhook"}}
		case dashboardSettingsBadHook:
			probe = url.Values{"display": {"x"}, "notify": {"webhook"}, "webhook": {"example.com/hook"}}
		}
		_, errs := dashboardValidate(probe)
		s.Settings = dashboardSettingsState{Errors: errs}
	}
	return nil
}

// StaticPaths enumerates one page per registered theme so the static
// export, the sitemap, llm.md and the coverage gate all see every
// route.
func (s *HeadlessDashboardScreen) StaticPaths(ctx context.Context) []map[string]string {
	out := make([]map[string]string, 0, len(landingRoutes))
	for _, r := range landingRoutes {
		out = append(out, map[string]string{"theme": r.Segment})
	}
	return out
}

func (s *HeadlessDashboardScreen) Render() render.HTML {
	return s.render(context.Background())
}

// RenderCtx renders with the request's context, so the components the
// page renders resolve their words through ui.StringsFor(ctx).
func (s *HeadlessDashboardScreen) RenderCtx(ctx context.Context) render.HTML {
	return s.render(ctx)
}

func (s *HeadlessDashboardScreen) render(ctx context.Context) render.HTML {
	r := s.Route
	// The "Command center" recipe (ui-composition-recipes): one
	// RecordSummary leads — state, next decision, a MetricBand of
	// signals, one action — then the chart and the invoice table as
	// same-weight modules in a Grid, the settings form last. The
	// summary is full width on desktop; on phones RecordSummary
	// itself reorders (action into the lead region, signals in the
	// band) so the page needs no second mobile composition.
	return ui.Themed(r.Ref, container(
		ui.PageHeader(ui.PageHeaderConfig{
			Actions:  landingThemeSwitcher(dashboardRoutePath, r.Segment),
			Title:    "Billing & usage",
			Subtitle: "Northwind Workspace on the Pro plan, September 2026.",
		}),
		dashboardSummary(),
		ui.Grid(ui.GridConfig{Min: "24rem"},
			dashboardUsageSection(),
			dashboardInvoicesSection(r, s.SortBy, s.SortDir),
		),
		ui.Section(ui.SectionConfig{
			ID:          "hd-settings-section",
			Heading:     "Account settings",
			Description: "Server-validated. With the runtime: an island swap and focus lands on the summary. Without script: the same POST redirects back and this page re-renders the answer.",
		}, dashboardSettingsRegion(ctx, r, s.Settings)),
	))
}

// ── The dashboard fixtures ──────────────────────────────────────────
// All static and deterministic: an invented product's billing and
// usage, in the file, with no clock and no randomness.

// dashboardUsage is twelve months of API requests, in millions of
// calls. The chart and its DetailList text alternative read the same
// slice, so the two can never disagree.
var dashboardUsage = struct {
	Months []string
	Values []float64
}{
	Months: []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
	Values: []float64{0.86, 0.90, 0.97, 1.02, 0.99, 1.08, 1.15, 1.21, 1.18, 1.26, 1.31, 1.42},
}

// dashboardUsageMonths names the months in full, for the text
// alternative's rows.
var dashboardUsageMonths = []string{
	"January", "February", "March", "April", "May", "June",
	"July", "August", "September", "October", "November", "December",
}

// dashboardSummary renders the page's dominant element: the state of
// the workspace's billing, the next decision, four signals, one
// action. HeadingLevel 2 keeps the outline under the PageHeader's h1.
func dashboardSummary() render.HTML {
	return ui.RecordSummary(ui.RecordSummaryConfig{
		ID:           "hd-summary",
		HeadingLevel: 2,
		Tone:         ui.RecordSummaryToneWarning,
		Eyebrow:      "Northwind Workspace · Billing",
		Title:        "Usage is near the plan ceiling",
		Description:  "API requests and storage both track close to the Pro plan's included amounts. Nothing has throttled; the next invoice grows only if the seats renew as they stand.",
		Status: ui.StatusBadge(ui.StatusBadgeConfig{
			Label: "Pro plan · active", Variant: ui.StatusSuccess,
		}),
		Highlight: ui.Callout(ui.CalloutConfig{
			ID: "hd-summary-decision", Title: "Next decision · Oct 1", Variant: ui.StatusWarning,
		}, render.Text("Seats renew at 12 on Oct 1. Three seats have been idle for 60 days — removing them keeps the invoice at $171.50.")),
		Metrics: ui.MetricBand(ui.MetricBandConfig{
			ID: "hd-summary-metrics", Label: "Billing and usage signals",
			Items: []ui.MetricBandItem{
				{Label: "Seats", Value: "9 of 12", Hint: "3 idle 60+ days"},
				{Label: "API requests", Value: "1.42M", Hint: "of 1.5M included"},
				{Label: "Storage", Value: "96 GB", Hint: "of 100 GB included"},
				{Label: "Next invoice", Value: "$196.00", Hint: "due Oct 1"},
			},
		}),
		Aside:  ui.Muted(render.Text("Billing contact: ada@northwind.dev. Annual plan, monthly invoices.")),
		Footer: ui.Muted(render.Text("Usage as of Sep 28 · invoices from the billing provider")),
		Actions: ui.LinkButton(ui.LinkButtonConfig{
			ID: "hd-summary-action", Label: "Manage billing", Href: "#hd-settings-section",
		}),
	})
}

// dashboardUsageSection renders the usage chart named by its visible
// heading (a Section with an ID gives that heading the id
// hd-usage-title), with the same twelve values as a DetailList under
// it — the text alternative a screen reader or a copy-paste reads.
func dashboardUsageSection() render.HTML {
	values := dashboardUsage.Values
	alt := make([]ui.DetailItem, len(values))
	for i, v := range values {
		alt[i] = ui.DetailItem{
			Label: dashboardUsageMonths[i] + " requests",
			Value: render.Text(fmt.Sprintf("%.2fM", v)),
		}
	}
	return ui.Section(ui.SectionConfig{
		ID:          "hd-usage",
		Heading:     "API usage, twelve months",
		Description: "Millions of requests per month against the 1.5M included. The values are listed as text under the chart.",
	},
		ui.Stack(ui.StackConfig{},
			ui.LineChart(ui.LineChartConfig{
				ID: "hd-usage-chart",
				// The visible heading above names the chart.
				LabelledBy: "hd-usage-title",
				Series: []ui.LineSeries{{
					Name:   "API requests (millions)",
					Values: values,
					Area:   true,
				}},
				Labels: dashboardUsage.Months,
				Width:  520,
				Height: 220,
			}),
			// The values behind a disclosure: twelve rows open by
			// default doubled the column's height against the table
			// beside it, and on a phone pushed the table a screen down.
			ui.Collapsible(ui.CollapsibleConfig{Summary: "Monthly values as text"},
				ui.DetailList(ui.DetailListConfig{Items: alt})),
		),
	)
}

// dashboardInvoice is one row of the invoice table. Issued is an ISO
// date so the default order (newest first) and the sort are both plain
// lexical comparisons; AmountCents keeps the sort numeric.
type dashboardInvoice struct {
	Number      string
	Issued      string // ISO date, displayed through dashboardInvoiceDate
	AmountCents int
	Status      string // paid | refunded | overdue
}

var dashboardInvoices = []dashboardInvoice{
	// The numbers use a non-breaking hyphen (U+2011) so a narrow
	// column never splits "INV" from its digits.
	{Number: "INV‑018", Issued: "2026-09-01", AmountCents: 19600, Status: "paid"},
	{Number: "INV‑017", Issued: "2026-08-01", AmountCents: 19600, Status: "paid"},
	{Number: "INV‑016", Issued: "2026-07-01", AmountCents: 17150, Status: "paid"},
	{Number: "INV‑015", Issued: "2026-06-01", AmountCents: 17150, Status: "refunded"},
	{Number: "INV‑014", Issued: "2026-05-01", AmountCents: 14700, Status: "overdue"},
}

// dashboardInvoiceMonths maps an ISO month to its display spelling.
var dashboardInvoiceMonths = map[string]string{
	"01": "Jan", "02": "Feb", "03": "Mar", "04": "Apr", "05": "May", "06": "Jun",
	"07": "Jul", "08": "Aug", "09": "Sep", "10": "Oct", "11": "Nov", "12": "Dec",
}

// dashboardInvoiceDate renders 2026-09-01 as "Sep 1, 2026".
func dashboardInvoiceDate(iso string) string {
	if len(iso) != 10 {
		return iso
	}
	// Every row sits in one year, so month and day are enough, and
	// the column stays one line wide on a phone.
	month, day := iso[5:7], strings.TrimPrefix(iso[8:10], "0")
	return dashboardInvoiceMonths[month] + " " + day
}

// dashboardInvoiceBadge maps a status to its pill.
func dashboardInvoiceBadge(status string) render.HTML {
	switch status {
	case "paid":
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: "Paid", Variant: ui.StatusSuccess})
	case "refunded":
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: "Refunded", Variant: ui.StatusNeutral})
	default:
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: "Overdue", Variant: ui.StatusDanger})
	}
}

// dashboardSortedInvoices returns the fixture in the requested order:
// by issue date or amount, ascending or descending, newest-first when
// no column is active. Stable, so equal keys keep the fixture order.
func dashboardSortedInvoices(sortBy string, dir ui.SortDir) []dashboardInvoice {
	rows := append([]dashboardInvoice(nil), dashboardInvoices...)
	less := func(i, j int) bool {
		switch sortBy {
		case "amount":
			return rows[i].AmountCents < rows[j].AmountCents
		case "issued":
			return rows[i].Issued < rows[j].Issued
		default:
			return rows[i].Issued > rows[j].Issued // newest first
		}
	}
	if dir == ui.SortDesc {
		less = func(i, j int) bool {
			switch sortBy {
			case "amount":
				return rows[i].AmountCents > rows[j].AmountCents
			case "issued":
				return rows[i].Issued > rows[j].Issued
			default:
				return rows[i].Issued > rows[j].Issued
			}
		}
	}
	sort.SliceStable(rows, less)
	return rows
}

// dashboardInvoicesTable renders the five most recent invoices. The
// sort anchors carry the island contract beside their href: with the
// runtime the table swaps in place; without it the href navigates to
// this page carrying ?sort=&dir= and the page re-renders sorted.
func dashboardInvoicesTable(segment, sortBy string, dir ui.SortDir) render.HTML {
	rows := dashboardSortedInvoices(sortBy, dir)
	out := make([]ui.Row, len(rows))
	for i, inv := range rows {
		out[i] = ui.Row{
			ID: inv.Number,
			Cells: map[string]render.HTML{
				"invoice": render.Text(inv.Number),
				"issued":  render.Text(dashboardInvoiceDate(inv.Issued)),
				"amount":  render.Text(fmt.Sprintf("$%.2f", float64(inv.AmountCents)/100)),
				"status":  dashboardInvoiceBadge(inv.Status),
			},
		}
	}
	return ui.DataTable(ui.DataTableConfig{
		ID: "hd-invoices-table",
		// The caption names the table's scroll region for AT. It must
		// differ from the section heading's text ("Recent invoices"):
		// two landmarks with one name is the axe landmark-unique
		// finding. CaptionHidden keeps the visible heading the only
		// place the words appear.
		Caption:       "Invoice table, five most recent",
		CaptionHidden: true,
		Columns: []ui.Column{
			{Key: "invoice", Header: "Invoice"},
			{Key: "issued", Header: "Issued", Sortable: true},
			{Key: "amount", Header: "Amount", Sortable: true, Align: "end"},
			{Key: "status", Header: "Status"},
		},
		Rows: out,
		// The active sort travels. Path is this page under this theme:
		// the no-script href re-enters through Load, which reads the
		// same parameters back out.
		Path:    dashboardRoutePath(segment),
		SortBy:  sortBy,
		SortDir: dir,
		Island: headless.Island{
			Signal:   dashboardInvoicesSignal,
			Endpoint: dashboardInvoicesEndpoint(segment),
		},
	})
}

// dashboardInvoicesRegion is the signal-bound region the island
// response replaces.
func dashboardInvoicesRegion(segment, sortBy string, dir ui.SortDir) render.HTML {
	return interactive.BindHTML(
		html.Div(html.DivConfig{ID: "hd-invoices-region"}, dashboardInvoicesTable(segment, sortBy, dir)),
		dashboardInvoicesSignal)
}

// dashboardInvoicesSection wraps the table region in its section.
func dashboardInvoicesSection(r landingRoute, sortBy string, dir ui.SortDir) render.HTML {
	return ui.Section(ui.SectionConfig{
		ID:          "hd-invoices",
		Heading:     "Recent invoices",
		Description: "Five most recent. Sorting asks the server: an island swap with the runtime, a plain link without it.",
	}, dashboardInvoicesRegion(r.Segment, sortBy, dir))
}

// ── The invoices island handler ─────────────────────────────────────

const dashboardInvoicesSignal = "hd-invoices"

// dashboardInvoicesEndpoint is the island GET for one theme's page.
// The theme rides the endpoint path — not the page query — so the
// no-script href stays clean (?sort=&dir= alone) while the island
// answer can still build correct hrefs for the table it swaps in.
// Mounted in setupServer at /__site/headless/invoices/{theme}.
func dashboardInvoicesEndpoint(segment string) string {
	return "/__site/headless/invoices/" + segment
}

// serveHeadlessInvoices answers the invoice table's island GET: the
// sort anchor's query in, the re-rendered table out. The no-script
// path never reaches it — that path navigates and the page's Load
// answers — so there is no redirect face to mirror.
func serveHeadlessInvoices(w http.ResponseWriter, r *http.Request) {
	route, ok := landingRouteFor(router.Param(r, "theme"))
	if !ok {
		// The same line the sibling handlers hold: a wrong theme
		// would silently lie about which palette draws the answer.
		http.Error(w, "unknown theme", http.StatusBadRequest)
		return
	}
	sortBy := r.URL.Query().Get("sort")
	if sortBy != "issued" && sortBy != "amount" {
		sortBy = ""
	}
	var dir ui.SortDir
	switch d := r.URL.Query().Get("dir"); d {
	case string(ui.SortAsc), string(ui.SortDesc):
		dir = ui.SortDir(d)
	}
	render.RespondHTML(w, dashboardInvoicesTable(route.Segment, sortBy, dir))
}

// ── The handler ─────────────────────────────────────────────────────

// serveHeadlessSettings answers both the island RPC (JSON body → 200
// with the re-rendered region; the errors ARE the answer) and the
// no-script native POST (urlencoded body → 303 See Other back to the
// dashboard route, whose query carries the outcome alone). Mounted in
// setupServer.
func serveHeadlessSettings(w http.ResponseWriter, r *http.Request) {
	var form url.Values
	island := false
	switch {
	case strings.HasPrefix(r.Header.Get("Content-Type"), "application/json"):
		island = true
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		var body struct {
			Display  string `json:"display"`
			Password string `json:"password"`
			Notify   string `json:"notify"`
			Webhook  string `json:"webhook"`
			Theme    string `json:"theme"`
		}
		if err := handler.DecodeStrict(r.Body, &body); err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			// Constant on purpose: the parse error text is request
			// data and never belongs in the response.
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		form = url.Values{
			"display": {body.Display}, "password": {body.Password},
			"notify": {body.Notify}, "webhook": {body.Webhook},
			"theme": {body.Theme},
		}
	// The runtime's RPC sends multipart when the form carries a file
	// input (the avatar upload): FormData is the only body a file can
	// travel in, so that shape is an island answer too, with the
	// demo's memory cap on the in-memory part.
	case strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data"):
		island = true
		r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
		if err := r.ParseMultipartForm(8 << 10); err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		if r.MultipartForm == nil || len(r.MultipartForm.Value) == 0 {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		form = r.MultipartForm.Value
	default:
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if err := r.ParseForm(); err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		form = r.PostForm
	}
	segment := form.Get("theme")
	form.Del("theme")

	route, ok := landingRouteFor(segment)
	if !ok {
		http.Error(w, "unknown theme", http.StatusBadRequest)
		return
	}

	outcome, errs := dashboardValidate(form)
	state := dashboardSettingsState{Errors: errs, Done: outcome == dashboardSettingsOK}
	if island {
		// The island answer is a render of its own, so it resolves
		// its words from the request the same way the page did.
		render.RespondHTML(w, renderDashboardSettings(r.Context(), route, state))
		return
	}
	// Post-redirect-get: the outcome lives in the dashboard route's
	// query, so a refresh or a back button never re-POSTs. The typed
	// values do not travel — a refused password belongs in no
	// history and no referrer.
	q := url.Values{"settings": {string(outcome)}}
	http.Redirect(w, r, dashboardRoutePath(route.Segment)+"?"+q.Encode(), http.StatusSeeOther)
}
