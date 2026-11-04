package uihost

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/router"
)

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
