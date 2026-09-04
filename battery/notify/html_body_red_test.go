//go:build red

package notify_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/notify"
)

// CONTRACT-QUESTION red: the maintainer must decide who owns HTML escaping in
// the notify pipeline. MapTemplater.Render's doc explicitly disclaims it —
// "TextBody / HTMLBody are not modified, those are payload bytes and any
// HTML safety is the rendering layer's job" (notify.go) — but MapTemplater
// IS the rendering layer the package ships, and the downstream EmailChannel
// passes Rendered.HTMLBody straight into email.Email.HTMLBody. The sibling
// primitive battery/email.Execute renders its HTMLBody with html/template
// (contextual autoescape), so the batteries disagree on where the obligation
// lands. If the answer is "host templates must pre-escape", delete this test
// and strengthen the MapTemplater doc + the EmailChannel doc to say so; if
// the answer is "the framework escapes at the HTML sink", MapTemplater needs
// html/template or an escape pass.

// RED TEST — open finding, 2026-09-04 adversarial pass round 3 (tests-only; no fix applied).
// Family: F9 Output encoding at non-HTML sinks (user data interpolated into
// HTML email bodies unescaped)
// Property: a user-controlled value interpolated into an HTML email body is
// escaped before the body reaches the sender — no layer in the pipeline may
// hand raw attacker bytes to the HTML body sink.
// Surfaces: notify.go::MapTemplater.Render (interpolate on HTMLBody with no
// escaping) feeding notify.go::EmailChannel.Send (Rendered.HTMLBody →
// email.Email.HTMLBody verbatim). The Data map is host-supplied at the API
// level, but its values are routinely USER-controlled (display names,
// addresses, user-entered content in the notification payload), and the
// package's own doc example interpolates {{name}}.
// Finding: interpolate() is a raw {{placeholder}} string substitution, and
// Render applies it to HTMLBody unchanged. Data{name: "<script>..."} renders
// straight into the HTML body that EmailChannel hands the SMTP sender:
// stored/reflected XSS in whatever MUA renders the message (webmail is an
// HTML application). battery/email.Execute proves the sibling posture — it
// renders HTMLBody through html/template's contextual autoescape — so the
// escaping obligation currently falls in the gap between the two batteries.
// Severity: medium — user data in transactional HTML email is the normal
// case, webmail XSS is a session-theft-class sink, but the exploit requires
// a host template that interpolates user data into HTMLBody, which the doc
// example itself models.
// Fix direction: render HTMLBody via html/template (like battery/email.
// Execute) or escape interpolated values at the HTML sink in EmailChannel;
// alternatively document the host obligation on BOTH MapTemplater.Render and
// EmailChannel if the raw pass-through is intentional.

// TestMapTemplaterEscapesHTMLBody renders a user-controlled value into an
// HTML email body template and asserts it cannot arrive at the sender as
// live markup.
func TestMapTemplaterEscapesHTMLBody(t *testing.T) {
	tmpl := notify.NewMapTemplater()
	tmpl.Set("welcome", "email", notify.Template{
		Subject:  "Welcome",
		TextBody: "Hi {{name}}",
		HTMLBody: "<p>Welcome, {{name}}!</p>",
	})

	r, err := tmpl.Render(context.Background(), "welcome", "email",
		map[string]any{"name": `<script>alert("pwned")</script>`})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(r.HTMLBody, "<script>") {
		t.Errorf("SECURITY: [notify-html] MapTemplater interpolated a user-controlled value into "+
			"HTMLBody unescaped (rendered %q): EmailChannel hands this straight to the SMTP HTML part, "+
			"and battery/email.Execute autoescapes the same shape via html/template — the escaping "+
			"obligation currently falls between the two batteries. CONTRACT-QUESTION: maintainer decides "+
			"framework-escapes-at-sink versus documented host obligation.",
			r.HTMLBody)
	}
}
