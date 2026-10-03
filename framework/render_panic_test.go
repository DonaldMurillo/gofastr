package framework

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/testkit"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type renderFailureTB struct {
	testing.TB
	messages []string
}

func (t *renderFailureTB) Errorf(format string, args ...any) {
	t.messages = append(t.messages, fmt.Sprintf(format, args...))
}

type hiddenRenderFailure struct{}

func (hiddenRenderFailure) Render() render.HTML           { panic("sidebar exploded") }
func (hiddenRenderFailure) RenderError(error) render.HTML { return "normal-looking fallback" }

func TestHarnessReportsRecoveredRenderPanic(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(old)
	app := NewApp(WithoutDefaultMiddleware())
	app.Router().Get("/broken", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := component.SafeRenderCtx(r.Context(), hiddenRenderFailure{})
		fmt.Fprint(w, body)
	}))
	app.Router().Get("/healthy", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "healthy") }))
	reporter := &renderFailureTB{TB: t}
	harness := TestHarness(reporter, app)
	harness.Get("/healthy")
	if len(reporter.messages) != 0 {
		t.Fatal("healthy request failed")
	}
	for _, request := range []func() *TestResponse{
		func() *TestResponse { return harness.Get("/broken") },
		func() *TestResponse { return harness.AsUser("alice").Request("GET", "/broken", nil).Execute() },
	} {
		reporter.messages = nil
		response := request()
		if response.Status() != 200 || response.Body() != "normal-looking fallback" {
			t.Fatalf("recovery changed: %d %q", response.Status(), response.Body())
		}
		if len(reporter.messages) != 1 || !strings.Contains(reporter.messages[0], "hiddenRenderFailure: sidebar exploded") {
			t.Fatalf("request did not fail with component and panic: %v", reporter.messages)
		}
	}
	if text := logs.String(); !strings.Contains(text, "level=ERROR") || !strings.Contains(text, "stack=") || !strings.Contains(text, "hiddenRenderFailure.Render") {
		t.Fatalf("missing error-level render stack: %s", text)
	}
}

func TestPlainRouterRenderPanicStatus(t *testing.T) {
	site := uiapp.NewApp("panic-status")
	site.Register("/broken", hiddenRenderFailure{}, nil,
		uiapp.InterceptFrom("/healthy", uiapp.ScreenDrawer))
	site.Register("/healthy", healthyRenderStatus{}, nil)
	app := NewApp(WithoutDefaultMiddleware()).Mount(uihost.New(site))
	srv := httptest.NewServer(app.Router())
	defer srv.Close()
	for _, mode := range []string{"full", "navigate", "intercept", "fills"} {
		t.Run(mode, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, srv.URL+"/broken", nil)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "full" {
				req.Header.Set("X-Gofastr-Navigate", "1")
				req.Header.Set("X-Gofastr-From", "/healthy")
			}
			if mode == "intercept" {
				req.Header.Set("X-Gofastr-Intercept", "1")
			}
			if mode == "fills" {
				req.Header.Set("X-Gofastr-Fills", "2")
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusInternalServerError {
				t.Errorf("recovered render panic status = %d, want 500", resp.StatusCode)
			}
			// The overlay marker is the proof this request actually went
			// through the intercept path; without it a regression that stops
			// intercepting would still pass via the full-page arm above.
			if mode == "intercept" && resp.Header.Get("X-Gofastr-Overlay") == "" {
				t.Error("intercept mode answered without X-Gofastr-Overlay — the request did not exercise the intercept path")
			}
			fallback := "normal-looking fallback"
			if mode == "navigate" || mode == "fills" {
				fallback = "Page not found"
			}
			if !strings.Contains(string(body), fallback) {
				t.Errorf("fallback body lost: %s", body)
			}
		})
	}
	resp, err := http.Get(srv.URL + "/healthy")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthy status = %d, want 200", resp.StatusCode)
	}
}

type healthyRenderStatus struct{}

func (healthyRenderStatus) Render() render.HTML { return "healthy" }

func TestPlainRouterLayoutRenderPanicStatus(t *testing.T) {
	site := uiapp.NewApp("layout-status")
	aside := uiapp.NewOutlet("aside", uiapp.OutletOptions{
		Deferred: true,
		Loading:  &uiapp.Loading{Show: uiapp.LoadingComponent("loading")},
	})
	shell := uiapp.NewLayout("shell", uiapp.LayoutSpec{Outlets: []*uiapp.Outlet{aside}},
		func(ctx context.Context, tree *uiapp.LayoutTree) render.HTML {
			return render.Join(tree.Primary(), tree.Place(aside))
		})
	site.SetDefaultLayout(shell)
	site.RegisterScreen(uiapp.NewScreen("/broken", healthyRenderStatus{}).
		Fill(aside, hiddenRenderFailure{}), nil)
	site.Register("/healthy", healthyRenderStatus{}, nil)
	host := uihost.New(site)
	app := NewApp(WithoutDefaultMiddleware()).Mount(host)
	srv := httptest.NewServer(app.Router())
	defer srv.Close()
	// Obtain a real session so a part reaches rendering rather than the
	// dead-session reset. No TestHarness observer is installed.
	page, err := http.Get(srv.URL + "/healthy")
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	for _, mode := range []string{"full", "envelope", "part"} {
		t.Run(mode, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, srv.URL+"/broken", nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, cookie := range page.Cookies() {
				req.AddCookie(cookie)
			}
			if mode != "full" {
				req.Header.Set("X-Gofastr-Navigate", "1")
				req.Header.Set("X-Gofastr-From", "/healthy")
				req.Header.Set("X-Gofastr-Fills", "2")
			}
			if mode == "part" {
				req.Header.Set("X-Gofastr-Part", "l:shell#aside")
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusInternalServerError {
				t.Errorf("recovered outlet panic status = %d, want 500; body %s", resp.StatusCode, body)
			}
			if mode == "envelope" && resp.Header.Get("X-Gofastr-Envelope") != "2" {
				t.Error("request did not exercise fills envelope")
			}
		})
	}
}

func TestAllowRenderPanicsIsolationAndCleanup(t *testing.T) {
	site := uiapp.NewApp("intentional-panic")
	site.Register("/broken", hiddenRenderFailure{}, nil)
	app := NewApp(WithoutDefaultMiddleware()).Mount(uihost.New(site))
	plain := app.Router()
	check := func(t *testing.T, handler http.Handler, want int) {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/broken", nil))
		if response.Code != want || !strings.Contains(response.Body.String(), "normal-looking fallback") {
			t.Fatalf("recovery = %d %q, want %d with fallback", response.Code, response.Body.String(), want)
		}
	}
	var exempt http.Handler
	t.Run("parallel", func(t *testing.T) {
		t.Run("intentional", func(t *testing.T) {
			t.Parallel()
			exempt = testkit.AllowRenderPanics(t, plain)
			check(t, exempt, http.StatusOK)
		})
		t.Run("ordinary", func(t *testing.T) {
			t.Parallel()
			check(t, plain, http.StatusInternalServerError)
		})
	})
	// Cleanup revokes even a wrapper retained after its owning test finishes.
	check(t, exempt, http.StatusInternalServerError)
}
