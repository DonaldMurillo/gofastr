package runtime_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

type addressPage struct{ project string }

func (page *addressPage) SetParams(params map[string]string) { page.project = params["project"] }
func (page *addressPage) Render() render.HTML                { return render.Text(page.project) }

type addressAside struct{}

func (*addressAside) Render() render.HTML { return render.Text("READY-ASIDE") }

func TestDeferredParamUsesServerAddress(t *testing.T) {
	application := app.NewApp("Addresses")
	shell := app.NewLayout("shell", app.LayoutSpec{}, func(_ context.Context, layout *app.LayoutTree) render.HTML {
		return layout.Primary()
	})
	aside := app.NewOutlet("aside", app.OutletOptions{
		Deferred: true,
		Loading:  &app.Loading{Show: app.LoadingComponent(render.Text("LOADING-ASIDE"))},
	})
	project := app.NewLayout("project", app.LayoutSpec{Outlets: []*app.Outlet{aside}}, func(_ context.Context, layout *app.LayoutTree) render.HTML {
		return render.Join(layout.Primary(), layout.Place(aside))
	})
	application.SetDefaultLayout(shell)
	group := app.NewScreenGroup("/projects/{project}", project)
	group.Screen(app.NewScreen("/projects/{project}", &addressPage{}).Fill(aside, &addressAside{}), nil)
	group.Screen(app.NewScreen("/projects/{project}/detail", &addressPage{}).Fill(aside, &addressAside{}), nil)
	application.Router.ScreenGroup(group)
	host := uihost.New(application)
	type partResult struct {
		address string
		markup  string
		status  int
		body    string
	}
	parts := make(chan partResult, 32)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		address := request.Header.Get("X-Gofastr-Part")
		if address == "" {
			host.ServeHTTP(writer, request)
			return
		}
		response := httptest.NewRecorder()
		host.ServeHTTP(response, request)
		for name, values := range response.Header() {
			writer.Header()[name] = values
		}
		writer.WriteHeader(response.Code)
		_, _ = writer.Write(response.Body.Bytes())
		parts <- partResult{address, request.Header.Get("X-Gofastr-Markup"), response.Code, response.Body.String()}
	}))
	t.Cleanup(server.Close)
	for _, example := range []struct{ path, value string }{
		{"/projects/a%2Fb", "a/b"},
		{"/projects/%25", "%"},
		{"/projects/a%252Fb", "a%2Fb"},
		{"/projects/%2525", "%25"},
	} {
		t.Run(example.path, func(t *testing.T) {
			ctx := chromedptest.Context(t)
			path := example.path + "/detail"
			if err := chromedp.Run(ctx, chromedp.Navigate(server.URL+example.path),
				chromedp.Evaluate(`window.__gofastr.loadModule('envelope')`, nil),
				chromedp.Poll(`!!window.__gofastr?._navHooks?.envelope`, nil, chromedp.WithPollingTimeout(5*time.Second)),
				chromedp.Evaluate(`window.stamp=1;window.naved='';window.filled=[];window.addEventListener('gofastr:navigate',()=>window.naved=location.pathname);window.addEventListener('gofastr:fill',event=>window.filled.push(event.detail.addr))`, nil),
				chromedp.Evaluate(`window.__gofastr.navigate(`+strconv.Quote(path)+`)`, nil)); err != nil {
				t.Fatal(err)
			}
			want := "g:/projects/" + example.value + "/:project"
			select {
			case result := <-parts:
				if result.markup != runtime.MarkupVersion {
					t.Errorf("part markup = %q, want %q", result.markup, runtime.MarkupVersion)
				}
				if result.address != want+"#aside" || result.status != http.StatusOK {
					t.Fatalf("part address=%q status=%d body=%s, want %q status=200", result.address, result.status, result.body, want+"#aside")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no deferred part request")
			}
			var address string
			if err := chromedp.Run(ctx,
				chromedp.Poll(fmt.Sprintf(`window.naved===%q && window.stamp===1 && window.filled.includes(%q) && [...document.querySelectorAll('[data-cui-outlet]')].some(outlet=>outlet.getAttribute('data-cui-outlet')===%q && outlet.textContent==='READY-ASIDE')`, path, want+"#aside", want+"#aside"), nil, chromedp.WithPollingTimeout(5*time.Second)),
				chromedp.Evaluate(`document.querySelector('[data-cui-layout-key^="g:"]').getAttribute('data-cui-layout-key')`, &address)); err != nil {
				var state string
				_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({nav:window.naved,stamp:window.stamp,path:location.pathname,outlets:[...document.querySelectorAll('[data-cui-outlet]')].map(outlet=>[outlet.getAttribute('data-cui-outlet'),outlet.textContent])})`, &state))
				t.Fatalf("%v: %s", err, state)
			}
			if address != want {
				t.Fatalf("live layout key=%q, want %q", address, want)
			}
		})
	}
}
