package uihost

import (
	"context"
	"encoding/json"
	stdhtml "html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// embedParamComp renders the param its route bound, so a test sees which
// concrete path the surface rendered.
type embedParamComp struct {
	name, value string
}

func (c *embedParamComp) SetParams(p map[string]string) { c.value = p[c.name] }
func (c *embedParamComp) Render() render.HTML {
	return render.HTML("<p>" + c.name + ":" + stdhtml.EscapeString(c.value) + "</p>")
}

// newParamEmbedFixture serves three surfaces: "report" on
// /reports/:id:int, "files" on a catch-all, and "static" on /static.
// /reports/:id/edit is an ordinary screen no surface renders.
func newParamEmbedFixture(t *testing.T) embedFixture {
	t.Helper()
	application := app.NewApp("Embed Params")
	report := app.NewScreen("/reports/:id:int", &embedParamComp{name: "id"})
	edit := app.NewScreen("/reports/:id/edit", &embedParamComp{name: "id"})
	files := app.NewScreen("/files/{p...}", &embedParamComp{name: "p"})
	static := app.NewScreen("/static", &embedSubjectComp{})
	for _, s := range []*app.Screen{report, edit, files, static} {
		application.RegisterScreen(s, nil)
	}
	eh, err := fembed.New(fembed.Config{
		Surfaces: []fembed.Surface{
			{Name: "report", Screen: report, Origins: []string{embedTestOrigin}},
			{Name: "files", Screen: files, Origins: []string{embedTestOrigin}},
			{Name: "static", Screen: static, Origins: []string{embedTestOrigin}},
		},
		BurnStore: fembed.NewMemoryBurnStore(),
		Resolve:   func(_ context.Context, subject string) (any, error) { return subject, nil },
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))
	return embedFixture{host: New(application, WithEmbed(eh)), embed: eh, surface: "report"}
}

var embedCfgRe = regexp.MustCompile(`<meta name="gofastr-embed" content="([^"]*)">`)

func shellConfig(t *testing.T, body string) map[string]string {
	t.Helper()
	m := embedCfgRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("shell carries no gofastr-embed config:\n%s", body)
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(stdhtml.UnescapeString(m[1])), &cfg); err != nil {
		t.Fatalf("decode shell config: %v", err)
	}
	return cfg
}

// A dynamic surface's frame URL carries the concrete app path after the
// surface name. The shell names it for the content fetch and the
// frame's own page path; the content renders it with the params the
// route binds. A path the surface's screen does not match is a 404.
func TestEmbedSurfaceBindsPathParams(t *testing.T) {
	f := newParamEmbedFixture(t)
	for shell, want := range map[string]struct{ path, content string }{
		"/__gofastr/embed/report/reports/42": {"/reports/42", "/__gofastr/embed/report/content?path=%2Freports%2F42"},
		"/__gofastr/embed/files/files/a/b":   {"/files/a/b", "/__gofastr/embed/files/content?path=%2Ffiles%2Fa%2Fb"},
		"/__gofastr/embed/static":            {"/static", "/__gofastr/embed/static/content"},
	} {
		rec := f.do(t, http.MethodGet, shell, "")
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", shell, rec.Code)
			continue
		}
		cfg := shellConfig(t, rec.Body.String())
		if cfg["path"] != want.path || cfg["content"] != want.content {
			t.Errorf("GET %s: path %q content %q, want %q and %q", shell, cfg["path"], cfg["content"], want.path, want.content)
		}
	}
	for _, shell := range []string{
		"/__gofastr/embed/report",                 // a dynamic surface needs its path
		"/__gofastr/embed/report/reports/abc",     // :int refuses it
		"/__gofastr/embed/report/reports/42/edit", // another screen's path
		"/__gofastr/embed/report/other",           // no screen
		"/__gofastr/embed/report/reports/4%2F2",   // an encoded separator
		"/__gofastr/embed/files/files/a%2Fb",      // one the catch-all would take
		"/__gofastr/embed/files/files/a%5Cb",      // an encoded backslash
		"/__gofastr/embed/files/files/%2E%2E/x",   // an encoded dot segment
		"/__gofastr/embed/static/static",          // a static surface takes no path
	} {
		if rec := f.do(t, http.MethodGet, shell, ""); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", shell, rec.Code)
		}
	}

	content := func(surface, query string) (int, string) {
		grant := f.grantFor(t, surface)
		rec := f.do(t, http.MethodGet, "/__gofastr/embed/"+surface+"/content"+query, "", func(req *http.Request) {
			req.Header.Set(embedGrantHeader, grant)
		})
		return rec.Code, rec.Body.String()
	}
	for query, want := range map[string]string{
		"?path=%2Freports%2F42": "<p>id:42</p>",
		"?path=%2Freports%2F7":  "<p>id:7</p>",
	} {
		if code, body := content("report", query); code != http.StatusOK || !regexp.MustCompile(regexp.QuoteMeta(want)).MatchString(body) {
			t.Errorf("content%s: status %d, want 200 with %q:\n%s", query, code, want, body)
		}
	}
	if code, body := content("files", "?path=%2Ffiles%2Fa%2Fb"); code != http.StatusOK || !regexp.MustCompile(`<p>p:a/b</p>`).MatchString(body) {
		t.Errorf("files content: status %d, want 200 with p:a/b:\n%s", code, body)
	}
	for _, query := range []string{"", "?path=%2Freports%2Fabc", "?path=%2Freports%2F42%2Fedit", "?path=reports%2F42", "?path=%2Freports%2F..%2F42"} {
		if code, _ := content("report", query); code != http.StatusNotFound {
			t.Errorf("report content%s: status %d, want 404", query, code)
		}
	}
	for _, query := range []string{"?path=files%2Fa", "?path=%2Ffiles%2F%25zz", "?path=%2Ffiles%2Fa%252Fb", "?path=%2Ffiles%2F%252E%252E%2Fx"} {
		if code, _ := content("files", query); code != http.StatusNotFound {
			t.Errorf("files content%s: status %d, want 404", query, code)
		}
	}
	if code, _ := content("static", "?path=%2Fstatic"); code != http.StatusNotFound {
		t.Errorf("static content with ?path=: status %d, want 404", code)
	}
}

// A dynamic surface's grant reads the widget catalog against its route
// pattern: a widget scoped to the section the pattern sits under is
// offered in the frame, one scoped elsewhere is not.
func TestEmbedDynamicCatalogUsesThePattern(t *testing.T) {
	widget.Mount(router.New(), widget.New("dynrep-section").PagesPrefix("/reports/").Definition())
	widget.Mount(router.New(), widget.New("dynrep-admin").Pages("/admin").Definition())
	f := newParamEmbedFixture(t)
	grant := f.grantFor(t, "report")
	rec := f.do(t, http.MethodGet, "/__gofastr/widgets?page=%2Fadmin", "", func(r *http.Request) {
		r.Header.Set(embedGrantHeader, grant)
	})
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"dynrep-section"`) || strings.Contains(body, `"dynrep-admin"`) {
		t.Errorf("catalog for a /reports/:id:int grant: %d %s, want dynrep-section and not dynrep-admin", rec.Code, body)
	}
}
