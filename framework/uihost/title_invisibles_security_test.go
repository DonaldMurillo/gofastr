package uihost

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Dynamic-route titles re-read ScreenTitle() AFTER Load (core-ui/app:
// "re-read ScreenTitle() AFTER Load so dynamic routes (e.g. /docs/:slug)
// can compute the title from data fetched in Load"), so the title is
// user-data-bearing by design. Both transports that carry it to the
// browser tab must strip the textsafe invisible set (2026-09-05 round-4
// finding, family F25 Bidi/invisible/confusable characters): the full-page
// <title> element (render.Text escapes markup, not invisible characters)
// and the partial-nav X-Gofastr-Title header, which the client decodes
// straight into document.title (core-ui/runtime/frag/nav.js). A hostile
// document title (or profile/display name a screen echoes) would otherwise
// reorder the tab title ("access" reads as the leading token via an RLO)
// or hide a sentinel codepoint behind a zero-width.
//
// Property: characters that render as nothing (bidi controls U+202E/U+2066,
// zero-width U+200B/U+FEFF) must not reach the browser tab title via either
// title transport.
// Surfaces: framework/uihost handlePage's full-page <title> element,
// framework/uihost handlePartialPage's X-Gofastr-Title emission, and the
// client sink core-ui/runtime/frag/nav.js document.title write
// (mechanically implied by the header, asserted here at the two server
// transports).
func TestTitleTransportsStripInvisibles(t *testing.T) {
	shapes := []struct {
		name string
		r    rune
	}{
		{"RLO U+202E", '\u202e'},
		{"LRI U+2066", '\u2066'},
		{"ZWSP U+200B", '\u200b'},
		{"BOM U+FEFF", '\ufeff'},
	}
	for _, sh := range shapes {
		poisoned := "Admin " + string(sh.r) + "access"

		a := uiapp.NewApp("r4title")
		a.Register("/t", &titleComp{title: poisoned}, nil)
		ds := New(a)

		// Surface 1: full-page <title> element.
		req := httptest.NewRequest(http.MethodGet, "http://localhost/t", nil)
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: full page status %d, want 200", sh.name, w.Code)
		}
		if i := strings.Index(w.Body.String(), "<title>"); i >= 0 {
			rest := w.Body.String()[i:]
			end := strings.Index(rest, "</title>")
			if end >= 0 && strings.ContainsRune(rest[:end], sh.r) {
				t.Errorf("SECURITY: [uihost] %s survives the full-page <title> element: %q — "+
					"render.Text escapes markup but not invisible characters, so a title computed "+
					"from loaded data (a doc title, a display name) reorders or salts the tab title "+
					"the browser shows", sh.name, rest[:end])
			}
		} else {
			t.Fatalf("%s: full page carries no <title> element", sh.name)
		}

		// Surface 2: partial-nav X-Gofastr-Title header (the client's
		// decodeURIComponent(document.title) source).
		preq := httptest.NewRequest(http.MethodGet, "http://localhost/t", nil)
		preq.Header.Set("X-Gofastr-Navigate", "1")
		pw := httptest.NewRecorder()
		ds.ServeHTTP(pw, preq)
		raw := pw.Header().Get("X-Gofastr-Title")
		if raw == "" {
			t.Fatalf("%s: partial response carries no X-Gofastr-Title", sh.name)
		}
		decoded, err := url.PathUnescape(raw)
		if err != nil {
			t.Fatalf("%s: X-Gofastr-Title %q does not decode: %v", sh.name, raw, err)
		}
		if strings.ContainsRune(decoded, sh.r) {
			t.Errorf("SECURITY: [uihost] %s survives X-Gofastr-Title: header %q decodes to %q — "+
				"frag/nav.js writes decodeURIComponent(header) into document.title, so the invisible "+
				"character reaches the tab title verbatim", sh.name, raw, decoded)
		}
	}
}

// titleComp is a screen whose title carries an invisible character, the
// shape a dynamic route produces when it computes its title from loaded
// (user-influenced) data.
type titleComp struct {
	title string
}

func (c *titleComp) ScreenTitle() string { return c.title }

func (c *titleComp) Render() render.HTML {
	return render.HTML("<main>title surface</main>")
}
