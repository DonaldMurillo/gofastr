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
