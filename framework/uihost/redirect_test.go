package uihost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// requireHardRedirect asserts a hard GET of path yields a permanent (308)
// redirect to wantLocation, short-circuiting before screen resolution.
func requireHardRedirect(t *testing.T, ds *UIHost, path, wantLocation string) {
	t.Helper()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("GET %s: got %d, want 308", path, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != wantLocation {
		t.Errorf("Location: got %q, want %s", loc, wantLocation)
	}
}

// A hard GET of a redirect path yields a permanent (308) redirect to the
// resolved target, short-circuiting before screen resolution.
func TestRedirectHardGET308(t *testing.T) {
	a := app.NewApp("redirects")
	a.Register("/", &testHomeComp{}, nil)
	a.Redirect("/old", "/new")
	a.Register("/new", &testHomeComp{}, nil)
	requireHardRedirect(t, New(a), "/old", "/new")
}

// A pattern redirect resolves the passthrough params into the Location.
func TestRedirectPatternHardGET308(t *testing.T) {
	a := app.NewApp("redirects")
	a.Register("/", &testHomeComp{}, nil)
	a.RedirectPattern("/old/{id}", "/new/{id}")
	a.Register("/new/{id}", &paramJSONComp{}, nil)
	requireHardRedirect(t, New(a), "/old/42", "/new/42")
}

// The route manifest carries the redirect target so the client can rewrite
// SPA navigation without a round-trip.
func TestRedirectInRouteManifest(t *testing.T) {
	a := app.NewApp("redirects")
	a.Register("/", &testHomeComp{}, nil)
	a.Redirect("/old", "/new")
	ds := New(a)

	body := ds.buildRouteScript(context.Background())
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
