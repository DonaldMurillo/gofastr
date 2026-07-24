package uihost

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// A hard GET of a redirect path yields a permanent (308) redirect to the
// resolved target, short-circuiting before screen resolution.
func TestRedirectHardGET308(t *testing.T) {
	a := app.NewApp("redirects")
	a.Register("/", &testHomeComp{}, nil)
	a.Redirect("/old", "/new")
	a.Register("/new", &testHomeComp{}, nil)
	ds := New(a)

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/old", nil))
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("GET /old: got %d, want 308", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/new" {
		t.Errorf("Location: got %q, want /new", loc)
	}
}

// A pattern redirect resolves the passthrough params into the Location.
func TestRedirectPatternHardGET308(t *testing.T) {
	a := app.NewApp("redirects")
	a.Register("/", &testHomeComp{}, nil)
	a.RedirectPattern("/old/{id}", "/new/{id}")
	a.Register("/new/{id}", &paramJSONComp{}, nil)
	ds := New(a)

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/old/42", nil))
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("GET /old/42: got %d, want 308", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/new/42" {
		t.Errorf("Location: got %q, want /new/42", loc)
	}
}

// The route manifest carries the redirect target so the client can rewrite
// SPA navigation without a round-trip.
func TestRedirectInRouteManifest(t *testing.T) {
	a := app.NewApp("redirects")
	a.Register("/", &testHomeComp{}, nil)
	a.Redirect("/old", "/new")
	ds := New(a)

	body := ds.buildRouteScript()
	start := strings.IndexByte(body, '[')
	end := strings.LastIndexByte(body, ']')
	if start < 0 || end <= start {
		t.Fatalf("could not find manifest JSON array: %s", body)
	}
	var raw []map[string]any
	if err := json.Unmarshal([]byte(body[start:end+1]), &raw); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	var found map[string]any
	for _, e := range raw {
		if e["path"] == "/old" {
			found = e
			break
		}
	}
	if found == nil {
		t.Fatal("redirect /old missing from manifest")
	}
	if found["redirect"] != "/new" {
		t.Errorf("manifest redirect field: got %v, want /new", found["redirect"])
	}
}

type paramJSONComp struct{ id string }

func (p *paramJSONComp) SetParams(m map[string]string) { p.id = m["id"] }
func (p *paramJSONComp) Render() render.HTML           { return render.HTML("<p>" + p.id + "</p>") }
