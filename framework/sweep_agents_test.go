package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/a2a"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// A push URL naming a private host is refused unless the host set
// A2AConfig.AllowPrivatePush.
func TestSweepAgentsA2APushRefusesPrivateHostByDefault(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow=%v", allow), func(t *testing.T) {
			env := newA2ATestEnv(t, A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}, AllowPrivatePush: allow})
			code, body := env.a2aRequest(t, "tok-alice",
				`{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"m1","role":"ROLE_USER","metadata":{"skill":"echo"},"parts":[{"text":"hi"}]}}}`)
			if code != http.StatusOK {
				t.Fatalf("SendMessage: %d %s", code, body)
			}
			taskID, _ := a2aTask(t, body)["id"].(string)
			if taskID == "" {
				t.Fatalf("no task id in %s", body)
			}
			payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":%q,"params":{"taskId":%q,"url":"https://127.0.0.1:9/cb"}}`,
				a2a.MethodCreateTaskPushNotificationConfig, taskID)
			code, body = env.a2aRequest(t, "tok-alice", payload)
			if code != http.StatusOK {
				t.Fatalf("CreateTaskPushNotificationConfig: %d %s", code, body)
			}
			var resp struct {
				Error *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				t.Fatalf("decode %s: %v", body, err)
			}
			if !allow {
				if resp.Error == nil || resp.Error.Code != a2a.CodeInvalidParams {
					t.Fatalf("SECURITY: [ssrf] a loopback push URL was accepted without AllowPrivatePush: %s", body)
				}
				return
			}
			if resp.Error != nil {
				t.Fatalf("AllowPrivatePush did not admit a loopback push URL: %s", body)
			}
		})
	}
}

func sweepAgentsCardURL(t *testing.T, app *App, req *http.Request) string {
	t.Helper()
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("server card: %d %s", rec.Code, rec.Body.String())
	}
	var card struct {
		Remotes []struct {
			URL string `json:"url"`
		} `json:"remotes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil || len(card.Remotes) == 0 {
		t.Fatalf("decode card %s: %v", rec.Body.String(), err)
	}
	return card.Remotes[0].URL
}

// X-Forwarded-Host never names the origin of a well-known document, even
// from a trusted proxy.
func TestSweepAgentsWellKnownIgnoresForwardedHost(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithMCP(), WithConfig(AppConfig{TrustedProxies: []string{"10.0.0.0/8"}})))
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp/server-card.json", nil)
	req.Host = "app.example.com"
	req.RemoteAddr = "10.1.2.3:4444"
	req.Header.Set("X-Forwarded-Host", "evil.example")
	if got := sweepAgentsCardURL(t, app, req); got != "http://app.example.com/mcp" {
		t.Errorf("SECURITY: [wellknown-cache-poison] card url = %q, want http://app.example.com/mcp", got)
	}
}

// A trusted proxy's X-Forwarded-Proto is honoured only as exactly "http"
// or "https".
func TestSweepAgentsForwardedProtoMustBeExactScheme(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithMCP(), WithConfig(AppConfig{TrustedProxies: []string{"10.0.0.0/8"}})))
	defer cleanup()
	for _, proto := range []string{"https,http", "https://evil.example/p", "HTTPS", "ftp"} {
		req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp/server-card.json", nil)
		req.Host = "app.example.com"
		req.RemoteAddr = "10.1.2.3:4444"
		req.Header.Set("X-Forwarded-Proto", proto)
		if got := sweepAgentsCardURL(t, app, req); got != "http://app.example.com/mcp" {
			t.Errorf("SECURITY: [wellknown-cache-poison] X-Forwarded-Proto %q gave card url %q, want http://app.example.com/mcp", proto, got)
		}
	}
}

func sweepAgentsVary(h http.Header) map[string]bool {
	have := map[string]bool{}
	for _, v := range h.Values("Vary") {
		for _, f := range strings.Split(v, ",") {
			have[strings.ToLower(strings.TrimSpace(f))] = true
		}
	}
	return have
}

// Every JSON well-known document varies on Host and X-Forwarded-Proto,
// and a configured BaseURL removes the Vary with the dependency.
func TestSweepAgentsWellKnownDocsVaryUnlessBaseURL(t *testing.T) {
	paths := []string{"/.well-known/mcp.json", "/.well-known/mcp/catalog.json", "/.well-known/mcp/server-card.json"}
	for _, base := range []string{"", "https://app.example.com"} {
		app, cleanup := startApp(t, NewApp(WithMCP(), WithConfig(AppConfig{BaseURL: base})))
		for _, path := range paths {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			app.router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d", path, rec.Code)
			}
			have := sweepAgentsVary(rec.Header())
			want := base == ""
			if have["host"] != want || have["x-forwarded-proto"] != want {
				t.Errorf("base %q %s: Vary %q, want Host and X-Forwarded-Proto = %v", base, path, rec.Header().Values("Vary"), want)
			}
		}
		cleanup()
	}
}

// A well-known document that cannot encode answers 500, not an empty 200.
func TestSweepAgentsWellKnownEncodeFailureIs500(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithUCP(UCPConfig{Services: []map[string]any{{"bad": func() {}}}})))
	defer cleanup()
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/ucp", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("unencodable /.well-known/ucp = %d %q, want 500", rec.Code, rec.Body.String())
	}
}

func sweepAgentsPanics(f func()) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	f()
	return false
}

// A nil control gate and a role gate with no roles panic at wiring time
// instead of admitting every caller.
func TestSweepAgentsGateConstructorsRefuseEmpty(t *testing.T) {
	if !sweepAgentsPanics(func() { WithMCPControlGate(nil) }) {
		t.Error("SECURITY: [authz] WithMCPControlGate(nil) did not panic")
	}
	if !sweepAgentsPanics(func() { MCPRequireRole() }) {
		t.Error("SECURITY: [authz] MCPRequireRole() with no roles did not panic")
	}
}

// app_routines reports a routine declared for another engine as
// skipped_for_dialect, not missing, and a routine for every engine as
// dialect "all".
func TestSweepAgentsRoutinesSkipOtherDialect(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := NewApp(WithDB(db), WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	app.Routine(migrate.Routine{Name: "every", Up: "DROP VIEW IF EXISTS every;\nCREATE VIEW every AS SELECT 1"})
	app.Routine(migrate.Routine{Name: "pg_only", Up: "CREATE OR REPLACE FUNCTION pg_only() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql", Dialect: migrate.DialectPostgres})
	plan := migrate.Plan{Registry: app.Registry, Routines: app.migrationRoutines}
	if err := migrate.AutoMigratePlanContext(context.Background(), db, plan); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	res, err := app.MCP.CallTool(context.Background(), "app_routines", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	routines, _ := res.(map[string]any)["routines"].([]map[string]any)
	got := map[string]map[string]any{}
	for _, r := range routines {
		got[r["name"].(string)] = r
	}
	if s := got["pg_only"]["ledger_state"]; s != "skipped_for_dialect" {
		t.Errorf("pg_only ledger_state = %v, want skipped_for_dialect", s)
	}
	if d := got["every"]["dialect"]; d != "all" {
		t.Errorf("every dialect = %v, want all", d)
	}
}

// DisableEntitySkills leaves only the hand-written skills on the card.
func TestSweepAgentsA2ADisableEntitySkills(t *testing.T) {
	installA2AOwnerExtractor(t)
	for _, disable := range []bool{false, true} {
		app := NewApp(WithDB(sqliteDB(t)), WithA2A(A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}, DisableEntitySkills: disable}))
		crudTrue := true
		app.Entity("notes", entity.EntityConfig{
			Table:    "notes",
			Fields:   []schema.Field{{Name: "title", Type: schema.String, Required: true}},
			Exposure: &entity.ExposureConfig{CRUD: &crudTrue, MCP: true},
		}.WithTimestamps(false))
		var ids []string
		for _, sk := range app.A2ASkills() {
			ids = append(ids, sk.ID)
		}
		want := "[echo entity.notes]"
		if disable {
			want = "[echo]"
		}
		if got := fmt.Sprint(ids); got != want {
			t.Errorf("DisableEntitySkills=%v: skills %s, want %s", disable, got, want)
		}
	}
}

// The agent_auth skill URL defaults to the resolved origin plus /auth.md.
func TestSweepAgentsAgentAuthSkillDefaultsToOrigin(t *testing.T) {
	app, cleanup := startApp(t, NewApp(
		WithOAuthAuthorizationServer(OAuthAuthorizationServerConfig{Issuer: "https://ex.test"}),
		WithAuthMD(AuthMDConfig{Markdown: "# auth", AgentAuth: &AgentAuthBlock{}}),
	))
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	req.Host = "ex.test"
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, req)
	var doc struct {
		AgentAuth struct {
			Skill string `json:"skill"`
		} `json:"agent_auth"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if doc.AgentAuth.Skill != "http://ex.test/auth.md" {
		t.Errorf("agent_auth.skill = %q, want http://ex.test/auth.md", doc.AgentAuth.Skill)
	}
}
