package main

// Every screen and fill component. Screens render the main slot;
// fills target the shell's toolbar and aside outlets. Real copy
// everywhere — this is a product surface, not a fixture.

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// --- icons -----------------------------------------------------------

// The few line icons the app needs, registered in the framework's
// icon registry so every consumer styles them through the same
// stroke/currentColor contract.
func init() {
	ui.RegisterIcon("inbox", `<polyline points="22 12 16 12 14 15 10 15 8 12 2 12"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/>`)
	ui.RegisterIcon("folder", `<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>`)
	ui.RegisterIcon("bar-chart", `<line x1="18" y1="20" x2="18" y2="10"/><line x1="12" y1="20" x2="12" y2="4"/><line x1="6" y1="20" x2="6" y2="14"/>`)
	ui.RegisterIcon("sliders", `<line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="1" y1="14" x2="7" y2="14"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="17" y1="16" x2="23" y2="16"/>`)
	ui.RegisterIcon("chevron-left", `<polyline points="15 18 9 12 15 6"/>`)
}

// --- sidebar ---------------------------------------------------------

// sidebarNavConfig builds the shell's nav: Inbox, the four projects
// under Projects, Reports, Settings. CurrentPath stays unset: the
// sidebar is kept chrome whose build never re-runs, so the runtime's
// activelink sweep owns the current entry from its load (first paint
// marks nothing for the few hundred ms before the idle load).
func sidebarNavConfig() ui.SidebarConfig {
	projectItems := make([]ui.SidebarItem, 0, len(projects))
	for _, p := range projects {
		projectItems = append(projectItems, ui.SidebarItem{
			Label:     p.Name,
			Href:      "/projects/" + p.Slug,
			MatchPath: "/projects/" + p.Slug,
		})
	}
	return ui.SidebarConfig{
		NavLabel: "Primary",
		// The drawer's header brand: the same name the top bar shows
		// (the layout does not expose the brand to the shell build).
		DrawerTitle:           "Acme Tracker",
		NativeMobile:          true,
		SuppressDrawerTrigger: true,
		Items: []ui.SidebarItem{
			{Label: "Inbox", Href: "/inbox", Icon: ui.Icon("inbox", ui.IconConfig{}), MatchPath: "/inbox"},
			{
				Label:     "Projects",
				Icon:      ui.Icon("folder", ui.IconConfig{}),
				Open:      true,
				Children:  projectItems,
				MatchPath: "/projects",
			},
			{Label: "Reports", Href: "/reports", Icon: ui.Icon("bar-chart", ui.IconConfig{}), MatchPath: "/reports"},
			{Label: "Settings", Href: "/settings", Icon: ui.Icon("sliders", ui.IconConfig{}), MatchPath: "/settings"},
		},
	}
}

// --- Home -------------------------------------------------------------

// HomeScreen is the dashboard: the fact band fills fast, the aside's
// recent activity streams in behind it.
type HomeScreen struct{}

func (s *HomeScreen) Render() render.HTML {
	facts := ui.MetricBand(ui.MetricBandConfig{Label: "Issue totals", Items: []ui.MetricBandItem{
		{Label: "Open issues", Value: strconv.Itoa(countByStatus(statusOpen))},
		{Label: "In progress", Value: strconv.Itoa(countByStatus(statusProgress))},
		{Label: "Blocked", Value: strconv.Itoa(countByStatus(statusBlocked))},
		{Label: "Done this week", Value: strconv.Itoa(countDoneThisWeek())},
	}})
	cards := projectCards()
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Overview")),
		facts,
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Projects")),
		ui.Grid(ui.GridConfig{Min: "17rem", Gap: ui.GapMD}, cards...),
	)
}

// projectCards renders one linked card per project: name, health,
// open count, and the opened-per-week trend.
func projectCards() []render.HTML {
	cards := make([]render.HTML, 0, len(projects))
	for _, p := range projects {
		p := p
		cards = append(cards, ui.Card(ui.CardConfig{
			Href:         "/projects/" + p.Slug,
			Heading:      p.Name,
			HeadingLevel: 2,
			Description:  p.Summary,
			Footer: render.Join(
				ui.StatusBadge(ui.StatusBadgeConfig{Label: p.Health, Variant: p.Tone}),
				html.Span(html.TextConfig{},
					render.Text(strconv.Itoa(openCount(p.Slug))+" open")),
			),
		},
			ui.Sparkline(ui.SparklineConfig{
				Values: p.Trend, Width: 220, Height: 36,
				Shape: ui.SparklineArea, FullWidth: true,
			}),
		))
	}
	return cards
}

// ProjectsIndexScreen lists the projects — the "Projects" crumb and
// the sidebar's group heading both land here.
type ProjectsIndexScreen struct{}

func (s *ProjectsIndexScreen) Render() render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Projects")),
		ui.Muted(render.Text("Four projects, one tracker. Each card opens the project's issue list.")),
		ui.Grid(ui.GridConfig{Min: "17rem", Gap: ui.GapMD}, projectCards()...),
	)
}

// RecentActivityPanel fills the home aside: the cross-project feed,
// deliberately slow so it arrives as its own deferred part request.
type RecentActivityPanel struct{}

func (p *RecentActivityPanel) Load(ctx context.Context) error {
	select {
	case <-time.After(900 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *RecentActivityPanel) Render() render.HTML {
	events := recentEvents(6)
	items := make([]ui.TimelineEvent, 0, len(events))
	for _, e := range events {
		items = append(items, ui.TimelineEvent{
			Title: e.Author,
			Meta:  e.On,
			Body: html.Paragraph(html.TextConfig{},
				render.Text(e.Summary)),
			Variant: ui.TimelineInfo,
		})
	}
	return ui.Card(ui.CardConfig{Heading: "Recent activity", HeadingLevel: 2},
		ui.Timeline(ui.TimelineConfig{Events: items}))
}

// --- Inbox -------------------------------------------------------------

// InboxScreen is the notification list.
type InboxScreen struct{}

func (s *InboxScreen) Render() render.HTML {
	rows := make([]render.HTML, 0, len(notifications))
	for _, n := range notifications {
		var unread render.HTML
		if n.Unread {
			unread = ui.StatusBadge(ui.StatusBadgeConfig{Label: "Unread", Variant: ui.StatusInfo})
		}
		rows = append(rows, ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD},
			unread,
			ui.Avatar(ui.AvatarConfig{Name: n.Author, Size: ui.AvatarSm}),
			ui.Stack(ui.StackConfig{Gap: ui.GapSM},
				html.Paragraph(html.TextConfig{},
					render.Text(n.Author+" "),
					ui.Muted(render.Text(n.Text))),
				html.Span(html.TextConfig{}, render.Text(n.On)),
			),
		))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Inbox")),
		ui.Card(ui.CardConfig{Variant: ui.CardFlat}, render.Join(rows...)),
	)
}

// InboxToolbar fills the inbox page's toolbar: one quiet action.
type InboxToolbar struct{}

func (t *InboxToolbar) Render() render.HTML {
	return ui.Toolbar(ui.ToolbarConfig{Label: "Inbox actions", Groups: []ui.ToolbarGroup{{
		Children: []render.HTML{
			ui.Button(ui.ButtonConfig{Label: "Mark all read", Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall}),
		},
	}}})
}

// --- Reports -------------------------------------------------------------

// ReportsScreen is the slow page: its Load takes about a second and a
// quarter, so the route's declared skeleton covers the swap slot
// while it is in flight.
type ReportsScreen struct{}

func (s *ReportsScreen) Load(ctx context.Context) error {
	select {
	case <-time.After(1200 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *ReportsScreen) Render() render.HTML {
	cards := make([]render.HTML, 0, len(projects))
	for _, p := range projects {
		p := p
		open, inProgress, blocked, done := statusCounts(p.Slug)
		id := "tracker-report-" + p.Slug
		cards = append(cards, ui.Card(ui.CardConfig{
			Heading:      p.Name,
			HeadingLevel: 2,
			Description:  strconv.Itoa(openCount(p.Slug)) + " open",
			ID:           id,
			Footer: render.Join(
				// Health reads as state, the same badge the
				// project header and cards use — not prose.
				ui.StatusBadge(ui.StatusBadgeConfig{Label: p.Health, Variant: p.Tone}),
				ui.Muted(render.Text("opened per week")),
			),
		},
			ui.BarChart(ui.BarChartConfig{
				LabelledBy: id,
				ShowLabels: true,
				FitHeight:  true,
				Bars: []ui.BarChartBar{
					{Label: "Open", Value: float64(open), Color: "info"},
					{Label: "In progress", Value: float64(inProgress), Color: "var(--color-primary)"},
					{Label: "Blocked", Value: float64(blocked), Color: "danger"},
					{Label: "Done", Value: float64(done), Color: "success"},
				},
			}),
			ui.Sparkline(ui.SparklineConfig{
				Values: p.Trend, Width: 260, Height: 40,
				Shape: ui.SparklineArea, LabelledBy: id, FullWidth: true,
			}),
		))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Reports")),
		ui.Muted(render.Text("Issue counts by status for each project, with the opened-per-week trend behind them.")),
		ui.LinkButton(ui.LinkButtonConfig{
			Href: "/reports/export", Label: "Export CSV", Icon: "download",
		}),
		ui.Grid(ui.GridConfig{Min: "19rem", Gap: ui.GapMD}, cards...),
	)
}

// --- Settings -------------------------------------------------------------

// SettingsScreen is the short profile form. The aside is left
// unfilled on purpose: the outlet renders nothing.
type SettingsScreen struct{}

func (s *SettingsScreen) Render() render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Settings")),
		ui.Card(ui.CardConfig{Heading: "Workspace", HeadingLevel: 2},
			ui.Form(ui.FormConfig{
				Action:      "/settings",
				Method:      "GET",
				SubmitLabel: "Save changes",
			},
				ui.TextField(ui.TextFieldConfig{
					Name:  "name",
					Label: "Workspace name",
					Value: "Acme Workspace",
				}),
				ui.FormField(ui.FormFieldConfig{
					Label: "Contact email",
					For:   "set-email",
					Help:  "Invoices and security notices go here.",
					Input: func(fc headless.FieldControl) render.HTML {
						return ui.Control(ui.ControlConfig{
							Field: fc, Type: "email", Name: "email",
							Value: "ops@acme.example", AutoComplete: "email",
						})
					},
				}),
				ui.Select(ui.SelectConfig{
					Name:  "theme",
					Label: "Theme",
					Options: []ui.SelectOption{
						{Value: "system", Text: "Match this device", Selected: true},
						{Value: "light", Text: "Light"},
						{Value: "dark", Text: "Dark"},
					},
				}),
				ui.Switch(ui.ToggleConfig{
					Name:    "email_digest",
					Label:   "Email me a Monday digest",
					Checked: true,
					Help:    "One summary of what moved across your projects.",
				}),
			),
		),
	)
}

// --- Not found -------------------------------------------------------------

// NotFoundScreen renders an unknown URL inside the shell.
type NotFoundScreen struct{}

func (s *NotFoundScreen) Render() render.HTML {
	return ui.EmptyState(ui.EmptyStateConfig{
		Title:        "Page not found",
		HeadingLevel: 1,
		Description:  "The address you followed does not exist in this workspace. It may have moved, or it never did.",
		Action:       ui.LinkButton(ui.LinkButtonConfig{Label: "Back to overview", Href: "/"}),
	})
}

// ErrorScreen renders a whole-page failure (LEG-40: the Legacy
// project's issue store is down) inside the shell, matching the
// NotFoundScreen's shape: the same empty-state card, one honest
// sentence, one quiet way out. No error text ever reaches it.
type ErrorScreen struct{}

func (s *ErrorScreen) Render() render.HTML {
	return ui.EmptyState(ui.EmptyStateConfig{
		Title:        "Something went wrong",
		HeadingLevel: 1,
		Description:  "This page could not be loaded. Try again in a moment.",
		Action:       ui.LinkButton(ui.LinkButtonConfig{Label: "Back to overview", Href: "/"}),
	})
}

// --- The project layer -----------------------------------------------------

// newProjectLayout builds the layer kept while you move between one
// project's issues: the project header and the issue list are static
// chrome; the detail slot (the layer's primary) is the swap target
// and slides in from the right, and back the other way. On phones
// ListDetail shows one pane at a time; l.VTRegion names that pane region.
func newProjectLayout() *uiapp.Layout {
	slide := uiapp.Slide(uiapp.Right, 220*time.Millisecond)
	slide.Narrow = "768px" // ListDetail's phone breakpoint.
	return uiapp.NewLayout("project", uiapp.LayoutSpec{
		Primary: uiapp.PrimaryConfig{
			// The move is picked from the RESOLVED project: Billing
			// slides, Search fades through (its two trails are text
			// the root crossfade would ghost), Auth crossfades, Legacy
			// — the archived project nobody hurries — just fades. The
			// stylesheet covers the set, sorted and static; Back
			// mirrors the edge through the recorded pick.
			Transitions: map[string]uiapp.Transition{
				"slide":        slide,
				"fade-through": uiapp.FadeThrough(200 * time.Millisecond),
				"crossfade":    uiapp.Crossfade(200 * time.Millisecond),
				"fade":         uiapp.Crossfade(420 * time.Millisecond),
			},
			TransitionFor: func(ctx context.Context) string {
				p, err := resolvedProject.Get(ctx)
				if err != nil {
					return ""
				}
				switch p.Slug {
				case "billing":
					return "slide"
				case "search":
					return "fade-through"
				case "auth":
					return "crossfade"
				case "legacy":
					return "fade"
				}
				return ""
			},
		},
	}, buildProject)
}

// buildProject renders the project layer: header (name, health, open
// count) and the issue list pane beside the detail slot. The layer is
// KEPT while you move between one project's issues, so nothing here
// may read route state — the current row's mark is the runtime's
// (activelink stamps aria-current on the exact-href match), and a
// server mark read off the entering match would go stale on the very
// next click.
func buildProject(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
	p, err := resolvedProject.Get(ctx)
	if err != nil {
		return l.Primary()
	}
	filter, err := resolvedFilter.Get(ctx)
	if err != nil {
		filter = "" // the query read cannot fail; an empty filter lists every issue
	}
	rows := make([]render.HTML, 0, len(issuesBySlug[p.Slug]))
	for _, iss := range issuesBySlug[p.Slug] {
		if strings.Contains(strings.ToLower(iss.Key()+" "+iss.Title+" "+iss.Status+" "+iss.Assignee), strings.ToLower(filter)) {
			rows = append(rows, issueRow(p.Slug, iss, filter))
		}
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{Compact: true, Title: p.Name, Subtitle: strconv.Itoa(openCount(p.Slug)) + " open · " + strconv.Itoa(len(issuesBySlug[p.Slug])) + " issues",
			Badge: ui.StatusBadge(ui.StatusBadgeConfig{Label: p.Health, Variant: p.Tone})}),
		ui.ListDetail(ui.ListDetailConfig{
			ListLabel: "Issues", ExtraAttrs: l.VTRegion(), MobileSinglePane: true,
			BackHref: "/projects/" + p.Slug, BackLabel: "Back to issues",
			List: ui.Stack(ui.StackConfig{Gap: ui.GapNone},
				ui.FilterToolbar(ui.FilterToolbarConfig{Compact: true, Action: "/projects/" + p.Slug,
					Search: &ui.FilterSearch{Name: "filter", Value: filter, Placeholder: "Filter issues", Label: "Filter issues"}}),
				ui.Stack(ui.StackConfig{Gap: ui.GapNone}, rows...)),
			Detail: l.Primary(),
		}),
	)
}

// issueRow renders one list row: the key, the title, the status, the
// assignee. The rows live in a KEPT layer, so the current row's mark
// is the runtime's alone: the activelink sweep stamps
// aria-current="page" on the exact-href match and CardRow highlights it.
func issueRow(slug string, iss Issue, filter string) render.HTML {
	href := "/projects/" + slug + "/issues/" + strconv.Itoa(iss.Number)
	if filter != "" {
		href += "?filter=" + url.QueryEscape(filter)
	}
	return ui.Card(ui.CardConfig{
		Variant: ui.CardRow, Href: href, Heading: iss.Key(), HeadingLevel: 2, Description: iss.Title,
		ExtraAttrs: html.Attrs{"data-key": iss.Key()},
	}, ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS, NoWrap: true},
		ui.StatusBadge(ui.StatusBadgeConfig{Label: iss.Status, Variant: statusVariant(iss.Status)}),
		ui.Avatar(ui.AvatarConfig{Name: iss.Assignee, Size: ui.AvatarSm})))
}

// ProjectIndexScreen is what the detail slot shows before an issue is
// picked: a quiet pointer to the list. The project comes from the
// group's resolver.
type ProjectIndexScreen struct {
	project string
}

func (s *ProjectIndexScreen) SetParams(m map[string]string) {}

func (s *ProjectIndexScreen) Load(ctx context.Context) error {
	p, err := resolvedProject.Get(ctx)
	if err != nil {
		return err
	}
	s.project = p.Slug
	return nil
}

func (s *ProjectIndexScreen) ScreenTitle() string {
	if p, ok := projectBySlug(s.project); ok {
		return p.Name
	}
	return "Project"
}

// StaticPaths exports the four project index pages: the route is
// parametric now, the builder needs the concrete slugs.
func (s *ProjectIndexScreen) StaticPaths(ctx context.Context) []map[string]string {
	out := make([]map[string]string, 0, len(projects))
	for _, p := range projects {
		out = append(out, map[string]string{"project": p.Slug})
	}
	return out
}

func (s *ProjectIndexScreen) Render() render.HTML {
	return ui.ListDetailPlaceholder(ui.EmptyState(ui.EmptyStateConfig{
		Title:        "No issue selected",
		HeadingLevel: 2,
		Description:  "Pick an issue from the list to read its history, or filter the list to find one.",
	}))
}

// ProjectToolbar is the group fill every project page carries.
type ProjectToolbar struct{}

func (t *ProjectToolbar) Render() render.HTML {
	return ui.Toolbar(ui.ToolbarConfig{Plain: true, Label: "Project actions", Groups: []ui.ToolbarGroup{{
		Children: []render.HTML{
			ui.Button(ui.ButtonConfig{Label: "New issue", Size: ui.ButtonSizeSmall}),
		},
	}}})
}

// ProjectOverviewPanel fills the project index's aside.
type ProjectOverviewPanel struct {
	project string
}

func (p *ProjectOverviewPanel) Load(ctx context.Context) error {
	proj, err := resolvedProject.Get(ctx)
	if err != nil {
		return err
	}
	p.project = proj.Slug
	return nil
}

func (p *ProjectOverviewPanel) Render() render.HTML {
	proj, _ := projectBySlug(p.project)
	open, inProgress, blocked, done := statusCounts(p.project)
	team := projectTeam(p.project)
	avatars := make([]ui.AvatarConfig, 0, len(team))
	for _, name := range team {
		avatars = append(avatars, ui.AvatarConfig{Name: name, Size: ui.AvatarSm})
	}
	return ui.Card(ui.CardConfig{Heading: "About this project", HeadingLevel: 2},
		ui.Stack(ui.StackConfig{Gap: ui.GapMD},
			html.Paragraph(html.TextConfig{}, render.Text(proj.Summary)),
			ui.DetailList(ui.DetailListConfig{Inline: true, Items: []ui.DetailItem{
				{Label: "Health", Value: ui.StatusBadge(ui.StatusBadgeConfig{Label: proj.Health, Variant: proj.Tone})},
				{Label: "Open", Value: render.Text(strconv.Itoa(open))},
				{Label: "In progress", Value: render.Text(strconv.Itoa(inProgress))},
				{Label: "Blocked", Value: render.Text(strconv.Itoa(blocked))},
				{Label: "Done", Value: render.Text(strconv.Itoa(done))},
				{Label: "Team", Value: ui.AvatarGroup(ui.AvatarGroupConfig{
					Avatars: avatars, Max: 6, Label: "Team", ShowNames: true,
				})},
			}}),
		),
	)
}

// projectTeam lists the distinct assignees working a project.
func projectTeam(slug string) []string {
	seen := map[string]bool{}
	var out []string
	for _, iss := range issuesBySlug[slug] {
		if iss.Assignee == "" || seen[iss.Assignee] {
			continue
		}
		seen[iss.Assignee] = true
		out = append(out, iss.Assignee)
	}
	return out
}

// --- The issue page ---------------------------------------------------------

// IssueScreen renders the detail slot: the issue itself. The project
// and the issue both come from resolvers (the group's "project", this
// screen's "issue"); an unknown number renders the not-found card in
// place, an unavailable store fails the page (LEG-40).
type IssueScreen struct {
	project string
	n       string
	iss     Issue
	found   bool
}

// issueResolver is resolvedIssue's resolver body: the issue the route
// names, inside the resolved project. An unknown number resolves to a
// nil issue (the page renders its not-found card in place); the
// Legacy project's issue STORE being unavailable is a page error —
// the datastore is down, not the route (LEG-40 is the reachable case).
func issueResolver(ctx context.Context) (*Issue, error) {
	p, err := resolvedProject.Get(ctx)
	if err != nil {
		return nil, err
	}
	m, _ := uiapp.MatchFromContext(ctx)
	iss, ok := issueByNumber(p.Slug, atoiOr(m.Param("n"), 0))
	if !ok {
		return nil, nil
	}
	if p.Slug == "legacy" && iss.Number == 40 {
		return nil, errors.New("tracker: the Legacy project's issue store is unavailable")
	}
	return &iss, nil
}

func (s *IssueScreen) SetParams(m map[string]string) { s.n = m["n"] }

// ScreenTitle makes the route's title dynamic once the issue is known.
func (s *IssueScreen) ScreenTitle() string {
	if s.found {
		return s.iss.Key() + " · " + s.iss.Title
	}
	return "Issue"
}

func (s *IssueScreen) Load(ctx context.Context) error {
	p, err := resolvedProject.Get(ctx)
	if err != nil {
		return err
	}
	s.project = p.Slug
	iss, err := resolvedIssue.Get(ctx)
	if err != nil {
		return err
	}
	if iss != nil {
		s.iss, s.found = *iss, true
	}
	return nil
}

// StaticPaths exports every project's issue pages except the ones a
// live server contains: the Legacy project (its store can fail,
// LEG-40) and the one malformed record (BIL-63's activity boundary is
// a live-server answer, not bytes to bake).
func (s *IssueScreen) StaticPaths(ctx context.Context) []map[string]string {
	var out []map[string]string
	for _, p := range projects {
		if p.Slug == "legacy" {
			continue
		}
		for _, iss := range issuesBySlug[p.Slug] {
			if malformedActivity(iss) {
				continue
			}
			out = append(out, map[string]string{"project": p.Slug, "n": strconv.Itoa(iss.Number)})
		}
	}
	return out
}

// malformedActivity reports whether the issue's feed holds the one
// unparseable event.
func malformedActivity(iss Issue) bool {
	for _, c := range iss.Comments {
		if c.Malformed {
			return true
		}
	}
	return false
}

func (s *IssueScreen) Render() render.HTML {
	iss, ok := issueByNumber(s.project, atoiOr(s.n, 0))
	if !ok {
		return ui.EmptyState(ui.EmptyStateConfig{
			Title:        "Issue not found",
			HeadingLevel: 2,
			Description:  "This issue number does not exist in the project.",
		})
	}
	paras := make([]render.HTML, 0, len(iss.Paragraphs))
	for _, p := range iss.Paragraphs {
		paras = append(paras, html.Paragraph(html.TextConfig{}, render.Text(p)))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapMD, ID: "issue-detail"},
		ui.Stack(ui.StackConfig{Gap: ui.GapSM},
			ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
				html.Span(html.TextConfig{}, render.Text(iss.Key())),
				ui.Tag(ui.TagConfig{Label: iss.Priority, Variant: priorityVariant(iss.Priority)}),
			),
			ui.PageHeader(ui.PageHeaderConfig{Title: iss.Title, HeadingLevel: 2, Compact: true}),
			ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
				ui.StatusBadge(ui.StatusBadgeConfig{Label: iss.Status, Variant: statusVariant(iss.Status)}),
				ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
					ui.Avatar(ui.AvatarConfig{Name: iss.Assignee, Size: ui.AvatarSm}),
					render.Text(iss.Assignee)),
			),
		),
		ui.DetailList(ui.DetailListConfig{Inline: true, Items: []ui.DetailItem{
			{Label: "Status", Value: render.Text(iss.Status)},
			{Label: "Priority", Value: render.Text(iss.Priority)},
			{Label: "Assignee", Value: render.Text(iss.Assignee)},
			{Label: "Reporter", Value: render.Text(iss.Reporter)},
			{Label: "Created", Value: render.Text(iss.Created)},
			{Label: "Updated", Value: render.Text(iss.Updated)},
		}}),
		ui.Stack(ui.StackConfig{Gap: ui.GapMD, TrimMargins: true}, paras...),
	)
}

// IssueToolbar fills an issue page's toolbar with its actions. They
// are honest buttons with no wiring: the showcase's moves are layout,
// not mutations.
type IssueToolbar struct {
	project string
	n       string
}

func (t *IssueToolbar) SetParams(m map[string]string) { t.n = m["n"] }

// Load declines the fill when the issue does not exist: an unknown id
// keeps the project's toolbar (the group fill's New issue) instead of
// actions on nothing. Every other page keeps its own error semantics.
func (t *IssueToolbar) Load(ctx context.Context) error {
	p, err := resolvedProject.Get(ctx)
	if err != nil {
		return err
	}
	t.project = p.Slug
	if _, ok := issueByNumber(t.project, atoiOr(t.n, 0)); !ok {
		return uiapp.ErrNoFill
	}
	return nil
}

func (t *IssueToolbar) Render() render.HTML {
	return ui.Toolbar(ui.ToolbarConfig{Plain: true, Label: "Issue actions", Groups: []ui.ToolbarGroup{{
		Children: []render.HTML{
			ui.Button(ui.ButtonConfig{Label: "Assign", Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall}),
			ui.Button(ui.ButtonConfig{Label: "Change status", Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall}),
			ui.Button(ui.ButtonConfig{Label: "Close issue", Variant: ui.ButtonDanger, Size: ui.ButtonSizeSmall}),
		},
	}}})
}

// ActivityPanel fills an issue page's aside: the activity timeline,
// which takes about 700 ms to gather (it arrives as its own deferred
// part). One record's feed is malformed (BIL-63: a data bug on that
// one issue) — its loader panics and the ErrorBoundary below shows
// the short notice, the rest of the page intact.
type ActivityPanel struct {
	project string
	n       string
}

func (p *ActivityPanel) SetParams(m map[string]string) { p.n = m["n"] }

func (p *ActivityPanel) Load(ctx context.Context) error {
	proj, err := resolvedProject.Get(ctx)
	if err != nil {
		return err
	}
	p.project = proj.Slug
	if iss, ok := issueByNumber(p.project, atoiOr(p.n, 0)); ok && malformedActivity(iss) {
		// A data bug on this one record: the event payload cannot be
		// parsed. The panic takes the same channel a Load error takes,
		// and the boundary renders.
		panic("tracker: malformed event payload on " + iss.Key())
	}
	select {
	case <-time.After(700 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RenderError is the panel's ErrorBoundary: a short notice, never the
// panic text.
func (p *ActivityPanel) RenderError(err error) render.HTML {
	return ui.Card(ui.CardConfig{Variant: ui.CardFlat},
		ui.Stack(ui.StackConfig{Gap: ui.GapSM},
			ui.Icon("activity", ui.IconConfig{Size: "20"}),
			html.Paragraph(html.TextConfig{},
				render.Text("Activity could not load.")),
			ui.Muted(render.Text("This issue's event feed has a bad record. The rest of the page is unaffected.")),
		),
	)
}

func (p *ActivityPanel) Render() render.HTML {
	iss, ok := issueByNumber(p.project, atoiOr(p.n, 0))
	if !ok {
		return ""
	}
	events := []ui.TimelineEvent{{
		Title:   iss.Reporter + " reported this issue",
		Meta:    iss.Created,
		Variant: ui.TimelineNeutral,
	}}
	for _, c := range iss.Comments {
		events = append(events, ui.TimelineEvent{
			Title:   c.Author + " commented",
			Meta:    c.On,
			Body:    html.Paragraph(html.TextConfig{}, render.Text(c.Text)),
			Variant: ui.TimelineInfo,
		})
	}
	events = append(events, ui.TimelineEvent{
		Title:   "Status set to " + iss.Status,
		Meta:    iss.Updated,
		Variant: ui.TimelineSuccess,
	})
	return ui.Card(ui.CardConfig{Heading: "Activity", HeadingLevel: 2},
		ui.Timeline(ui.TimelineConfig{Events: events}))
}

// ArchivedActivity is the Legacy project's region-guard alt: an
// archived project's activity is read only in the old system, so the
// aside says that instead of a feed it cannot serve.
type ArchivedActivity struct{}

func (c *ArchivedActivity) Render() render.HTML {
	return ui.Card(ui.CardConfig{Variant: ui.CardFlat},
		ui.Stack(ui.StackConfig{Gap: ui.GapSM},
			ui.Icon("archive", ui.IconConfig{Size: "20"}),
			html.Paragraph(html.TextConfig{},
				render.Text("Archived project, activity is read only in the old system.")),
			ui.Muted(render.Text("Legacy issues keep their history in the pre-2023 tracker.")),
		),
	)
}

// ExportScreen is the Reports export target: the exporter itself is
// down, its policy says so — a soft navigation answers 503 text/plain
// and the runtime's toast names it.
type ExportScreen struct{}

func (s *ExportScreen) Render() render.HTML {
	return ui.EmptyState(ui.EmptyStateConfig{
		Title:        "Export unavailable",
		HeadingLevel: 1,
		Description:  "The CSV exporter is down for maintenance. Try again later.",
	})
}

// --- helpers -----------------------------------------------------------------

func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
