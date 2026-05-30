package print

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// getHost is like get() but lets a test set the request Host header — used
// to prove the PDF path does NOT trust it.
func getHost(t *testing.T, h http.Handler, path, host string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ----- SSRF regression: the PDF app.css URL must come from BaseURL, never
// from the client-controlled Host header. -----------------------------------

func TestPDFAppCSSUsesBaseURLNotHost(t *testing.T) {
	fr := &fakeRenderer{}
	b := New(Config{
		DefaultAccess: Public,
		AppCSSURL:     "/__gofastr/app.css",
		BaseURL:       "https://canonical.example",
		PDFRenderer:   fr,
	}).Document(Document{Name: "doc", Path: "/doc", Build: docBuild("x")})

	getHost(t, mount(t, b), "/print/doc/pdf", "evil.attacker.example")

	if !strings.Contains(fr.gotHTML, `href="https://canonical.example/__gofastr/app.css"`) {
		t.Errorf("PDF app.css should use BaseURL; got %q", fr.gotHTML)
	}
	if strings.Contains(fr.gotHTML, "evil.attacker.example") {
		t.Fatalf("SSRF: spoofed Host leaked into PDF HTML: %q", fr.gotHTML)
	}
	if fr.gotBaseURL != "https://canonical.example" {
		t.Errorf("renderer baseURL = %q, want BaseURL", fr.gotBaseURL)
	}
}

func TestPDFAppCSSRelativeWhenNoBaseURL(t *testing.T) {
	fr := &fakeRenderer{}
	b := New(Config{
		DefaultAccess: Public,
		AppCSSURL:     "/__gofastr/app.css", // no BaseURL
		PDFRenderer:   fr,
	}).Document(Document{Name: "doc", Path: "/doc", Build: docBuild("x")})

	getHost(t, mount(t, b), "/print/doc/pdf", "evil.attacker.example")

	if strings.Contains(fr.gotHTML, "evil.attacker.example") {
		t.Fatalf("SSRF: Host leaked into PDF HTML without BaseURL: %q", fr.gotHTML)
	}
	if !strings.Contains(fr.gotHTML, `href="/__gofastr/app.css"`) {
		t.Errorf("expected relative app.css when no BaseURL; got %q", fr.gotHTML)
	}
}

// ----- Build panic recovery -------------------------------------------------

func TestBuildPanicIsClean500(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "doc", Path: "/doc",
		Build: func(*http.Request) (component.Component, error) { panic("boom-secret") },
	})
	rec := get(t, mount(t, b), "/print/doc")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "boom-secret") {
		t.Errorf("panic value leaked to client: %q", rec.Body.String())
	}
}

// ----- writeBuildError branches --------------------------------------------

func TestBuildErrorForbidden403(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "doc", Path: "/doc",
		Build: func(*http.Request) (component.Component, error) { return nil, ErrForbidden },
	})
	if rec := get(t, mount(t, b), "/print/doc"); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestBuildErrorGeneric500(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "doc", Path: "/doc",
		Build: func(*http.Request) (component.Component, error) { return nil, errors.New("db down: secret") },
	})
	rec := get(t, mount(t, b), "/print/doc")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("error detail leaked: %q", rec.Body.String())
	}
}

// ----- RequireOwner allow path ----------------------------------------------

func TestRequireOwnerAllows(t *testing.T) {
	b := New(Config{}).Document(Document{
		Name: "doc", Path: "/doc",
		Access: RequireOwner(func(*http.Request, any) bool { return true }),
		Build:  docBuild("<p>ok</p>"),
	})
	if rec := get(t, authed(mount(t, b)), "/print/doc"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// ----- auto-print: wiring + the shared script route -------------------------

func TestAutoPrintWiredOnHTMLNotPDF(t *testing.T) {
	fr := &fakeRenderer{}
	b := New(Config{DefaultAccess: Public, PDFRenderer: fr}).Document(Document{
		Name: "doc", Path: "/doc", AutoPrint: true, Build: docBuild("x"),
	})
	h := mount(t, b)

	html := get(t, h, "/print/doc").Body.String()
	if !strings.Contains(html, `<script src="/print/__autoprint.js"></script>`) {
		t.Errorf("HTML route missing auto-print script: %q", html)
	}
	get(t, h, "/print/doc/pdf")
	if strings.Contains(fr.gotHTML, "__autoprint.js") {
		t.Errorf("PDF must not include auto-print script: %q", fr.gotHTML)
	}
}

func TestAutoPrintRouteServesScript(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{Name: "doc", Path: "/doc", Build: docBuild("x")})
	rec := get(t, mount(t, b), "/print/__autoprint.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("Content-Type = %q, want text/javascript", ct)
	}
	if !strings.Contains(rec.Body.String(), "window.print()") {
		t.Errorf("script body missing window.print(): %q", rec.Body.String())
	}
}

// ----- PrintLink ------------------------------------------------------------

func TestPrintLink(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "invoice", Path: "/invoice/{id}", Build: docBuild("x"),
	})
	mount(t, b)

	got := string(b.PrintLink("invoice", map[string]string{"id": "42"}, "Print invoice"))
	if !strings.Contains(got, `href="/print/invoice/42"`) {
		t.Errorf("href wrong: %q", got)
	}
	if !strings.Contains(got, `target="_blank"`) || !strings.Contains(got, `rel="noopener"`) {
		t.Errorf("missing safe new-tab attrs: %q", got)
	}
	if !strings.Contains(got, ">Print invoice<") {
		t.Errorf("label missing: %q", got)
	}

	// Unknown document → empty.
	if s := b.PrintLink("nope", nil, "x"); s != "" {
		t.Errorf("unknown doc should yield empty, got %q", s)
	}
	// Default label.
	if s := string(b.PrintLink("invoice", map[string]string{"id": "1"}, "")); !strings.Contains(s, ">Print<") {
		t.Errorf("default label missing: %q", s)
	}
	// Param value is path-escaped.
	if s := string(b.PrintLink("invoice", map[string]string{"id": "a/b"}, "x")); strings.Contains(s, "a/b") {
		t.Errorf("param not escaped: %q", s)
	}
}

// ----- pdfFilename across param counts --------------------------------------

func TestPDFFilenameParamCounts(t *testing.T) {
	cases := []struct {
		name, path, get, want string
	}{
		{"flat", "/flat", "/print/flat/pdf", `filename="flat.pdf"`},
		{"inv", "/inv/{id}", "/print/inv/9/pdf", `filename="inv-9.pdf"`},
		{"multi", "/m/{a}/{b}", "/print/m/1/2/pdf", `filename="multi.pdf"`}, // 2 params → no suffix
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := New(Config{DefaultAccess: Public, PDFRenderer: &fakeRenderer{}}).
				Document(Document{Name: c.name, Path: c.path, Build: docBuild("x")})
			rec := get(t, mount(t, b), c.get)
			if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, c.want) {
				t.Errorf("Content-Disposition = %q, want %q", cd, c.want)
			}
		})
	}
}

// ----- RegisterRoutes idempotency + path-collision guards -------------------

func TestRegisterRoutesIdempotent(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{Name: "d", Path: "/d", Build: docBuild("x")})
	h := mount(t, b)
	// Second call must not panic on the underlying ServeMux.
	if err := b.RegisterRoutes(nil); err != nil {
		t.Fatalf("second RegisterRoutes: %v", err)
	}
	if rec := get(t, h, "/print/d"); rec.Code != http.StatusOK {
		t.Fatalf("route broken after second register: %d", rec.Code)
	}
}

func TestDocumentPathCollisionPanics(t *testing.T) {
	t.Run("autoprint suffix", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatalf("expected panic for autoprint-colliding path")
			}
		}()
		New(Config{}).Document(Document{Name: "x", Path: autoPrintSuffix, Build: docBuild("x")})
	})
	t.Run("duplicate path", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatalf("expected panic for duplicate path")
			}
		}()
		New(Config{}).
			Document(Document{Name: "a", Path: "/same", Build: docBuild("x")}).
			Document(Document{Name: "b", Path: "/same", Build: docBuild("y")})
	})
}

// ----- component CSS is collected + inlined ---------------------------------

var followupCompStyle = registry.RegisterStyle("print-followup-comp", func(style.Theme) string {
	return `[data-fui-comp="print-followup-comp"]{color:var(--color-primary)}`
})

type followupStyledComp struct{}

func (followupStyledComp) Render() render.HTML {
	return followupCompStyle.WrapHTML(render.HTML(`<p>styled</p>`))
}

func TestComponentCSSInlinedWhenUsed(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "doc", Path: "/doc",
		Build: func(*http.Request) (component.Component, error) { return followupStyledComp{}, nil },
	})
	body := get(t, mount(t, b), "/print/doc").Body.String()
	if !strings.Contains(body, `[data-fui-comp="print-followup-comp"]`) {
		t.Errorf("scoped component CSS not inlined into print shell: %q", body)
	}
}

func TestNoComponentCSSWhenUnused(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "doc", Path: "/doc", Build: docBuild("<p>plain</p>"),
	})
	body := get(t, mount(t, b), "/print/doc").Body.String()
	if strings.Contains(body, "print-followup-comp") {
		t.Errorf("unexpected component CSS for a marker-free body")
	}
}
