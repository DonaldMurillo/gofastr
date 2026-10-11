package ui_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestCompactLayoutGeometry(t *testing.T) {
	site := app.NewApp("Layout checks")
	navcfg := ui.SidebarConfig{NavLabel: "Projects", NativeMobile: true, SuppressDrawerTrigger: true, Items: []ui.SidebarItem{{Label: "Project", Href: "/workspace"}}}
	nav, _ := component.SafeRenderCtx(context.Background(), ui.Sidebar(navcfg))
	rows := make([]render.HTML, 8)
	for i := range rows {
		rows[i] = ui.Card(ui.CardConfig{Variant: ui.CardRow, Href: "/workspace", Heading: "TASK-12", Description: "A record title that wraps to two short lines"}, ui.StatusBadge(ui.StatusBadgeConfig{Label: "Open"}))
	}
	detail := ui.Stack(ui.StackConfig{},
		ui.Responsive(ui.ResponsiveConfig{}, "",
			ui.LinkButton(ui.LinkButtonConfig{Label: "Back", Href: "/workspace"})),
		ui.PageHeader(ui.PageHeaderConfig{Compact: true, HeadingLevel: 2, Title: "A record title"}),
		ui.DetailList(ui.DetailListConfig{Inline: true, Items: []ui.DetailItem{
			{Label: "Assignee", Value: render.Text("Ada Lovelace")},
		}}),
		ui.Stack(ui.StackConfig{ID: "description", Gap: ui.GapMD, TrimMargins: true},
			html.Paragraph(html.TextConfig{}, render.Text("First paragraph")),
			html.Paragraph(html.TextConfig{}, render.Text("Second paragraph")),
		),
	)
	// The app bar is the app's own package in a real app; here a plain
	// banner stands in. It hosts the sidebar's phone trigger, so the
	// sidebar suppresses its own (one phone menu control).
	workspace := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		html.Header(html.HeaderConfig{Banner: true}, ui.Cluster(ui.ClusterConfig{Justify: ui.JustifyBetween},
			render.Text("Project"),
			ui.SidebarDrawerTrigger(navcfg),
		)),
		ui.ContentRow(ui.ContentRowConfig{
			Viewport:      true,
			PhoneNavFlush: true,
			Aside:         render.Text("Context"),
			Sidebar:       nav,
			Toolbar: ui.Cluster(ui.ClusterConfig{Justify: ui.JustifyBetween},
				ui.Breadcrumbs(ui.BreadcrumbsConfig{Label: "Trail"}, ui.Crumb{Text: "Project", Current: true}),
				ui.Toolbar(ui.ToolbarConfig{Plain: true, Label: "Actions", Groups: []ui.ToolbarGroup{
					{Children: []render.HTML{ui.Button(ui.ButtonConfig{Label: "New"})}},
				}}),
			),
		}, html.Main(html.MainConfig{},
			ui.PageHeader(ui.PageHeaderConfig{Compact: true, Title: "Project", Badge: ui.StatusBadge(ui.StatusBadgeConfig{Label: "On track"})}),
			ui.ListDetail(ui.ListDetailConfig{
				ListLabel: "Records",
				List: ui.Stack(ui.StackConfig{Gap: ui.GapNone},
					ui.FilterToolbar(ui.FilterToolbarConfig{
						Compact: true,
						Action:  "/workspace",
						Search:  &ui.FilterSearch{Name: "q", Placeholder: "Filter records"},
					}),
					render.Join(rows...),
				),
				Detail: detail,
			}),
		)))
	compactNav, _ := component.SafeRenderCtx(context.Background(), ui.Sidebar(ui.SidebarConfig{Compact: true, CurrentPath: "/docs", Items: []ui.SidebarItem{{Label: "Article", Href: "/docs"}}}))
	// A docs page's grid is the app's own package; the compact rail
	// and the compact page header are the kit's, so the row holds them.
	docs := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		html.Header(html.HeaderConfig{Banner: true}, render.Text("Docs")),
		ui.ContentRow(ui.ContentRowConfig{Sidebar: compactNav}, html.Main(html.MainConfig{},
			ui.PageHeader(ui.PageHeaderConfig{Title: "Article", Compact: true}),
			html.Paragraph(html.TextConfig{}, render.Text("Introduction")),
		)))
	releases := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		html.Header(html.HeaderConfig{Banner: true}, ui.Banner(ui.BannerConfig{
			Strip: true, Title: "Tracker 2.4 is out",
			Body:   "Pinned views and saved filters ship today.",
			Action: ui.Link(ui.LinkConfig{Text: "Read the release notes", Href: "#release-one"}),
		})),
		html.Main(html.MainConfig{}, ui.Stack(ui.StackConfig{Gap: ui.Gap2XL},
			ui.Section(ui.SectionConfig{Compact: true, Heading: "Release one"}, render.Text("Changes")),
			ui.Section(ui.SectionConfig{Compact: true, Heading: "Release two"}, render.Text("Changes")),
		)))
	// An app frame pads main itself, so a page header first in it adds
	// no inset of its own.
	frame := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		html.Header(html.HeaderConfig{Banner: true}, render.Text("App")),
		ui.ContentRow(ui.ContentRowConfig{Sidebar: nav}, html.Main(html.MainConfig{},
			ui.PageHeader(ui.PageHeaderConfig{Title: "Invoices", Subtitle: "3 invoices"}),
			html.Paragraph(html.TextConfig{}, render.Text("Rows")),
		)))
	for path, body := range map[string]render.HTML{"/workspace": workspace, "/docs": docs, "/releases": releases, "/frame": frame} {
		site.RegisterScreen(app.NewScreen(path, app.NewStaticComponent(body)), nil)
	}
	host := uihost.New(site)
	fw := framework.NewApp()
	fw.Use(host.RouteMatchMiddleware())
	fw.Mount(host)
	if err := fw.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(fw.Router())
	defer srv.Close()
	ctx := chromedptest.Context(t)
	check := func(t *testing.T, path string, width int64, js string) {
		t.Helper()
		var result string
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 800), chromedp.Navigate(srv.URL+path), chromedp.Poll(`!!window.__gofastr`, nil), chromedp.Evaluate(`(()=>{const q=s=>document.querySelector(s), r=e=>e.getBoundingClientRect();`+js+`;return ""})()`, &result)); err != nil {
			t.Fatal(err)
		}
		if result != "" {
			t.Fatal(result)
		}
	}
	t.Run("TitleBadge", func(t *testing.T) {
		for _, width := range []int64{1280, 390} {
			check(t, "/workspace", width, `
const title=q('main > .fui-page-header h1'), badge=q('main > .fui-page-header .fui-badge');
if(!title||!badge)return 'missing title or badge';
const a=r(title), b=r(badge);
if(!(b.left>=a.right && b.left-a.right<=16 && b.top<a.bottom))return 'badge not beside the title';
if(document.documentElement.scrollWidth>innerWidth)return 'horizontal overflow';
title.textContent='Customer billing settings and payments';
const c=r(title), d=r(badge);
if(innerWidth===390 && !(Math.abs(d.left-c.left)<=1 && d.top>=c.bottom))return 'badge did not wrap below the long title';
if(document.documentElement.scrollWidth>innerWidth)return 'long title overflows';
`)
		}
	})
	t.Run("Workspace", func(t *testing.T) {
		check(t, "/workspace", 1280, `
const rows=[...document.querySelectorAll('.fui-card--row')];
if(rows.filter(e=>r(e).bottom<=800).length<7)return 'dense rows exceed viewport';
const filter=q('[data-cui-comp="ui-filter-toolbar"]');
if(r(filter).height>48)return 'filter controls stack';
const term=q('dt'),value=q('dd');
if(Math.abs(r(term).y-r(value).y)>1)return 'detail fields stack';
const paragraphs=q('#description').children;
if(r(paragraphs[1]).y-r(paragraphs[0]).bottom>8)return 'paragraph margins compound the gap';
if(getComputedStyle(q('[aria-label="Actions"]')).borderTopWidth!=='0px')return 'toolbar frame';
if([...document.querySelectorAll('a')].some(e=>e.textContent==='Back'&&r(e).height))return 'desktop back link';
if(r(q('.fui-content-row__aside')).width!==288)return 'aside default drifted from 18rem';
`)
	})
	t.Run("Phone", func(t *testing.T) {
		check(t, "/workspace", 390, `if(document.documentElement.scrollWidth!==390)return 'phone overflow';if([...document.querySelectorAll('summary,button')].filter(e=>r(e).height&&r(e).y<120&&/navigation/i.test(e.getAttribute('aria-label')||'')).length!==1)return 'duplicate menu';if(r(q('[aria-label="Trail"]')).x<12)return 'phone gutter';if(getComputedStyle(q('.fui-content-row__nav')).borderBottomWidth!=='0px')return 'empty nav band draws a separator'`)
	})
	t.Run("PhoneDocument", func(t *testing.T) {
		check(t, "/docs", 390, `
const trigger=q('.fui-sidebar__hamburger');
if(r(trigger).width<300||r(trigger).height<44)return 'small article navigation trigger';
if(!getComputedStyle(trigger,'::after').content.includes(trigger.getAttribute('aria-label')))return 'article navigation label hidden';
`)
	})
	t.Run("PhoneAnnouncement", func(t *testing.T) {
		check(t, "/releases", 390, `if(r(q('[data-cui-comp="ui-banner"]')).height>56)return 'announcement adds empty rows'`)
	})
	t.Run("FrameHeaderInset", func(t *testing.T) {
		check(t, "/frame", 1280, `
const main=q('.fui-content-row main');
const pad=parseFloat(getComputedStyle(main).paddingTop), inset=r(main.querySelector('h1')).top-r(main).top;
if(pad<24)return 'the frame no longer pads main: '+pad;
if(Math.abs(inset-pad)>1)return 'page header adds its own inset inside the padded main: '+inset+' for a '+pad+' pad';
`)
	})
	t.Run("Document", func(t *testing.T) {
		check(t, "/docs", 1280, `
if(getComputedStyle(q('h1').closest('.fui-page-header')).borderBottomWidth!=='0px')return 'title divider';
const link=q('.fui-sidebar__link');
if(r(link).height>32)return 'loose rail';
if(getComputedStyle(link).backgroundColor!=='rgba(0, 0, 0, 0)')return 'filled rail marker';
`)
	})
}
