package ui_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

type workspaceItem struct{}

func (*workspaceItem) Render() render.HTML { return render.Text("Item detail") }

type workspaceContext struct{}

func (*workspaceContext) Render() render.HTML {
	return ui.Card(ui.CardConfig{Heading: "Context"}, ui.DetailList(ui.DetailListConfig{
		Items: []ui.DetailItem{{Label: "Team", Value: ui.AvatarGroup(ui.AvatarGroupConfig{
			Avatars: []ui.AvatarConfig{{Name: "Ada", Size: ui.AvatarSm}, {Name: "Grace", Size: ui.AvatarSm}, {Name: "Alan", Size: ui.AvatarSm}, {Name: "Barbara", Size: ui.AvatarSm}, {Name: "Edsger", Size: ui.AvatarSm}, {Name: "Frances", Size: ui.AvatarSm}},
			Max:     6, ShowNames: true,
		})}},
	}))
}

func TestWorkspaceKeptListAndEmptyAside(t *testing.T) {
	site := app.NewApp("Workspace")
	aside := app.NewOutlet("context")
	shell := app.NewLayout("workspace", app.LayoutSpec{Outlets: []*app.Outlet{aside}}, func(_ context.Context, l *app.LayoutTree) render.HTML {
		return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
			ui.ContentRow(ui.ContentRowConfig{Aside: l.Place(aside), AsideLabel: "Activity", Toolbar: ui.Toolbar(ui.ToolbarConfig{Label: "Actions", Groups: []ui.ToolbarGroup{{Children: []render.HTML{html.Link(html.LinkConfig{Href: "/items/empty", Text: "Empty"}), html.Link(html.LinkConfig{Href: "/items/full", Text: "Full"})}}}})}, l.Primary()))
	})
	site.SetDefaultLayout(shell)
	layer := app.NewLayout("items", app.LayoutSpec{}, func(_ context.Context, l *app.LayoutTree) render.HTML {
		rows := make([]render.HTML, 80)
		for i := range rows {
			rows[i] = html.Paragraph(html.TextConfig{}, render.Text(fmt.Sprintf("Item %d", i)))
		}
		return ui.ListDetail(ui.ListDetailConfig{List: render.Join(rows...), ListLabel: "Items", Detail: l.Primary()})
	})
	group := app.NewScreenGroup("/items", layer)
	group.Screen(app.NewScreen("/items/full", &workspaceItem{}).Fill(aside, &workspaceContext{}), nil)
	group.Screen(app.NewScreen("/items/empty", &workspaceItem{}), nil)
	site.Router.ScreenGroup(group)
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
	var before, after float64
	var contextFits bool
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 800), chromedp.Navigate(srv.URL+"/items/full"), chromedp.Poll(`!!window.__gofastr`, nil), chromedp.Evaluate(`document.querySelector('.fui-content-row__aside').getBoundingClientRect().width`, &before), chromedp.Evaluate(`(()=>{const aside=document.querySelector('.fui-content-row__aside'), group=aside.querySelector('[data-fui-comp="ui-avatar-group"]');return group.getBoundingClientRect().right<=aside.getBoundingClientRect().right})()`, &contextFits), chromedp.Evaluate(`window.keptList=document.querySelector('.fui-list-detail__list');keptList.scrollTop=200`, nil), chromedp.Click(`a[href="/items/empty"]`), chromedp.Poll(`location.pathname === '/items/empty' && document.querySelector('.fui-content-row__aside > [data-fui-outlet]').textContent === ''`, nil), chromedp.Evaluate(`document.querySelector('.fui-content-row__aside').getBoundingClientRect().width`, &after)); err != nil {
		t.Fatal(err)
	}
	if !contextFits {
		t.Fatal("context values are clipped inside the narrow desktop aside")
	}
	if before <= 0 || after != 0 {
		t.Fatalf("aside width %v -> %v, want visible -> zero", before, after)
	}
	var kept bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`keptList===document.querySelector('.fui-list-detail__list') && keptList.scrollTop===200`, &kept)); err != nil {
		t.Fatal(err)
	}
	if !kept {
		t.Fatal("list node or scroll lost across detail navigation")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`a[href="/items/full"]`), chromedp.Poll(`location.pathname === '/items/full' && !!document.querySelector('.fui-content-row__aside [data-fui-comp="ui-avatar-group"]')`, nil), chromedp.Evaluate(`document.querySelector('.fui-content-row__aside').getBoundingClientRect().width`, &after)); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("refilled aside width %v, want %v", after, before)
	}
	var stacks bool
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Evaluate(`(()=>{const list=document.querySelector('.fui-list-detail__list').getBoundingClientRect(), detail=document.querySelector('.fui-list-detail__detail').getBoundingClientRect(),aside=document.querySelector('.fui-content-row__aside').getBoundingClientRect();return list.top>=detail.bottom && aside.top>=list.bottom && document.documentElement.scrollWidth===390})()`, &stacks)); err != nil {
		t.Fatal(err)
	}
	if !stacks {
		t.Fatal("phone panes/aside do not stack or overflow")
	}
}
