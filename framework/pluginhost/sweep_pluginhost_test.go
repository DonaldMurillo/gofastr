package pluginhost

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestBrokerRouteIsPlainHostScript(t *testing.T) {
	rt := router.New()
	RegisterBrokerRoute(rt)
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, BrokerScriptURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cross-Origin-Resource-Policy"); got != "" {
		t.Errorf("broker CORP = %q, want none", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("broker CSP = %q, want none", got)
	}
}

func TestMountMarkerDefaults(t *testing.T) {
	got := string(MountMarker(MountConfig{Plugin: "p"}))
	for _, want := range []string{`data-fui-plugin-docid="` + DefaultDocID + `"`, `data-fui-plugin-minheight="` + DefaultMinHeight + `"`} {
		if !strings.Contains(got, want) {
			t.Errorf("marker %q lacks %q", got, want)
		}
	}
}

func TestCheckHostRequirementsNilLoggerWarns(t *testing.T) {
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)
	CheckHostRequirements(nil, "camera=()", scannerModule(t, "camera"))
	if warnCount(&buf) != 1 {
		t.Fatalf("want one warning on slog.Default, got:\n%s", buf.String())
	}
}

func TestFramedAssetDropsUpstreamXFrameOptions(t *testing.T) {
	rt := router.New()
	srv := NewAssetServer(fstest.MapFS{"e.html": {Data: []byte("x")}}, "/p", []AssetSpec{{Name: "e.html", Framed: true}})
	srv.Register(rt)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		rt.ServeHTTP(w, r)
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://h/p/e.html", nil))
	if got := rec.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("X-Frame-Options = %q on a framed asset", got)
	}
}

func TestRequestOriginHostRules(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = ""
	if got, ok := requestOrigin(r); !ok || got != "http://localhost" {
		t.Errorf("empty Host: %q %v, want http://localhost", got, ok)
	}
	r.Host = strings.Repeat("a", 254)
	if _, ok := requestOrigin(r); ok {
		t.Error("a Host over 253 bytes was accepted")
	}
}

func TestJoinPathOneSlash(t *testing.T) {
	for _, c := range [][3]string{{"", "a.js", "/a.js"}, {"/p/", "/a.js", "/p/a.js"}, {"/p", "a.js", "/p/a.js"}} {
		if got := joinPath(c[0], c[1]); got != c[2] {
			t.Errorf("joinPath(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
