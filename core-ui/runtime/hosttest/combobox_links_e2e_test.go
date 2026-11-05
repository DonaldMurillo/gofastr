package hosttest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	_ "github.com/DonaldMurillo/gofastr/battery/sitesearch"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func comboboxLinkSite(t *testing.T, rows, surface string, negative render.HTML) *httptest.Server {
	t.Helper()
	options := []headless.ComboboxOption{
		{Label: "Birch", Href: "/birch?view=tree#leaves"},
		{Label: "Cedar", Href: "/cedar?view=tree#needles"},
	}
	props := headless.ComboboxProps{ID: "links", Name: "q", Label: "Search", Options: options}
	switch rows {
	case "island":
		props.Options = nil
		props.NoScriptAction = "/"
		props.Island = &headless.Island{Endpoint: "/options", Signal: "link-options"}
	case "search":
		props.Options = []headless.ComboboxOption{{Label: "Help", Href: "/cedar"}}
		props.Parts.Attrs = headless.PartAttrs{headless.PartComboboxListbox: {"data-search": ""}}
	}
	box := headless.Combobox(props, nil)
	content := render.Join(negative, box, negative)
	page := content
	switch surface {
	case "widget":
		page = render.Tag("section", map[string]string{"id": "link-surface", "data-cui-widget": "link-trap"}, content)
	case "intercept":
		page = render.Tag("a", map[string]string{"id": "open-surface", "href": "/inspect"}, render.Text("Inspect"))
	case "disclosure":
		page = headless.Disclosure(headless.DisclosureProps{
			ID: "link-surface", Summary: render.Text("Search"), Open: true, Trap: true, Content: content,
			Parts: headless.Parts{Attrs: headless.PartAttrs{headless.PartSummary: {"tabindex": "-1"}}},
		}, nil)
	case "panehost":
		page = headless.PaneHost(headless.PaneHostProps{
			ID: "link-surface", Primary: render.Text("Primary"), Secondary: content, SecondaryOpen: true,
		}, nil)
	}
	site := app.NewApp("Combobox links")
	site.RegisterScreen(app.NewScreen("/", app.NewStaticComponent(page)), nil)
	site.RegisterScreen(app.NewScreen("/birch", app.NewStaticComponent(`<h1 id="leaves">Birch leaves</h1>`)), nil)
	site.RegisterScreen(app.NewScreen("/cedar", app.NewStaticComponent(`<h1 id="needles">Cedar needles</h1>`)), nil)
	site.Register("/inspect", app.NewStaticComponent(content), nil, app.InterceptFrom("/", app.ScreenDrawer))
	host := uihost.New(site)
	routes := router.New()
	host.Mount(routes)
	mux := http.NewServeMux()
	mux.HandleFunc("/options", func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, headless.ComboboxRows("links-listbox", options, nil))
	})
	mux.HandleFunc("/__gofastr/search/query", func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"url":"/birch?view=tree","title":"Birch","sections":[{"heading":"Leaves","id":"leaves"}]}]}`)
	})
	mux.HandleFunc("/trap.css", func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "text/css")
	})
	mux.Handle("/", routes)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func comboboxLinkPage(t *testing.T, server *httptest.Server, rows, surface string) context.Context {
	t.Helper()
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(server.URL)); err != nil {
		t.Fatal(err)
	}
	if surface == "intercept" {
		if err := chromedp.Run(ctx, chromedp.Poll(`!!window.__gofastr?.loadedModules?.intercept`, nil), chromedp.Click(`#open-surface`), chromedp.WaitVisible(`#links`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Poll(`!!window.__gofastr?.loadedModules?.['headless-combobox']`, nil)); err != nil {
		t.Fatal(err)
	}
	if surface == "widget" {
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`__gofastr.loadModule('widgets').then(() => __gofastr.mountWidget({name:'link-trap', backdrop:true, stylePath:'/trap.css'}, null, document.getElementById('link-surface')))`, nil),
			chromedp.Poll(`!!__gofastr.loadedModules.widgetfocus`, nil),
		); err != nil {
			t.Fatal(err)
		}
	}
	if surface == "panehost" {
		if err := chromedp.Run(ctx, chromedp.Poll(`!!__gofastr.loadedModules['headless-panehost']`, nil)); err != nil {
			t.Fatal(err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.trapInitialFocus = document.activeElement.id || document.activeElement.tagName`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#links`)); err != nil {
		t.Fatal(err)
	}
	if rows == "search" {
		if err := chromedp.Run(ctx, chromedp.Poll(`!!__gofastr.loadedModules.sitesearch`, nil)); err != nil {
			t.Fatal(err)
		}
	}
	if rows != "ssr" {
		if err := chromedp.Run(ctx, chromedp.SendKeys(`#links`, "bir")); err != nil {
			t.Fatal(err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Poll(`document.querySelector('#links-listbox a[role="option"]') && !document.getElementById('links-listbox').hidden`, nil)); err != nil {
		t.Fatal(err)
	}
	if rows == "search" {
		if err := chromedp.Run(ctx, chromedp.Poll(`!!document.querySelector('#links-listbox [data-search-result]')`, nil)); err != nil {
			t.Fatal(err)
		}
	}
	return ctx
}

func TestComboboxLinksStayOutOfTrap(t *testing.T) {
	for _, surface := range []string{"widget", "intercept", "disclosure", "panehost"} {
		for _, rows := range []string{"ssr", "island", "search"} {
			t.Run(surface+"/"+rows, func(t *testing.T) {
				server := comboboxLinkSite(t, rows, surface, "")
				ctx := comboboxLinkPage(t, server, rows, surface)
				for _, tabindex := range []string{"-1", "-7", " -12 "} {
					for _, key := range []string{kb.Shift + kb.Tab, kb.Tab} {
						var focused string
						if err := chromedp.Run(ctx,
							chromedp.Click(`#links`),
							chromedp.Evaluate(fmt.Sprintf(`document.querySelectorAll('#links-listbox a').forEach(option => option.setAttribute('tabindex', %q))`, tabindex), nil),
							chromedp.KeyEvent(key),
							chromedp.Evaluate(`document.activeElement.id || document.activeElement.tagName`, &focused),
						); err != nil {
							t.Fatal(err)
						}
						if focused != "links" {
							t.Fatalf("Tab with tabindex=%q focused %q, want links", tabindex, focused)
						}
					}
				}
			})
		}
	}
}

func TestTrapSkipsNegativeControls(t *testing.T) {
	negative := render.HTML(`<button tabindex="-2">Skip button</button><input tabindex=" -3 " aria-label="Skip input"><select tabindex="-4" aria-label="Skip select"><option>Skip</option></select><textarea tabindex="-5" aria-label="Skip textarea"></textarea><a href="/cedar" tabindex="-6">Skip link</a><span tabindex="-7">Skip span</span>`)
	for _, surface := range []string{"widget", "intercept", "disclosure", "panehost"} {
		t.Run(surface, func(t *testing.T) {
			server := comboboxLinkSite(t, "ssr", surface, negative)
			ctx := comboboxLinkPage(t, server, "ssr", surface)
			if surface != "disclosure" {
				var initial string
				if err := chromedp.Run(ctx, chromedp.Evaluate(`window.trapInitialFocus`, &initial)); err != nil {
					t.Fatal(err)
				}
				if initial != "links" {
					t.Errorf("initial focus=%q, want first tabbable links", initial)
				}
			}
			for _, key := range []string{kb.Shift + kb.Tab, kb.Tab} {
				var focused string
				if err := chromedp.Run(ctx, chromedp.KeyEvent(key), chromedp.Evaluate(`document.activeElement.id || document.activeElement.tagName`, &focused)); err != nil {
					t.Fatal(err)
				}
				if focused != "links" {
					t.Fatalf("negative-tabindex control became a trap boundary: focus=%q", focused)
				}
			}
		})
	}
}
