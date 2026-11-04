package uihost

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestCustomToastStylesAcrossHosts(t *testing.T) {
	widget.IsolateForTest(t)
	var routers []*router.Router
	for _, name := range []string{"custom", "other"} {
		application := app.NewApp(name)
		application.Register("/", &describedScreen{}, nil)
		host := New(application, WithNoInlineFirstPaint())
		routes := router.New()
		host.Mount(routes)
		if name == "custom" {
			widget.MountBuilder(routes.Group("/shared"), preset.ToastStack("mine"))
		}
		host.ValidateBoot()
		host.ValidateBoot()
		routers = append(routers, routes)
	}
	if _, exists := widget.Lookup(DefaultToastStack); exists {
		t.Error("second host registered a default stack beside the custom stack")
	}
	linkPattern := regexp.MustCompile(`<link\b[^>]*>`)
	hrefPattern := regexp.MustCompile(`href="([^"]+)"`)
	for index, routes := range routers {
		t.Run([]string{"custom", "other"}[index], func(t *testing.T) {
			page := httptest.NewRecorder()
			routes.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
			markup := page.Body.String()
			if page.Code != http.StatusOK || strings.Count(markup, `data-cui-toast-stack="`) != 1 || !strings.Contains(markup, `data-cui-toast-stack="mine"`) {
				t.Errorf("expected exactly one custom toast stack: status=%d, count=%d", page.Code, strings.Count(markup, `data-cui-toast-stack="`))
			}
			styles := 0
			for _, link := range linkPattern.FindAllString(markup, -1) {
				if !strings.Contains(link, `rel="stylesheet"`) {
					continue
				}
				match := hrefPattern.FindStringSubmatch(link)
				if len(match) != 2 {
					t.Fatalf("stylesheet without href: %s", link)
				}
				if strings.HasPrefix(match[1], "https://") {
					continue
				}
				response := httptest.NewRecorder()
				routes.ServeHTTP(response, httptest.NewRequest(http.MethodGet, match[1], nil))
				if response.Code != http.StatusOK {
					t.Errorf("stylesheet %s: %d", match[1], response.Code)
				}
				styles++
			}
			if styles == 0 {
				t.Fatal("page linked no stylesheets")
			}
			definition, _ := widget.Lookup("mine")
			response := httptest.NewRecorder()
			routes.ServeHTTP(response, httptest.NewRequest(http.MethodGet, definition.StyleURL(), nil))
			if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/css") || response.Body.Len() == 0 {
				t.Errorf("custom stack style: %d, %q", response.Code, response.Header().Get("Content-Type"))
			}
		})
	}
}

func TestToastStylesAcrossHosts(t *testing.T) {
	widget.IsolateForTest(t)
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			application := app.NewApp(name)
			application.Register("/", &describedScreen{}, nil)
			host := New(application, WithNoInlineFirstPaint())
			routes := router.New()
			host.Mount(routes)
			host.ValidateBoot()
			host.ValidateBoot()
			server := httptest.NewServer(routes)
			defer server.Close()
			definition, ok := widget.Lookup(DefaultToastStack)
			if !ok {
				t.Fatal("missing default toast registration")
			}
			for _, path := range []string{"/", definition.StyleURL()} {
				response, err := server.Client().Get(server.URL + path)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
				response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != 200 {
					t.Fatalf("GET %s: %d", path, response.StatusCode)
				}
				if path != "/" && (!strings.Contains(response.Header.Get("Content-Type"), "text/css") || len(body) == 0) {
					t.Fatal("toast stylesheet is not nonempty CSS")
				}
			}
		})
	}
}

func bootedHost(t *testing.T, beforeBoot func(r *router.Router)) {
	t.Helper()
	widget.IsolateForTest(t)
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a)
	r := router.New()
	ds.Mount(r)
	if beforeBoot != nil {
		beforeBoot(r)
	}
	ds.ValidateBoot()
}

func TestBootMountsTheDefaultToastStack(t *testing.T) {
	bootedHost(t, nil)
	d, ok := widget.Lookup(DefaultToastStack)
	if !ok {
		t.Fatal("ValidateBoot mounted no toast stack; a header toast would have no region to land in")
	}
	if !preset.IsToastStack(d) {
		t.Fatalf("the default stack is not a preset toast stack: %+v", d)
	}
}

func TestBootMountsToastBesideOtherWidgets(t *testing.T) {
	bootedHost(t, func(routes *router.Router) {
		widget.MountBuilder(routes, widget.New("not-a-toast"))
	})
	if _, ok := widget.Lookup(DefaultToastStack); !ok {
		t.Fatal("an unrelated widget suppressed the default toast stack")
	}
}

func TestBootKeepsTheAppsOwnToastStack(t *testing.T) {
	bootedHost(t, func(r *router.Router) {
		widget.MountBuilder(r, preset.ToastStack("mine"))
	})
	if _, ok := widget.Lookup(DefaultToastStack); ok {
		t.Fatal("ValidateBoot mounted the default stack beside the app's own")
	}
	if _, ok := widget.Lookup("mine"); !ok {
		t.Fatal("the app's own stack is gone")
	}
}
