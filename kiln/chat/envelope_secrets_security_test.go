package chat

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Pins: no credential value in any session state the chat server
// hands out. serveToolDispatch journals tool-call Args VERBATIM into
// ChatEvent.Call.Args (journaledTools includes set_app_config), so a
// credentialed db_url/jwt_secret parks in sess.Chat; the chat-bearing
// GET surfaces emit session.chat with NO redaction — redactedWorld
// masks only the world-IR arm (the sibling world_dsn red test pins that
// arm; this is the chat-envelope arm).
// Surfaces: kiln/chat/server.go::serveToolDispatch :416-433, serveWorld
// :358-385 ("session":{"chat": sess.Chat} verbatim), serveStatus
// :325-349 (?fields=chat whole slice, ?fields=recent tail).
// Finding (verified by execution): POST /kiln/tool/set_app_config with
// db_url postgres://kiln:dsn-canary@... and auth jwt_secret jwt-canary
// leaves both canaries in /kiln/world (session arm),
// /kiln/status?fields=chat and ?fields=recent. Threat: a local process
// or a DNS-rebinding page (same-Host rebind passes the cross-site gate
// by design) reads the credential in full.
// Fix direction: mask credential-shaped values (dsnHasSecret-style
// db_url, jwt_secret, seed_password) in the journaled tool-call
// envelope — or at the chat-bearing GET emission sites — leaving benign
// config fields (name, api_prefix) visible so the timeline stays
// diagnosable; do not blank the whole Args map.

func TestKilnChatEnvelopeMasksCreds(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-envelope-red")
	if err != nil {
		t.Fatal("setup broken: EphemeralSQLite:", err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal("setup broken: live.New:", err)
	}
	tools := protocol.New(l)
	srv := New(l, tools)
	srv.Mount(l.Aux())

	const dsnCanary = "dsn-canary-0123456789"
	const jwtCanary = "jwt-canary-9876543210"

	// The HTTP dispatcher is the envelope surface: serveToolDispatch
	// journals the args map verbatim before dispatch (protocol.Tools
	// does not journal, so driving it would skip the finding).
	body := `{"config":{"name":"envelope-red","api_prefix":"envred","db_driver":"postgres","db_url":"postgres://kiln:` +
		dsnCanary + `@db.internal:5432/prod","auth":{"enabled":true,"jwt_secret":"` + jwtCanary + `"}}}`
	req := httptest.NewRequest(http.MethodPost, "/kiln/tool/set_app_config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup broken: POST /kiln/tool/set_app_config → %d: %s", rec.Code, rec.Body.String())
	}

	for _, target := range []string{
		"/kiln/world",
		"/kiln/status?fields=chat",
		"/kiln/status?fields=recent",
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		l.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("setup broken: GET %s → %d: %s", target, rec.Code, rec.Body.String())
		}
		got := rec.Body.String()
		if strings.Contains(got, dsnCanary) || strings.Contains(got, jwtCanary) {
			t.Errorf("SECURITY: [kiln-envelope-credential-leak] GET %s handed out the set_app_config credential verbatim (dsn=%s jwt=%s): serveToolDispatch journals tool-call Args into ChatEvent.Call.Args and every chat-bearing GET emits sess.Chat unredacted — redactedWorld covers only the world-IR arm. A local process or a same-Host DNS-rebinding page reads the credential in full: %.600s", target, dsnCanary, jwtCanary, got)
		}
		// Positive control: masking must be credential-shaped, not a
		// blanked envelope — the benign config fields the same tool
		// call carried stay visible so the timeline remains
		// diagnosable.
		if !strings.Contains(got, "envelope-red") || !strings.Contains(got, "envred") {
			t.Errorf("SECURITY: [kiln-envelope-credential-leak] GET %s lost the benign config fields (name envelope-red / api_prefix envred) from the tool-call envelope: the fix must mask credential-shaped values only, not blank Args: %.600s", target, got)
		}
	}
}

// TestKilnChatPanelSummaryMasksCreds is the surface extension of
// kiln-envelope-credential-leak onto the PANEL rendering surfaces: the
// chat-bearing GET legs stay pinned by TestKilnChatEnvelopeMasksCreds,
// so a fix landing on the GET emission flips only its own test.
//
// Property: no credential value reaches the rendered panel rows.
// Surfaces: kiln/chat/panel.go renderChatEvent :807-810 — the tool_call
// row embeds summarizeArgs(e.Call.Args), whose fallback previews the FIRST
// 80 BYTES of the args JSON verbatim; summarizeWorldEdit (:1133) feeds the
// journaled set_app_config payload through the same summarizeArgs.
// serveToolDispatch journals Args verbatim (server.go:416-433), so a
// credentialed db_url / jwt_secret parks in the row preview.
// Finding (probe, round 5): after POST /kiln/tool/set_app_config the
// rendered log shows the 'jwt-canary' and 'dsn-canary' fragments through
// the 80-byte preview. Threat: anyone who can read the panel HTML (local
// process, same-Host rebinding page) reads the credential prefix.
// Fix direction: mask credential-shaped values (dsn-with-password
// db_url, jwt_secret) before previewing args in summarizeArgs /
// summarizeWorldEdit, leaving benign fields (name, api_prefix) visible so
// rows stay diagnosable.
func TestKilnChatPanelSummaryMasksCreds(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-panel-red")
	if err != nil {
		t.Fatal("setup broken: EphemeralSQLite:", err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal("setup broken: live.New:", err)
	}
	tools := protocol.New(l)
	srv := New(l, tools)
	srv.Mount(l.Aux())

	const dsnCanary = "dsn-canary-7f3d"
	const jwtCanary = "jwt-canary-9e2c"

	// Each credentialed config is shaped so its canary lands inside
	// summarizeArgs's 80-byte preview (map keys re-encode sorted:
	// auth first, db_driver/db_url before name).
	post := func(body string) {
		req := httptest.NewRequest(http.MethodPost, "/kiln/tool/set_app_config", strings.NewReader(body))
		rec := httptest.NewRecorder()
		l.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("setup broken: POST /kiln/tool/set_app_config → %d: %s", rec.Code, rec.Body.String())
		}
	}
	post(`{"config":{"auth":{"enabled":true,"jwt_secret":"` + jwtCanary + `"},"name":"panel-red-a"}}`)
	post(`{"config":{"db_driver":"postgres","db_url":"postgres://kiln:` + dsnCanary + `@db.internal:5432/prod","name":"panel-red-b"}}`)
	// Benign control row: its fields must stay visible after any fix.
	post(`{"config":{"name":"panel-red","api_prefix":"panelred"}}`)

	// Render the log exactly as the panel does: chat rows (tool_call
	// envelopes) via renderChatEvent, world_edit rows via
	// summarizeWorldEdit.
	pe := &panelEnv{live: l, tools: tools}
	var rows string
	l.ReadSession(func(sess *journal.Session) { rows = pe.logHTMLForCurrentLocked(sess) })

	if strings.Contains(rows, dsnCanary) || strings.Contains(rows, jwtCanary) {
		t.Errorf("SECURITY: [kiln-panel-credential-leak] the rendered panel rows hand out the set_app_config credential through the args preview (dsn=%s jwt=%s): renderChatEvent embeds summarizeArgs(e.Call.Args) whose fallback previews the first 80 bytes of the journaled args verbatim (panel.go:807-810), and summarizeWorldEdit feeds the journaled set_app_config payload through the same path — mask credential-shaped values before previewing: %.600s", dsnCanary, jwtCanary, rows)
	}
	// Positive control: masking must be credential-shaped, not a blanked
	// row — the benign fields the same tool calls carried stay visible.
	if !strings.Contains(rows, "panel-red") || !strings.Contains(rows, "set_app_config") {
		t.Errorf("SECURITY: [kiln-panel-credential-leak] positive control: benign row fields (tool name set_app_config, config name panel-red) must stay visible in the rendered rows: %.600s", rows)
	}
}
