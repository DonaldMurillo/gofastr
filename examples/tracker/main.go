// Package main is Acme Tracker, the layout showcase as a real app: a
// small issue tracker built only from the framework's components and
// typed theme, where every tree-layout feature appears as ordinary
// product behaviour — no outlined boxes, no marker strings. The lab
// (examples/layoutlab) keeps the test fixtures; this is what the
// features look like worn in.
//
// Run with:
//
//	go run ./examples/tracker        # :8096, or $PORT
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/examples/tracker/appbar"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/static"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// defaultAddr is the fallback when $PORT is unset; `gofastr dev` and
// PaaS runtimes inject PORT and isolation.ListenAddr honours it.
const defaultAddr = ":8096"

// trackerShell and trackerProject are the two tree layouts, built in
// buildSite.
var (
	trackerShell   *uiapp.Layout
	trackerProject *uiapp.Layout
	// The shell's outlet handles: the screens' fills and the build's
	// placements name the same typed values.
	trackerToolbar *uiapp.Outlet
	trackerAside   *uiapp.Outlet
)

// The project group's resolver keys: the group declares them, and the
// screens, fills and guards read the same typed values.
var (
	resolvedProject = uiapp.NewKey[Project]("project")
	resolvedIssue   = uiapp.NewKey[*Issue]("issue")
	resolvedFilter  = uiapp.NewKey[string]("filter")
)

// bellTrigger is the notification bell's markup; the paired popover is
// mounted once in buildApp.
var bellTrigger render.HTML

func main() {
	// The slow aside fill is a DEFERRED outlet
	// it arrives as its own part request beside the page, visible in
	// DevTools with its body. Fill errors and concurrency need no
	// configuration: containment and concurrent loading are the only
	// behaviours the core ships.
	fwApp := buildApp()
	if dir := exportDir(os.Args[1:]); dir != "" {
		if err := exportTo(context.Background(), fwApp, dir); err != nil {
			log.Fatalf("export: %v", err)
		}
		log.Printf("tracker exported to %s", dir)
		return
	}
	listenAddr, err := isolation.ListenAddr(".", defaultAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("tracker listening on http://localhost%s", listenAddr)
	if err := fwApp.Start(listenAddr); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// exportTo renders the app to static HTML + assets, like the other
// examples. Every dynamic route carries StaticPaths; the Legacy
// project's issue pages are deliberately absent (see
// IssueScreen.StaticPaths for why).
func exportTo(ctx context.Context, fwApp *framework.App, dir string) error {
	var host *uihost.UIHost
	for _, m := range fwApp.Mountables() {
		if h, ok := m.(*uihost.UIHost); ok {
			host = h
			break
		}
	}
	if host == nil {
		return errors.New("no uihost.UIHost mounted")
	}
	if _, err := (&static.Builder{Host: host, OutDir: dir}).Build(ctx); err != nil {
		return err
	}
	return nil
}

// exportDir scans args for `--export <dir>` or `--export=<dir>`, the
// examples/site spelling: the target directory, or "" to serve live.
func exportDir(args []string) string {
	for i := range args {
		switch {
		case args[i] == "--export" && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(args[i], "--export="):
			return strings.TrimPrefix(args[i], "--export=")
		}
	}
	return ""
}

// routerMounter adapts *router.Router to ui.MountSidebar's mounter.
type routerMounter struct{ r *router.Router }

func (m routerMounter) MountWidget(def *widget.Definition) {
	widget.Mount(m.r, def)
}

// buildApp wires the app into a framework app without binding a port,
// so tests drive it in-process.
func buildApp() *framework.App {
	site := buildSite()
	host := uihost.New(site,
		// An unknown URL renders inside the shell like any other page.
		uihost.WithNotFoundScreen(&NotFoundScreen{}),
		// A whole-page failure (LEG-40's store) gets the tracker's own
		// screen through the shell, matching the not-found page.
		uihost.WithErrorScreen(&ErrorScreen{}),
	)
	fwApp := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "tracker"}),
	)
	ui.MountSidebar(routerMounter{fwApp.Router()}, sidebarNavConfig())
	fwApp.Use(host.RouteMatchMiddleware())
	fwApp.Mount(host)

	bellDef := bellPopover.Build()
	widget.Mount(fwApp.Router(), &bellDef)
	return fwApp
}

// bellPopover is built beside its trigger at init.
var bellPopover = func() *widget.Builder {
	var pop *widget.Builder
	bellTrigger, pop = ui.NotificationBell(ui.NotificationBellConfig{
		Name:        "tracker-bell",
		Href:        "/inbox",
		Label:       "Notifications",
		UnreadCount: unreadNotificationCount(),
		Items:       bellItems(),
	})
	return pop
}()

// bellItems renders the inbox's latest entries for the popover.
func bellItems() []ui.NotificationItem {
	items := make([]ui.NotificationItem, 0, len(notifications))
	for _, n := range notifications {
		items = append(items, ui.NotificationItem{
			Title:  n.Author,
			Body:   n.Text,
			Time:   n.On,
			Href:   issueHref(n.Num),
			Unread: n.Unread,
		})
	}
	return items
}

// issueHref links an issue number under its project.
func issueHref(num int) string {
	if num == 0 {
		return "/inbox"
	}
	p := issueProject(num)
	return "/projects/" + p.Slug + "/issues/" + strconv.Itoa(num)
}

// buildSite declares the screens, the shell, and the project layer.
func buildSite() *uiapp.App {
	site := uiapp.NewApp("Acme Tracker")
	site.WithTheme(theme.Default(theme.Overrides{
		// Acme's own accent over the framework palette: a deep teal
		// that keeps 4.5:1 on the light surface, with its dark twin.
		Primary: "#0F766E",
		Dark:    &theme.Overrides{Primary: "#5EEAD4"},
		// The app bar's own tokens (the search field's width).
	}).Extend(appbar.Tokens))

	// The shell: kept everywhere. A top bar (brand, global search,
	// bell, theme toggle), the sidebar nav, a toolbar row across the
	// content (breadcrumbs as a route area the server re-renders on
	// every navigation, plus the page's own action buttons in the
	// toolbar outlet), the main slot, and the aside outlet on the
	// right. The aside's Default is the quiet "activity is not
	// available" card a contained fill failure degrades to (Legacy
	// issue pages); pages that simply do not fill the aside render it
	// empty — the Default declines unless the route is a Legacy
	// issue.
	trackerToolbar = uiapp.NewOutlet("toolbar", uiapp.OutletOptions{
		Loading: &uiapp.Loading{
			Show:  uiapp.LoadingComponent(ui.Spinner(ui.SpinnerConfig{Label: "Loading actions"})),
			After: 200 * time.Millisecond,
			Min:   300 * time.Millisecond,
		},
		// FadeThrough, not Crossfade: the toolbar's two button
		// rows crossed at half opacity each on every project
		// navigation (U1's garbled phone frame) — sequential
		// legs keep one row readable at a time.
		Transition: uiapp.FadeThrough(150 * time.Millisecond),
	})
	trackerAside = uiapp.NewOutlet("aside", uiapp.OutletOptions{
		// Deferred the activity feeds
		// (home's recent activity at ~900 ms, an issue's at
		// ~700 ms) arrive as their own part request beside the
		// page — every outlet whose fill was slow enough to
		// stream before now travels as a part, and DevTools
		// shows it. Reports stays on the swap slot: its slowness
		// is the screen's Load, not an outlet fill, and parts
		// never run a screen Load.
		Deferred: true,
		Loading: &uiapp.Loading{
			// The skeleton promises exactly what arrives:
			// the same card surface, the "Activity"
			// heading, and timeline-shaped rows (dot,
			// name line, two text lines) — not anonymous
			// grey bars.
			Show: uiapp.LoadingComponent(ui.Card(ui.CardConfig{Heading: "Activity", HeadingLevel: 2},
				ui.SkeletonTimeline(ui.SkeletonTimelineConfig{Rows: 3, Label: "Loading activity"}))),
			After: 150 * time.Millisecond,
			Min:   250 * time.Millisecond,
		},
		// Same handover as the toolbar: the aside's two cards
		// never cross at half opacity each.
		Transition: uiapp.FadeThrough(150 * time.Millisecond),
	})
	trackerShell = uiapp.NewLayout("shell", uiapp.LayoutSpec{
		Primary: uiapp.PrimaryConfig{Transition: uiapp.Transition{Name: "tracker-page"}},
		Outlets: []*uiapp.Outlet{trackerToolbar, trackerAside},
		Areas: []uiapp.AreaSpec{{
			Name: "crumbs",
			// The trail's loading content: during a slow navigation
			// (Reports takes ~1.2s) the OLD trail above the new page's
			// skeleton lied about where the user was heading. One
			// short skeleton line, the width of a trail, takes its
			// place past 150ms — a fast navigation paints nothing.
			Loading: &uiapp.Loading{
				Show:  uiapp.LoadingComponent(ui.SkeletonLine(ui.SkeletonLineConfig{Label: "Loading location"})),
				After: 150 * time.Millisecond,
			},
			// U2: the crumb trail gets its own sequential fade so the
			// outgoing and incoming trails never draw at once (the
			// root crossfade ghosted "Billing / BIL-31" over
			// "Projects / Billing" for a beat on every navigation).
			// The ROOT stays a plain crossfade: unchanged pixels behind
			// it would blink if the root went sequential.
			Transition: uiapp.FadeThrough(160 * time.Millisecond),
		}},
	}, buildShell)
	site.SetDefaultLayout(trackerShell)

	site.RegisterScreen(uiapp.NewScreen("/projects", &ProjectsIndexScreen{}).WithTitle("Projects"), nil)
	site.RegisterScreen(uiapp.NewScreen("/", &HomeScreen{}).WithTitle("Overview").
		Fill(trackerAside, &RecentActivityPanel{}), nil)
	site.RegisterScreen(uiapp.NewScreen("/inbox", &InboxScreen{}).WithTitle("Inbox").
		Fill(trackerToolbar, &InboxToolbar{}), nil)
	site.RegisterScreen(uiapp.NewScreen("/reports", &ReportsScreen{}).WithTitle("Reports").
		// The slow reports page declares its own swap-slot skeleton:
		// a chart-shaped block, then table rows.
		WithLoading(&uiapp.Loading{
			Show: uiapp.LoadingComponent(ui.Stack(ui.StackConfig{Gap: ui.GapMD},
				ui.SkeletonCard(ui.SkeletonCardConfig{Label: "Loading reports", BodyLines: 4}),
				ui.SkeletonRow(ui.SkeletonRowConfig{HideChevron: true}),
				ui.SkeletonRow(ui.SkeletonRowConfig{HideChevron: true}),
				ui.SkeletonRow(ui.SkeletonRowConfig{HideChevron: true}),
			)),
			After: 150 * time.Millisecond,
			Min:   300 * time.Millisecond,
		}), nil)
	site.RegisterScreen(uiapp.NewScreen("/settings", &SettingsScreen{}).WithTitle("Settings"), nil)

	// The project layer: ONE group at /projects/{project}. The layer
	// key embeds the group's RESOLVED prefix, so navigating between
	// projects re-renders the layer (new header, new list) while
	// navigating between one project's issues keeps it — the product
	// rule, spelled the way the router owns it.
	trackerProject = newProjectLayout()
	group := uiapp.NewScreenGroup("/projects/{project}", trackerProject)
	group.Resolve(resolvedFilter.From(func(ctx context.Context) (string, error) {
		return uiapp.QueryFromContext(ctx).Get("filter"), nil
	}))
	// The project resolver: everything on a project page reads it; an
	// unknown slug is the not-found page (Requires runs it in the
	// policy phase, before any Load).
	group.Resolve(resolvedProject.From(func(ctx context.Context) (Project, error) {
		m, _ := uiapp.MatchFromContext(ctx)
		p, ok := projectBySlug(m.Param("project"))
		if !ok {
			return Project{}, fmt.Errorf("%s: %w", m.Param("project"), uiapp.ErrNotFound)
		}
		return p, nil
	}))
	// The issue resolver: declared on the GROUP so every member screen
	// can see the key (a Requires key with no declaration panics at
	// mount — a silently dropped policy check otherwise). On the
	// index screens the eager run is inert: no :n param resolves to a
	// nil issue. An unavailable Legacy issue STORE fails the issue
	// pages in the policy phase — LEG-40's "store unavailable" case.
	group.Resolve(resolvedIssue.From(issueResolver))
	group.Requires(resolvedProject)
	group.Requires(resolvedIssue)
	// The group fill: every project page carries the toolbar's
	// primary action unless the screen fills it itself.
	group.Fill(trackerToolbar, &ProjectToolbar{})
	group.Screen(uiapp.NewScreen("/projects/{project}", &ProjectIndexScreen{}).
		Fill(trackerAside, &ProjectOverviewPanel{}), nil)
	group.Screen(uiapp.NewScreen("/projects/{project}/issues/{n}", &IssueScreen{}).
		Fill(trackerToolbar, &IssueToolbar{}).
		// The archived guard: a Legacy issue's activity aside renders
		// the read-only notice instead of the feed — a REGION decision,
		// the issue itself renders on.
		Fill(trackerAside, &ActivityPanel{},
			uiapp.FillPolicy(uiapp.PolicyFunc(archivedAsideGuard))), nil)
	site.Router.ScreenGroup(group)

	// The exporter is down: the export route is a real screen whose
	// policy blocks with 503, so a soft navigation to it answers
	// text/plain and the runtime's toast names the status. It rides
	// the shell like every other route: a page that answers blocked
	// kills its deferred parts before any 409 part reset can reload
	// (commitParts/finish in the parts module).
	site.RegisterScreen(uiapp.NewScreen("/reports/export", &ExportScreen{}).
		WithTitle("Export").
		WithPolicy(uiapp.PolicyFunc(func(ctx context.Context) app.Decision {
			return decide.Block(http.StatusServiceUnavailable,
				"The CSV exporter is down for maintenance.")
		})), nil)

	return site
}

// archivedAsideGuard is the Legacy project's region guard on an issue
// page's activity fill: the project is archived, its activity is read
// only in the old system — render that notice in the aside while the
// issue itself renders.
func archivedAsideGuard(ctx context.Context) app.Decision {
	p, err := resolvedProject.Get(ctx)
	if err == nil && p.Slug == "legacy" {
		return decide.RenderAlt(func() component.Component { return &ArchivedActivity{} })
	}
	return decide.Allow()
}

// buildShell renders the kept shell: top bar, sidebar, toolbar row,
// main slot, aside. Everything static here survives every in-app
// navigation; the outlets, the area, and the primary slot are the
// runtime's swap targets. The frame is the pieces composed: a
// page-tall stack (footer-less; viewport pages keep their footer
// inside main), the app bar (its own package, --size-header-height
// tall — the fixed height the row's Viewport mode subtracts), and the
// content row.
func buildShell(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
	nav, _ := component.SafeRenderCtx(ctx, ui.Sidebar(sidebarNavConfig()))
	return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		// The app bar is the tracker's own package (appbar): brand,
		// global search, the bell and the theme toggle, and the
		// sidebar's phone navigation hosted at the right edge. Its
		// height is the --size-header-height token the row's Viewport
		// mode subtracts.
		appbar.Render(appbar.Config{
			Name:   "Acme Tracker",
			Href:   "/",
			Search: ui.SearchInput(ui.SearchInputConfig{Name: "q", ID: "tracker-search", Placeholder: "Search issues and projects"}),
			Actions: ui.Cluster(ui.ClusterConfig{NoWrap: true},
				bellTrigger, ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon})),
			// The sidebar's phone navigation is hosted here, so the
			// nav column renders empty on phones and must not draw a
			// separator (PhoneNavFlush below).
			MobileTrigger: ui.SidebarDrawerTrigger(sidebarNavConfig()),
		}),
		ui.ContentRow(ui.ContentRowConfig{
			Viewport: true,
			// The sidebar's phone navigation lives outside the column,
			// so the stacked band renders empty and draws no line.
			PhoneNavFlush: true,
			Sidebar:       nav,
			Toolbar: ui.Cluster(ui.ClusterConfig{Justify: ui.JustifyBetween},
				l.RouteArea("crumbs", crumbsArea), l.Place(trackerToolbar)),
			Aside: l.Place(trackerAside), AsideLabel: "Context",
		}, l.Primary()),
	)
}

// crumbsArea is the breadcrumb route area: re-rendered by the server
// on every navigation the shell survives, from the live match.
func crumbsArea(_ context.Context, m uiapp.Match) render.HTML {
	if m.Path() == "/" {
		return ui.Breadcrumbs(ui.BreadcrumbsConfig{Label: "Breadcrumb", CompactMobile: true}, ui.Crumb{Text: "Home", Current: true})
	}
	crumbs := []ui.Crumb{{Text: "Home", Href: "/"}}
	segs := strings.Split(strings.Trim(m.Path(), "/"), "/")
	switch {
	case len(segs) >= 2 && segs[0] == "projects":
		p, ok := projectBySlug(segs[1])
		name := titleCase(segs[1])
		if ok {
			name = p.Name
		}
		crumbs = append(crumbs, ui.Crumb{Text: "Projects", Href: "/projects"}, ui.Crumb{Text: name, Href: "/projects/" + segs[1]})
		if len(segs) >= 4 && segs[2] == "issues" {
			// The trail deepens only for an issue that exists; an unknown
			// id (LEG-12, any number not in the project) ends at the
			// project — a crumb for a page that is not there is a lie.
			if n, err := strconv.Atoi(segs[3]); err == nil {
				if iss, ok := issueByNumber(segs[1], n); ok {
					crumbs = append(crumbs, ui.Crumb{Text: iss.Key(), Current: true})
				} else {
					crumbs[len(crumbs)-1].Current = true
				}
			} else {
				crumbs[len(crumbs)-1].Current = true
			}
		} else {
			crumbs[len(crumbs)-1].Current = true
		}
	default:
		crumbs = append(crumbs, ui.Crumb{Text: titleCase(segs[len(segs)-1]), Current: true})
	}
	return ui.Breadcrumbs(ui.BreadcrumbsConfig{Label: "Breadcrumb", CompactMobile: true}, crumbs...)
}

// titleCase upper-cases the first rune of a path segment.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
