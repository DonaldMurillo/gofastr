package widget_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// A widget mounted on a prefixed group published its unprefixed paths
// (2026-10-27 U33), so the runtime fetched URLs the router had no route for.
func TestPrefixedMountPublishesItsURLs(t *testing.T) {
	root := router.New()
	def := widget.New("prefixed-mount").
		Signal("n", widget.SignalFunc(func() (any, error) { return 1, nil })).
		Build()
	widget.Mount(root.Group("/v2"), &def)

	rec := httptest.NewRecorder()
	widget.ServeWidgetList(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/widgets", nil))
	var list []struct {
		Cfg struct {
			Name       string `json:"name"`
			StylePath  string `json:"stylePath"`
			ChromePath string `json:"chromePath"`
			StatePath  string `json:"statePath"`
		} `json:"cfg"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]string{}
	for _, e := range list {
		if e.Cfg.Name == "prefixed-mount" {
			cfg = map[string]string{"stylePath": e.Cfg.StylePath, "chromePath": e.Cfg.ChromePath, "statePath": e.Cfg.StatePath}
		}
	}
	if len(cfg) == 0 {
		t.Fatalf("catalog has no prefixed-mount entry:\n%s", rec.Body)
	}
	for _, key := range []string{"stylePath", "chromePath", "statePath"} {
		u := cfg[key]
		if !strings.HasPrefix(u, "/v2/") {
			t.Errorf("%s = %q, want it under /v2", key, u)
			continue
		}
		got := httptest.NewRecorder()
		root.ServeHTTP(got, httptest.NewRequest(http.MethodGet, u, nil))
		if got.Code != http.StatusOK {
			t.Errorf("GET %s (%s) = %d, want 200", u, key, got.Code)
		}
	}
	if def.StyleURL() != cfg["stylePath"] || def.ChromeURL() != cfg["chromePath"] || def.StateURL() != cfg["statePath"] {
		t.Errorf("URL methods (%q %q %q) differ from the catalog %v", def.StyleURL(), def.ChromeURL(), def.StateURL(), cfg)
	}
	// The declared paths stay as declared: the prefix belongs to the mount.
	if def.StylePath != "/core-ui/widget/prefixed-mount/style.css" {
		t.Errorf("StylePath = %q, want the unprefixed default", def.StylePath)
	}
}

// A second host in the process served only a shared widget's stylesheet
// (PR #666 review), so the state, chrome and RPC URLs its catalog and
// markup name were 404s on that host.
func TestMountSharedServesEveryEndpoint(t *testing.T) {
	widget.IsolateForTest(t)
	first, second := router.New(), router.New()
	def := widget.New("shared-mount").
		Signal("n", widget.SignalFunc(func() (any, error) { return 1, nil })).
		RPC("POST", "/core-ui/widget/shared-mount/ping", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})).
		Build()
	widget.Mount(first.Group("/v2"), &def)
	widget.MountShared(second, &def)
	// A repeat registers nothing again; the router panics on a duplicate.
	widget.MountShared(second, &def)
	for _, c := range []struct {
		method, url string
		want        int
	}{
		{http.MethodGet, def.StyleURL(), http.StatusOK},
		{http.MethodGet, def.StateURL(), http.StatusOK},
		{http.MethodGet, def.ChromeURL(), http.StatusOK},
		{http.MethodPost, "/v2/core-ui/widget/shared-mount/ping", http.StatusNoContent},
	} {
		got := httptest.NewRecorder()
		second.ServeHTTP(got, httptest.NewRequest(c.method, c.url, nil))
		if got.Code != c.want {
			t.Errorf("%s %s on the second router = %d, want %d", c.method, c.url, got.Code, c.want)
		}
	}
}

// The routes MountShared adds carry the gate Mount puts on the same
// endpoints: a shared copy must not serve what the original refuses.
func TestMountSharedKeepsTheSessionGate(t *testing.T) {
	widget.IsolateForTest(t)
	first, second := router.New(), router.New()
	def := widget.New("shared-gated").
		Signal("n", widget.SignalFunc(func() (any, error) { return 1, nil })).
		Build()
	def.RequireSession = true
	widget.Mount(first, &def)
	widget.MountShared(second, &def)
	for _, url := range []string{def.StateURL(), def.ChromeURL()} {
		got := httptest.NewRecorder()
		second.ServeHTTP(got, httptest.NewRequest(http.MethodGet, url, nil))
		if got.Code != http.StatusForbidden {
			t.Errorf("GET %s without a session = %d, want 403", url, got.Code)
		}
	}
}
