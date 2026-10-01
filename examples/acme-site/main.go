// Package main is Acme Tracker's public face: the product's marketing
// site and help center, built from the layout primitive exactly the way
// docs/DESIGN-layout-outlets.md's "Ready-made layouts" section draws
// the site and docs shapes — real pages, real copy, no lab fixtures.
//
// The site shape: a root layout whose static cells are the <header> and
// <footer> landmarks around the primary outlet. The top bar and the
// footer are the site's own packages (siteheader, sitefooter: their Go,
// their owned style sheets, every dimension a theme token), not
// framework components. The marketing pages declare none of the opt-in machinery —
// no deferred outlets, no loading templates, no transition vocabulary,
// no route areas — so they ship the core runtime only.
//
// The docs shape: a group layout under /help (nav listing the articles,
// crumbs, toc and pager outlets, primary) drawn by the site's own
// helpdocs package. Articles with headings fill the toc outlet; the one
// short article leaves it empty and the TOC column collapses through
// :has(> :empty).
//
// Run with:
//
//	go run ./examples/acme-site     # :8097, or $PORT
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/examples/acme-site/helpdocs"
	"github.com/DonaldMurillo/gofastr/examples/acme-site/sitefooter"
	"github.com/DonaldMurillo/gofastr/examples/acme-site/siteheader"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// defaultAddr is the fallback when $PORT is unset; `gofastr dev` and
// PaaS runtimes inject PORT and isolation.ListenAddr honours it.
const defaultAddr = ":8097"

// The layouts and the site's own outlets, package vars so buildSite's
// construction and the screens share one value each.
var (
	// acmeSite is the root layout every page but the changelog uses.
	acmeSite *uiapp.Layout
	// acmeAnnounce is the site shell's announcement outlet (filled only
	// by the changelog).
	acmeAnnounce *uiapp.Outlet
	// helpDocs is the /help group's layout: nav, crumbs, toc, pager.
	helpDocs *uiapp.Layout
	// The docs layout's outlet handles, package vars so the screens'
	// fills and the build's placements name the same typed values.
	docsCrumbs *uiapp.Outlet
	docsToc    *uiapp.Outlet
	docsPager  *uiapp.Outlet
)

// resolvedArticle is the help article the /help/{slug} route names:
// the article screen declares it, and its fills read it.
var resolvedArticle = uiapp.NewKey[HelpArticle]("article")

func main() {
	fwApp := buildApp()
	listenAddr, err := isolation.ListenAddr(".", defaultAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("acme-site listening on http://localhost%s", listenAddr)
	if err := fwApp.Start(listenAddr); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// buildApp wires the site into a framework app without binding a port,
// so tests drive it in-process.
func buildApp() *framework.App {
	site := buildSite()
	host := uihost.New(site)
	fwApp := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "acme-site"}),
	)
	ui.MountSidebar(routerMounter{fwApp.Router()}, helpNavConfig(""))
	fwApp.Use(host.RouteMatchMiddleware())
	fwApp.Mount(host)
	return fwApp
}

type routerMounter struct{ r *router.Router }

func (m routerMounter) MountWidget(def *widget.Definition) {
	widget.Mount(m.r, def)
}

// buildSite declares the screens, the site shell(s), and the help
// center's group layer.
func buildSite() *uiapp.App {
	site := uiapp.NewApp("Acme Tracker")
	// The same product as the tracker example, so the same accent: a
	// deep teal that keeps 4.5:1 on the light surface, with its dark
	// twin. The header and help packages bring their own tokens
	// (siteheader.tokens.css, helpdocs.tokens.css); Extend adds them to
	// the theme.
	site.WithTheme(theme.Default(theme.Overrides{
		Primary: "#0F766E",
		Dark:    &theme.Overrides{Primary: "#5EEAD4"},
	}).Extend(siteheader.Tokens, helpdocs.Tokens))

	// The site shell: the announcement outlet above the header's
	// landmark, then static header and footer around the primary slot.
	// The outlet is the only layout machinery the marketing pages
	// declare; everything else is a plain partial swap of the primary.

	// The changelog's announcement rides a REAL outlet on the shared
	// site shell (FallbackNothing: the bar is simply absent everywhere
	// else), so a navigation between the marketing pages swaps only the
	// primary and the envelope re-applies the announcement fill — one
	// chain, one shell, no second layout. The outlet marker makes every
	// marketing page negotiate the fills envelope on navigation, and
	// nothing else: the module loads beside the first navigation's
	// fetch, never at boot.
	acmeAnnounce = uiapp.NewOutlet("announcement")
	acmeSite = uiapp.NewLayout("site", uiapp.LayoutSpec{
		Outlets: []*uiapp.Outlet{acmeAnnounce},
	}, buildSiteShell)
	site.SetDefaultLayout(acmeSite)

	site.RegisterScreen(uiapp.NewScreen("/", &HomeScreen{}).
		WithTitle("Acme Tracker — issue tracking that stays out of the way"), nil)
	site.RegisterScreen(uiapp.NewScreen("/pricing", &PricingScreen{}).
		WithTitle("Pricing — Acme Tracker"), nil)
	site.RegisterScreen(uiapp.NewScreen("/changelog", &ChangelogScreen{}).
		WithTitle("Changelog — Acme Tracker").
		Fill(acmeAnnounce, &AnnounceBar{}), nil)

	// The help center: ONE group at /help on a docs layout. The layer's
	// outlets are the per-article regions: crumbs and the pager beside
	// every article, the TOC rail only where the article has headings
	// (a fill whose Load declines leaves the outlet empty, and
	// helpdocs collapses the column through :has(> :empty)). The article
	// nav is static chrome — the server marks the current link, the
	// runtime's activelink pass keeps it fresh — so it keeps its scroll
	// and needs no machinery of its own.
	docsCrumbs = uiapp.NewOutlet("crumbs")
	docsToc = uiapp.NewOutlet("toc")
	docsPager = uiapp.NewOutlet("pager")
	helpDocs = uiapp.NewLayout("docs", uiapp.LayoutSpec{
		Outlets: []*uiapp.Outlet{docsCrumbs, docsToc, docsPager},
	}, buildHelpDocs)
	group := uiapp.NewScreenGroup("/help", helpDocs)
	// The article resolver lives on the ARTICLE screen, not the group:
	// the index at /help has no slug, and an unknown slug is still the
	// not-found page — Requires runs it in the policy phase, before
	// any Load.
	group.Screen(uiapp.NewScreen("/help", &HelpIndexScreen{}).
		WithTitle("Help center — Acme Tracker").
		Fill(docsCrumbs, &IndexCrumbs{}), nil)
	group.Screen(uiapp.NewScreen("/help/{slug}", &HelpArticleScreen{}).
		Resolve(resolvedArticle.From(func(ctx context.Context) (HelpArticle, error) {
			m, _ := uiapp.MatchFromContext(ctx)
			a, ok := articleBySlug(m.Param("slug"))
			if !ok {
				return HelpArticle{}, fmt.Errorf("%s: %w", m.Param("slug"), uiapp.ErrNotFound)
			}
			return a, nil
		})).
		Fill(docsCrumbs, &ArticleCrumbs{}).
		Fill(docsToc, &ArticleToc{}).
		Fill(docsPager, &ArticlePager{}), nil)
	site.Router.ScreenGroup(group)

	return site
}

// buildSiteShell renders the site's chrome: the announcement outlet
// inside the header's banner landmark (above the header's own row —
// page content outside every landmark is invisible to a screen
// reader's region navigation, axe region), then the static header and
// footer around the primary slot. Everything here survives every
// in-app navigation between pages that share this layout. The frame is
// a page-tall stack holding the site's own header and footer packages.
func buildSiteShell(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
	// The header is the banner landmark and a direct child of the
	// page-tall stack, so it stays pinned for the whole page; the
	// announcement strip scrolls away above it.
	return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		l.Place(acmeAnnounce),
		siteHeader(ctx),
		l.Primary(),
		siteFooter(),
	)
}

// AnnounceBar fills the site's announcement outlet — the changelog's
// "v2.4 is out" strip: one sentence, one link to the release's own
// section on the page below.
type AnnounceBar struct{}

func (b *AnnounceBar) Render() render.HTML {
	return ui.Banner(ui.BannerConfig{
		Strip:      true,
		Title:      "Acme Tracker 2.4 is out",
		Body:       "Pinned views and saved filters ship today.",
		Action:     ui.Link(ui.LinkConfig{Href: "#v2-4", Text: "Read the release notes"}),
		ExtraAttrs: html.Attrs{"data-acme-announcement": ""},
	})
}

// siteHeader is the site's one top bar: the logo and name, the four
// primary destinations, the theme toggle and the call to action. On
// phones the links and the call to action move into a full-width
// native <details> menu, so the bar works without JavaScript.
func siteHeader(ctx context.Context) render.HTML {
	return siteheader.Render(siteheader.Config{
		Ctx:  ctx,
		Name: "Acme Tracker",
		Links: []siteheader.Link{
			{Label: "Overview", Href: "/"},
			{Label: "Pricing", Href: "/pricing"},
			{Label: "Changelog", Href: "/changelog"},
			// Every article under /help keeps the tab lit.
			{Label: "Help", Href: "/help", Section: true},
		},
		CTA:     siteheader.Link{Label: "Get started", Href: "/pricing"},
		Actions: ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon}),
	})
}

// siteFooter is the site's colophon: the product's own pages and the
// help center's articles, over a quiet bottom line. Like the header it
// is the site's own package (sitefooter), not a framework component.
func siteFooter() render.HTML {
	helpLinks := make([]sitefooter.Link, 0, len(helpArticles))
	for _, a := range helpArticles {
		helpLinks = append(helpLinks, sitefooter.Link{Label: a.Title, Href: "/help/" + a.Slug})
	}
	return sitefooter.Render(sitefooter.Config{
		Name:    "Acme Tracker",
		Tagline: "Issue tracking that stays out of your way.",
		Columns: []sitefooter.Column{
			{
				Title: "Product",
				Links: []sitefooter.Link{
					{Label: "Overview", Href: "/"},
					{Label: "Pricing", Href: "/pricing"},
					{Label: "Changelog", Href: "/changelog"},
				},
			},
			{Title: "Help", Links: helpLinks},
		},
		Note: "© 2026 Acme — the public site and help center for Acme Tracker.",
	})
}

// buildHelpDocs renders the docs layer through the site's own helpdocs
// package: the article nav (a ROUTE AREA — the layer is kept while you
// move between articles, so the current-article mark must be re-derived
// server-side on every navigation, not read off the first match and
// left stale), the crumbs outlet above the article, the primary
// article, the pager outlet under it, and the toc outlet as the right
// rail, which collapses when an article fills nothing there.
func buildHelpDocs(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
	return helpdocs.Render(helpdocs.Config{
		Nav: l.RouteArea("helpnav", func(ctx context.Context, m uiapp.Match) render.HTML {
			return helpNav(ctx, m.Path())
		}),
		Crumbs: l.Place(docsCrumbs),
		Body:   l.Primary(),
		Pager:  l.Place(docsPager),
		Toc:    l.Place(docsToc),
	})
}

// helpNav uses Sidebar's grouped navigation, with a drawer on phones
// and a native disclosure when scripting is disabled. The route area's
// request ctx threads through, so a recovered render panic here reports
// to the request's observer (test harness failure, 500 in test
// binaries) instead of disappearing into a background context.
func helpNav(ctx context.Context, path string) render.HTML {
	nav, _ := component.SafeRenderCtx(ctx, ui.Sidebar(helpNavConfig(path)))
	return nav
}

func helpNavConfig(path string) ui.SidebarConfig {
	items := make([]ui.SidebarItem, 0, len(helpArticles))
	for _, a := range helpArticles {
		items = append(items, ui.SidebarItem{Label: a.Title, Href: "/help/" + a.Slug})
	}
	return ui.SidebarConfig{
		NavLabel: "Browse the help", CurrentPath: path, NativeMobile: true, Compact: true,
		DrawerName: "help-nav", DrawerTitle: "Browse the help",
		Items: []ui.SidebarItem{
			{Label: "Help center", Href: "/help"},
			{Label: "Articles", Open: true, Children: items},
		},
	}
}
