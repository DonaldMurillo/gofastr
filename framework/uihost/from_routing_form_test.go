package uihost

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type fromParamScreen struct{ slug string }

func (s *fromParamScreen) SetParams(m map[string]string) { s.slug = m["project"] }
func (s *fromParamScreen) Render() render.HTML           { return render.Text("P[" + s.slug + "]") }

func fromParamHost(t *testing.T) *UIHost {
	t.Helper()
	a := app.NewApp("t")
	pass := func(ctx context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }
	a.SetDefaultLayout(app.NewLayout("shell", app.LayoutSpec{}, pass))
	g := app.NewScreenGroup("/projects/{project}", app.NewLayout("project", app.LayoutSpec{}, pass))
	g.Screen(app.NewScreen("/projects/{project}", &fromParamScreen{}), nil)
	g.Screen(app.NewScreen("/projects/{project}/issues/{n}", &fromParamScreen{}), nil)
	a.Router.ScreenGroup(g)
	return New(a)
}

// The runtime sends X-Gofastr-From as location.pathname, percent-encoded.
// It is read in routing form, like the request path, so a project whose
// name the browser encodes keeps its layer across its own pages.
func TestFromHeaderKeepsEncodedParamLayer(t *testing.T) {
	ds := fromParamHost(t)
	w := partialGet(t, ds, "/projects/my%20proj/issues/1", "/projects/my%20proj")
	if got := w.Header().Get("X-Gofastr-Swap"); got != "g:/projects/my proj/:project" {
		t.Fatalf("X-Gofastr-Swap = %q, want the project layer kept", got)
	}
}

// A layer key holding non-ASCII travels percent-encoded (with % itself
// escaped), so the runtime's decodeURIComponent gives back the DOM's key.
func TestSwapHeaderIsASCII(t *testing.T) {
	ds := fromParamHost(t)
	w := partialGet(t, ds, "/projects/caf%C3%A9/issues/1", "/projects/caf%C3%A9")
	if got := w.Header().Get("X-Gofastr-Swap"); got != "g:/projects/caf%C3%A9/:project" {
		t.Fatalf("X-Gofastr-Swap = %q, want g:/projects/caf%%C3%%A9/:project", got)
	}
	// The key holds the decoded value, as the DOM's data-cui-layout-key
	// does, so a literal % goes out as %25 and decodes back to itself.
	full := httptest.NewRecorder()
	ds.ServeHTTP(full, httptest.NewRequest("GET", "/projects/100%25", nil))
	if !strings.Contains(full.Body.String(), `data-cui-layout-key="g:/projects/100%/:project"`) {
		t.Fatalf("full render lacks the decoded layer key: %s", full.Body.String())
	}
	w = partialGet(t, ds, "/projects/100%25/issues/1", "/projects/100%25")
	got := w.Header().Get("X-Gofastr-Swap")
	if dec, err := url.PathUnescape(got); err != nil || dec != "g:/projects/100%/:project" {
		t.Fatalf("X-Gofastr-Swap = %q decodes to %q (%v), want the DOM key", got, dec, err)
	}
}
