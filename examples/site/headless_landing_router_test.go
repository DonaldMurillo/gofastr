package main

// Router-level proofs for the newsletter POST handler
// (serveHeadlessSubscribe), the half the browser tests cannot pin
// deterministically: the status codes of every guard (body size, body
// shape, unknown theme carry) and the 303 the no-script path answers.
//
// The error bodies are constants on purpose — request-derived text never
// belongs in a response — and the malformed-body cases pin that too: the
// payload must not come back in the answer.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// serveSubscribe runs one POST against the site's router with a body and
// Content-Type, through the same app the serve helper boots.
func serveSubscribe(t *testing.T, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	app := newTestApp(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, landingSubscribePath, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Host = "localhost:8083" // loopback origin → dev session cookie
	app.Router().ServeHTTP(rec, req)
	return rec
}

func TestHeadlessSubscribeRouterGuards(t *testing.T) {
	big := `{"email":"` + strings.Repeat("a", 5<<10) + `@example.com","theme":"default"}`
	bigForm := url.Values{"email": {strings.Repeat("a", 5<<10)}, "theme": {"default"}}.Encode()

	cases := []struct {
		name        string
		contentType string
		body        string
		wantCode    int
		wantBody    string
		notBody     string
	}{
		{
			"oversized JSON body",
			"application/json",
			big,
			http.StatusRequestEntityTooLarge,
			"request body too large",
			"",
		},
		{
			// Strict decode refuses the unknown key, and its refusal
			// names the key — so a handler concatenating the error
			// would echo request data back.
			"malformed JSON body",
			"application/json",
			`{"email":"a@example.com","theme":"default","evil_extra":"x"}`,
			http.StatusBadRequest,
			"invalid JSON body",
			"evil_extra",
		},
		{
			"oversized form body",
			"application/x-www-form-urlencoded",
			bigForm,
			http.StatusRequestEntityTooLarge,
			"request body too large",
			"",
		},
		{
			"malformed form body",
			"application/x-www-form-urlencoded",
			"email=%zz&theme=default",
			http.StatusBadRequest,
			"invalid form body",
			"%zz",
		},
		{
			"unknown theme carry (JSON)",
			"application/json",
			`{"email":"a@example.com","theme":"retro"}`,
			http.StatusBadRequest,
			"unknown theme",
			"",
		},
		{
			"unknown theme carry (form)",
			"application/x-www-form-urlencoded",
			"email=a%40example.com&theme=retro",
			http.StatusBadRequest,
			"unknown theme",
			"",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := serveSubscribe(t, c.contentType, c.body)
			if rec.Code != c.wantCode {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, c.wantCode, rec.Body.String())
			}
			if got := rec.Body.String(); !strings.Contains(got, c.wantBody) {
				t.Errorf("body %q does not say %q", got, c.wantBody)
			}
			if c.notBody != "" && strings.Contains(rec.Body.String(), c.notBody) {
				t.Errorf("body echoes request data (%q): %s", c.notBody, rec.Body.String())
			}
		})
	}
}

// The island path answers 200 with the re-rendered region: the errors
// ARE the answer, so an invalid address comes back as the form carrying
// the summary, a valid one as the success callout.
func TestHeadlessSubscribeIslandAnswersRegion(t *testing.T) {
	rec := serveSubscribe(t, "application/json", `{"email":"","theme":"default"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty-email island submit: status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "hl-subscribe-summary") {
		t.Errorf("island answer does not carry the error summary: %s", body)
	}

	rec = serveSubscribe(t, "application/json", `{"email":"reader@example.com","theme":"default"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid island submit: status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "hl-subscribe-done") {
		t.Errorf("island answer does not carry the success callout: %s", body)
	}
}

// The no-script path is post-redirect-get: 303 to the landing route with
// the OUTCOME in the query — blank, invalid or ok — never a body of its
// own and never the submitted address, which would land in the reader's
// history and in any referrer a later click sends.
func TestHeadlessSubscribeNoScriptRedirects(t *testing.T) {
	rec := serveSubscribe(t, "application/x-www-form-urlencoded",
		"email=not-an-address&theme=default")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("invalid form submit: status = %d, want 303 (body: %s)", rec.Code, rec.Body.String())
	}
	want := landingRoutePath("default") + "?subscribe=invalid"
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("invalid form submit Location = %q, want %q", got, want)
	}
	if loc := rec.Header().Get("Location"); strings.Contains(loc, "not-an-address") {
		t.Errorf("Location %q carries the submitted address; the outcome travels, the value does not", loc)
	}

	rec = serveSubscribe(t, "application/x-www-form-urlencoded", "email=&theme=default")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("blank form submit: status = %d, want 303", rec.Code)
	}
	if got, want := rec.Header().Get("Location"), landingRoutePath("default")+"?subscribe=blank"; got != want {
		t.Errorf("blank form submit Location = %q, want %q", got, want)
	}

	rec = serveSubscribe(t, "application/x-www-form-urlencoded",
		"email=reader%40example.com&theme=dense")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("valid form submit: status = %d, want 303 (body: %s)", rec.Code, rec.Body.String())
	}
	want = landingRoutePath("dense") + "?subscribe=ok"
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("valid form submit Location = %q, want %q", got, want)
	}
}

// The landing route renders the newsletter region from that query: each
// outcome's own message, and the success callout — through the site's
// own chrome, not a standalone document.
func TestHeadlessLandingRendersSubscribeQuery(t *testing.T) {
	page := body(t, landingRoutePath("default")+"?subscribe=invalid")
	for _, want := range []string{
		`data-hui-form-errors`,
		"hl-subscribe-summary",
		"does not parse as an email",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("invalid-answered landing page lost %q", want)
		}
	}

	// The blank outcome says its own sentence, not the malformed one.
	page = body(t, landingRoutePath("default")+"?subscribe=blank")
	if !strings.Contains(page, "Enter an email address.") {
		t.Error("blank-answered landing page does not say the blank message")
	}

	page = body(t, landingRoutePath("default")+"?subscribe=ok")
	if !strings.Contains(page, "hl-subscribe-done") {
		t.Error("ok-answered landing page lost the success callout")
	}

	// The plain route renders the empty form: no summary, no callout.
	page = body(t, landingRoutePath("default"))
	if strings.Contains(page, "hl-subscribe-summary") || strings.Contains(page, "hl-subscribe-done") {
		t.Error("plain landing page rendered a subscribe answer with no query asking for one")
	}
}
