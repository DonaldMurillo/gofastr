package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// drawsMissing learns while it draws that its record is gone, the way an
// entityui record view does, and says so through app.SetStatus.
type drawsMissing struct{ component.ContextOnly }

func (drawsMissing) RenderCtx(ctx context.Context) render.HTML {
	app.SetStatus(ctx, http.StatusNotFound)
	return render.HTML(`<p>no such record</p>`)
}

// drawsMissingDeclaresGone sets 404 while drawing but declares 410
// through ScreenStatusCode: the declaration outranks the draw.
type drawsMissingDeclaresGone struct{ drawsMissing }

func (drawsMissingDeclaresGone) ScreenStatusCode() int { return http.StatusGone }

// A status set during the render reaches the full page and the partial
// navigation alike, with the body intact.
func TestSetStatusReachesPageAndPartial(t *testing.T) {
	a := app.NewApp("status")
	a.Register("/missing", drawsMissing{}, nil)
	a.Register("/", &testHomeComp{}, nil)
	ds := New(a)

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("full page status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no such record") {
		t.Errorf("full page body missing: %s", rec.Body.String())
	}

	for _, from := range []string{"", "/"} {
		req := httptest.NewRequest(http.MethodGet, "/missing", nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		if from != "" {
			req.Header.Set("X-Gofastr-From", from)
		}
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("partial (from %q) status = %d, want 404", from, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "no such record") {
			t.Errorf("partial (from %q) body missing: %s", from, rec.Body.String())
		}
	}
}

// drawsMissingByID and goneByID are the dynamic-route forms.
type drawsMissingByID struct{ drawsMissing }

func (*drawsMissingByID) SetParams(map[string]string) {}

type goneByID struct{ drawsMissingDeclaresGone }

func (*goneByID) SetParams(map[string]string) {}

// A dynamic route's llm.md for a record that is gone answers 404, the
// way a Load answering ErrNotFound does, never a 200 doc of the
// not-found body. A present record still gets its doc.
func TestDynamicLLMMDSetStatus404(t *testing.T) {
	a := app.NewApp("status")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/missing/{id}", &drawsMissingByID{}, nil)
	a.Register("/gone/{id}", &goneByID{}, nil)
	a.Register("/things/{id}", &loadedTitleComp{}, nil)
	ds := New(a, WithPublicLLMMD())

	for _, path := range []string{"/missing/1/llm.md", "/gone/1/llm.md"} {
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "no such record") {
			t.Errorf("%s = %d, want 404 without the not-found doc:\n%s", path, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/things/42/llm.md", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Thing 42") {
		t.Errorf("present record llm.md = %d, want 200 with its doc", rec.Code)
	}
}

// Negotiated markdown for a record that is gone carries the page's
// status: the agent asking for markdown gets the same 404 (or the
// screen's declared 410) a browser gets, with the not-found body.
func TestNegotiatedMarkdownSetStatus(t *testing.T) {
	a := app.NewApp("status")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/missing/{id}", &drawsMissingByID{}, nil)
	a.Register("/gone/{id}", &goneByID{}, nil)
	a.Register("/things/{id}", &loadedTitleComp{}, nil)
	// Value screens skip DI and render per URL like pointer ones: a
	// declared status and one set while drawing both apply.
	a.Register("/declared", drawsMissingDeclaresGone{}, nil)
	a.Register("/drawn", drawsMissing{}, nil)
	ds := New(a, WithPublicLLMMD(), WithMarkdownNegotiation())

	for path, want := range map[string]int{"/missing/1": http.StatusNotFound, "/gone/1": http.StatusGone, "/things/42": http.StatusOK, "/declared": http.StatusGone, "/drawn": http.StatusNotFound} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept", "text/markdown")
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		if rec.Code != want || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/markdown") {
			t.Errorf("%s = %d (%s), want %d markdown", path, rec.Code, rec.Header().Get("Content-Type"), want)
		}
	}
}

// loadedStatus reads its status from state only Load sets: called on an
// unloaded instance it panics.
type loadedStatus struct{ rec *struct{ gone bool } }

func (s *loadedStatus) SetParams(map[string]string) {}
func (s *loadedStatus) Load(context.Context) error {
	s.rec = &struct{ gone bool }{gone: true}
	return nil
}
func (s *loadedStatus) Render() render.HTML { return "<p>secret record</p>" }
func (s *loadedStatus) ScreenStatusCode() int {
	if s.rec.gone {
		return http.StatusGone
	}
	return 0
}

// A policy-refused screen's llm.md is the withheld doc: the status check
// never reaches the unloaded screen (whose ScreenStatusCode reads Load
// state) and nothing of its content leaks.
func TestRefusedLLMMDSkipsStatusCheck(t *testing.T) {
	a := app.NewApp("status")
	a.Register("/", &testHomeComp{}, nil)
	deny := app.PolicyFunc(func(context.Context) app.Decision {
		return app.Decision{Kind: app.DecisionBlock, Status: http.StatusForbidden}
	})
	a.RegisterScreen(app.NewScreen("/things/{id}", &loadedStatus{}).WithPolicy(deny), nil)
	ds := New(a, WithPublicLLMMD(), WithMarkdownNegotiation())

	for _, accept := range []string{"", "text/markdown"} {
		path := "/things/1/llm.md"
		if accept != "" {
			path = "/things/1"
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), "secret record") {
			t.Errorf("%s (Accept %q) leaked the refused screen:\n%s", path, accept, rec.Body.String())
		}
	}
}

// The screen's own ScreenStatusCode outranks a status its render set.
func TestScreenStatusCodeBeatsSetStatus(t *testing.T) {
	a := app.NewApp("status")
	a.Register("/gone", drawsMissingDeclaresGone{}, nil)
	ds := New(a)

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/gone", nil))
	if rec.Code != http.StatusGone {
		t.Errorf("status = %d, want 410", rec.Code)
	}
}

// RenderScreen honours a status set during the render when the caller
// named none, and an explicit ScreenResponse.Status still wins.
func TestRenderScreenSetStatus(t *testing.T) {
	ds := New(newRecoverApp())

	rec := httptest.NewRecorder()
	ds.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/x", nil), drawsMissing{}, ScreenResponse{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	ds.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/x", nil), drawsMissing{},
		ScreenResponse{Status: http.StatusUnauthorized})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("explicit status = %d, want 401 (explicit must win)", rec.Code)
	}
}

// An embed surface whose record is gone still answers 200 with its
// not-found body: the embed loader (boot-embed.js) mounts only a 2xx
// and shows "This panel could not load" for anything else, so a 404
// here would hide the body the screen drew for exactly this case.
func TestEmbedContentKeeps200(t *testing.T) {
	application := app.NewApp("Embed Test")
	missing := app.NewScreen("/missing", drawsMissing{}).WithTitle("Missing")
	application.RegisterScreen(missing, nil)
	eh, err := fembed.New(fembed.Config{
		Surfaces:  []fembed.Surface{{Name: "missing", Screen: missing, Origins: []string{embedTestOrigin}}},
		BurnStore: fembed.NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))
	f := embedFixture{host: New(application, WithEmbed(eh)), embed: eh, surface: "missing"}

	grant := f.grantFor(t, "missing")
	rec := f.do(t, http.MethodGet, "/__gofastr/embed/missing/content", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, grant)
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "no such record") {
		t.Errorf("embed content = %d, want 200 with the not-found body:\n%s", rec.Code, rec.Body.String())
	}
}
