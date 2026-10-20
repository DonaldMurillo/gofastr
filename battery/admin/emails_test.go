package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/email"
)

// sampleEmails is a welcome mail with both bodies and a text-only reset.
func sampleEmails() []EmailPreview {
	welcome := email.Template{
		Subject:  "Welcome, {{.name}}",
		TextBody: "Hi {{.name}}, thanks for joining.",
		HTMLBody: `<h1>Hi {{.name}}</h1><p>Thanks for joining.</p>`,
	}
	reset := email.Template{
		Subject:  "Reset your password",
		TextBody: "Open {{.link}} to pick a new password.",
	}
	return []EmailPreview{
		{Name: "welcome", Render: func(context.Context) (email.Email, error) {
			return email.Execute(welcome, map[string]any{"name": "Ada <script>"})
		}},
		{Name: "password-reset", Render: func(context.Context) (email.Email, error) {
			return email.Execute(reset, map[string]any{"link": "https://example.test/r/1"})
		}},
	}
}

func TestEmailsPageShowsFirstTemplate(t *testing.T) {
	x := setup(t, nil, Config{EmailPreviews: sampleEmails()}, nil)
	rr := get(x.as(theAdmin), "/admin/emails")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /admin/emails = %d; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		`href="/admin/emails?name=welcome"`,
		`href="/admin/emails?name=password-reset"`,
		"Welcome, Ada &lt;script&gt;",
		"Hi Ada &lt;script&gt;, thanks for joining.",
		`src="/admin/_email/welcome"`,
		`sandbox=""`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "<h1>Hi Ada") {
		t.Error("the HTML body is inlined into the admin page; it belongs in the sandboxed frame")
	}
}

func TestEmailsPageSelectsByQuery(t *testing.T) {
	x := setup(t, nil, Config{EmailPreviews: sampleEmails()}, nil)
	body := get(x.as(theAdmin), "/admin/emails?name=password-reset").Body.String()
	if !strings.Contains(body, "Open https://example.test/r/1 to pick a new password.") {
		t.Error("the selected template's text body is missing")
	}
	if !strings.Contains(body, "This template has no HTML body.") {
		t.Error("a text-only template should say it has no HTML body")
	}
	if strings.Contains(body, `src="/admin/_email/password-reset"`) {
		t.Error("a template with no HTML body should draw no frame")
	}
}

func TestEmailFrameIsSandboxed(t *testing.T) {
	x := setup(t, nil, Config{EmailPreviews: sampleEmails()}, nil)
	rr := get(x.as(theAdmin), "/admin/_email/welcome")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET frame = %d; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "<h1>Hi Ada &lt;script&gt;</h1>") {
		t.Errorf("frame body = %q", rr.Body.String())
	}
	h := rr.Header()
	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{"sandbox", "default-src 'none'", "frame-ancestors 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	if strings.Contains(csp, "script-src") {
		t.Errorf("CSP %q allows scripts", csp)
	}
	if got := h.Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, want SAMEORIGIN", got)
	}
	if got := h.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := h.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestEmailsRefuseNonAdmins(t *testing.T) {
	x := setup(t, nil, Config{EmailPreviews: sampleEmails()}, nil)
	for _, path := range []string{"/admin/emails", "/admin/_email/welcome"} {
		if rr := get(x.as(aReader), path); rr.Code != http.StatusForbidden {
			t.Errorf("reader GET %s = %d, want 403", path, rr.Code)
		}
		rr := get(x.as(nil), path)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("anonymous GET %s = %d, want 401", path, rr.Code)
		}
		if strings.Contains(rr.Body.String(), "Ada") {
			t.Errorf("anonymous GET %s leaked the rendered mail", path)
		}
	}
}

func TestEmailFrameUnknownName404(t *testing.T) {
	x := setup(t, nil, Config{EmailPreviews: sampleEmails()}, nil)
	if rr := get(x.as(theAdmin), "/admin/_email/nope"); rr.Code != http.StatusNotFound {
		t.Errorf("unknown preview = %d, want 404", rr.Code)
	}
}

func TestEmailRenderErrorStaysGeneric(t *testing.T) {
	broken := []EmailPreview{
		{Name: "broken", Render: func(context.Context) (email.Email, error) {
			return email.Email{}, errors.New("db password is hunter2")
		}},
		{Name: "panics", Render: func(context.Context) (email.Email, error) {
			panic("hunter2")
		}},
	}
	x := setup(t, nil, Config{EmailPreviews: broken}, nil)
	for _, name := range []string{"broken", "panics"} {
		body := get(x.as(theAdmin), "/admin/emails?name="+name).Body.String()
		if !strings.Contains(body, "did not render with its sample data") {
			t.Errorf("%s: page lacks the failure notice", name)
		}
		rr := get(x.as(theAdmin), "/admin/_email/"+name)
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("%s: frame = %d, want 500", name, rr.Code)
		}
		if strings.Contains(body+rr.Body.String(), "hunter2") {
			t.Errorf("%s: the error text reached the caller", name)
		}
	}
}

func TestEmailsNavOnlyWithPreviews(t *testing.T) {
	x := setup(t, nil, Config{EmailPreviews: sampleEmails()}, nil)
	if body := get(x.as(theAdmin), "/admin").Body.String(); !strings.Contains(body, `href="/admin/emails"`) {
		t.Error("sidebar lacks the Emails link")
	}
	bare := setup(t, nil, Config{}, nil)
	if body := get(bare.as(theAdmin), "/admin").Body.String(); strings.Contains(body, `href="/admin/emails"`) {
		t.Error("sidebar shows Emails with no previews")
	}
	if rr := get(bare.as(theAdmin), "/admin/emails"); rr.Code != http.StatusNotFound {
		t.Errorf("GET /admin/emails with no previews = %d, want 404", rr.Code)
	}
}

func TestEmailPreviewsConfigRefused(t *testing.T) {
	render := func(context.Context) (email.Email, error) { return email.Email{}, nil }
	for name, previews := range map[string][]EmailPreview{
		"empty name":   {{Name: "", Render: render}},
		"bad name":     {{Name: "Welcome Mail", Render: render}},
		"slash":        {{Name: "a/b", Render: render}},
		"duplicate":    {{Name: "welcome", Render: render}, {Name: "welcome", Render: render}},
		"no render fn": {{Name: "welcome"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := trySetup(t, nil, Config{EmailPreviews: previews}, nil); err == nil {
				t.Error("Init accepted the previews")
			}
		})
	}
}

func TestEmailsPathReservedForPages(t *testing.T) {
	if !reservedPagePath("/emails") {
		t.Error("an app page could take /emails")
	}
}
