package widget_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// The catalog listed every widget scoped to the page to anyone holding a
// session, and an anonymous page view mints one: the admin drawer and
// palette, and every widget behind RequireAuthenticated, named
// themselves to a caller whose /chrome would 403. It now lists only
// what the caller passes: the widget's gate and its ListedFor.
func TestCatalogListsOnlyReachable(t *testing.T) {
	widget.IsolateForTest(t)
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	widget.SetAuthenticatedCheck(func(r *http.Request) bool { return r.Header.Get("X-Signed-In") == "1" })
	t.Cleanup(func() { widget.SetAuthenticatedCheck(nil) })

	routes := router.New()
	signedIn := widget.New("audience-signed-in").Pages("/audience").Build()
	signedIn.RequireAuthenticated = true
	staff := widget.New("audience-staff").Pages("/audience").
		ListedFor(func(r *http.Request) bool { return r.Header.Get("X-Staff") == "1" }).Build()
	panics := widget.New("audience-panics").Pages("/audience").
		ListedFor(func(*http.Request) bool { panic("host bug") }).Build()
	open := widget.New("audience-open").Pages("/audience").Build()
	for _, d := range []*widget.Definition{&signedIn, &staff, &panics, &open} {
		widget.Mount(routes, d)
	}
	widget.MountRuntime(routes)

	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    []string
		absent  []string
	}{
		{"anonymous", nil, []string{"audience-open"}, []string{"audience-signed-in", "audience-staff", "audience-panics"}},
		{"signed in", map[string]string{"X-Signed-In": "1"}, []string{"audience-open", "audience-signed-in"}, []string{"audience-staff", "audience-panics"}},
		{"staff", map[string]string{"X-Signed-In": "1", "X-Staff": "1"}, []string{"audience-open", "audience-signed-in", "audience-staff"}, []string{"audience-panics"}},
	} {
		for _, page := range []string{"?page=/audience", ""} {
			req := httptest.NewRequest(http.MethodGet, "/__gofastr/widgets"+page, nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			routes.ServeHTTP(rec, req)
			body := rec.Body.String()
			for _, name := range tc.want {
				if !strings.Contains(body, `"name":"`+name+`"`) {
					t.Errorf("%s %q: catalog omits %s: %s", tc.name, page, name, body)
				}
			}
			for _, name := range tc.absent {
				if strings.Contains(body, `"name":"`+name+`"`) {
					t.Errorf("%s %q: catalog lists %s", tc.name, page, name)
				}
			}
		}
	}

	// The panic that hid the widget is logged, not swallowed.
	if !strings.Contains(logs.String(), "ListedFor hook panicked") || !strings.Contains(logs.String(), "audience-panics") {
		t.Errorf("a panicking ListedFor left no log: %s", logs.String())
	}

	if !staff.OfferedTo(withHeader("X-Staff", "1")) || staff.OfferedTo(withHeader("X-Staff", "0")) {
		t.Error("OfferedTo does not follow ListedFor")
	}
}

func withHeader(k, v string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/audience", nil)
	r.Header.Set(k, v)
	return r
}
