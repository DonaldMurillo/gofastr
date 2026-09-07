//go:build red

package main

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: every write operation on the lms lessons entity is permission-
// gated exactly as the entity's own comment promises ("every write still
// requires the permission below") — including create.
// Surfaces: examples/lms/gofastr.yml:258-261 lessons access block — `read:`
// blank, `update: catalog:write`, `delete: catalog:admin`, NO `create:` key.
// Sibling entities courses (:90-94) and modules (:207-211) both gate create
// with catalog:write. The omitted key decodes to "" (blueprint.go::
// decodeEntityAccess :1344-1347) and requirePermission treats "" as ungated
// (crud/owner.go::permissionForOp :28), so the one write the author forgot
// is world-open on both surfaces.
// Finding (verified by execution): anonymous POST /api/lessons
// {"title":"anon","module_id":<id>,"status":"published","lesson_type":"text"}
// = 201 with the row persisted (and, being published, served straight into
// the anonymous outline); anonymous MCP tools/call lessons_create also
// creates a published row with no session, while courses_create answers
// 403 and PATCH/DELETE on lessons answer 403.
// Fix direction: examples/lms/gofastr.yml lessons access block gains
// `create: catalog:write`. NOT a blueprint-validator change: intentional
// lead-capture entities omit create too, so omission is a legitimate idiom
// the validator must keep allowing.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// TestLmsLessonsRedCreateGated boots the real lms blueprint and asserts the
// lessons entity refuses create without catalog:write on all three arms:
// anonymous REST, registered role-less REST, and anonymous MCP tools/call.
// Contrast legs (courses create 403, lessons PATCH/DELETE 403, modules GET
// 200) pin the premise that only the lessons create gate is missing, and
// stay green once the access block gains create: catalog:write.
func TestLmsLessonsRedCreateGated(t *testing.T) {
	if testing.Short() {
		t.Skip("generates, builds, and boots an app")
	}
	bin, appDir := generateAndCompileBlueprint(t, "../../examples/lms/gofastr.yml", "lms")
	baseURL, appOut := bootGeneratedApp(t, "lms", bin, appDir)

	registerAndLogin := func(email string) *http.Client {
		t.Helper()
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal("setup broken: cookie jar:", err)
		}
		client := &http.Client{Jar: jar}
		creds := fmt.Sprintf(`{"email":%q,"password":"str0ng-passphrase"}`, email)
		for _, path := range []string{"/auth/register", "/auth/login"} {
			resp, err := client.Post(baseURL+path, "application/json", strings.NewReader(creds))
			if err != nil {
				t.Fatalf("setup broken: %s %s: %v", path, email, err)
			}
			out, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
				t.Fatalf("setup broken: %s %s = %d: %s\napp log:\n%s", path, email, resp.StatusCode, out, appOut.String())
			}
		}
		return client
	}

	api := func(client *http.Client, method, path, body string) (int, string) {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, baseURL+path, rd)
		if err != nil {
			t.Fatalf("setup broken: %s %s: build request: %v", method, path, err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("setup broken: %s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}

	// module_id the lessons FK demands, and a seeded lesson row for the
	// PATCH/DELETE contrast, read straight from the boot DB the harness
	// assigns — DATABASE_URL=file:<appDir>/boot-gate.db.
	bootDB, err := sql.Open("sqlite3", "file:"+filepath.Join(appDir, "boot-gate.db")+"?mode=ro")
	if err != nil {
		t.Fatalf("setup broken: open boot db: %v", err)
	}
	t.Cleanup(func() { _ = bootDB.Close() })
	seedID := func(table string) string {
		t.Helper()
		var id string
		if err := bootDB.QueryRow("SELECT id FROM " + table + " LIMIT 1").Scan(&id); err != nil {
			t.Fatalf("setup broken: seed id for %s: %v (migrations/seeds changed?)", table, err)
		}
		return id
	}
	moduleID, lessonID := seedID("modules"), seedID("lessons")

	lessonBody := func(title string) string {
		return fmt.Sprintf(`{"title":%q,"module_id":%q,"lesson_type":"text","status":"published"}`, title, moduleID)
	}

	anon := &http.Client{} // no jar: zero credentials

	// Leg 1: anonymous REST create must be refused AND leave no row.
	const anonTitle = "red-anon-lesson"
	code, raw := api(anon, http.MethodPost, "/api/lessons", lessonBody(anonTitle))
	if code < http.StatusUnauthorized {
		t.Errorf("SECURITY: [lessons-create-ungated] anonymous POST /api/lessons = %d (%s): the lessons access block omits create:, the missing key decodes to \"\" and requirePermission treats \"\" as ungated, so the one write the author forgot is world-open while update: catalog:write and delete: catalog:admin gate their ops (courses and modules gate create with catalog:write). Want 401/403", code, truncate(raw, 200))
	}
	if gcode, grow := api(anon, http.MethodGet, "/api/lessons", ""); gcode == http.StatusOK && strings.Contains(grow, anonTitle) {
		t.Errorf("SECURITY: [lessons-create-ungated] the anonymous lesson row persisted and is served in the public outline (GET /api/lessons contains %q): a published lesson any network peer injected. Want no row", anonTitle)
	}

	// Leg 2: a registered, role-less account is equally refused.
	stranger := registerAndLogin("stranger@learnhub.test")
	code, raw = api(stranger, http.MethodPost, "/api/lessons", lessonBody("red-stranger-lesson"))
	if code < http.StatusUnauthorized {
		t.Errorf("SECURITY: [lessons-create-ungated] registered role-less account POST /api/lessons = %d (%s): any signed-in stranger can publish lessons because create: is missing from the access block. Want 403", code, truncate(raw, 200))
	}

	// Leg 3: the MCP arm must inherit the same gate. Stateless JSON-RPC
	// over POST /mcp: initialize, then tools/call with no session cookie.
	initPayload := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"lessons-red","version":"0"}}}`
	icode, ibody := postMCP(t, baseURL, initPayload)
	if icode != http.StatusOK || !strings.Contains(ibody, `"result"`) {
		t.Fatalf("setup broken: POST /mcp initialize = %d: %s\napp log:\n%s", icode, ibody, appOut.String())
	}
	const mcpTitle = "red-mcp-lesson"
	callPayload := fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"lessons_create","arguments":{"title":%q,"module_id":%q,"lesson_type":"text","status":"published"}}}`, mcpTitle, moduleID)
	ccode, cbody := postMCP(t, baseURL, callPayload)
	if ccode != http.StatusOK {
		t.Fatalf("setup broken: POST /mcp tools/call lessons_create = HTTP %d, want 200 (JSON-RPC reports its own errors in the body): %s\napp log:\n%s", ccode, cbody, appOut.String())
	}
	var env struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result struct {
			Content []struct {
				Text    string `json:"text"`
				IsError bool   `json:"isError"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(cbody), &env); err != nil {
		t.Fatalf("setup broken: tools/call lessons_create response is not JSON: %v\n%s", err, cbody)
	}
	failure := ""
	if env.Error != nil {
		failure = env.Error.Message
	}
	for _, c := range env.Result.Content {
		if c.IsError {
			failure += " " + c.Text
		}
	}
	// Classify on the authorization signal, not on "an error happened",
	// so a payload/schema drift can't read as "gated" and pass vacuously.
	refused := strings.Contains(failure, "status 401") ||
		strings.Contains(failure, "status 403") ||
		strings.Contains(failure, "authentication required") ||
		strings.Contains(failure, "missing permission") ||
		strings.Contains(failure, "access denied")
	if failure != "" && !refused {
		t.Fatalf("setup broken: anonymous tools/call lessons_create failed for a non-authorization reason (arguments/schema drift?): %s\n%s", failure, cbody)
	}
	if !refused {
		t.Errorf("SECURITY: [lessons-create-ungated] anonymous MCP tools/call lessons_create succeeded (%s): the tool re-enters the same ungated create route, so an agent with no session publishes a lesson while courses_create answers 403. Want an authorization error", truncate(cbody, 200))
	}
	if gcode, grow := api(anon, http.MethodGet, "/api/lessons", ""); gcode == http.StatusOK && strings.Contains(grow, mcpTitle) {
		t.Errorf("SECURITY: [lessons-create-ungated] the MCP-created lesson row persisted into the public outline (GET /api/lessons contains %q). Want no row", mcpTitle)
	}

	// Contrast legs — the premise this red test is precise about. These
	// are gated/open exactly as the blueprint says, before AND after the
	// fix; if one drifts, the premise broke, not the finding.
	code, raw = api(anon, http.MethodPost, "/api/courses", fmt.Sprintf(`{"title":"red-contrast","topic":"x","level":"beginner","price":0,"status":"published"}`))
	if code != http.StatusUnauthorized && code != http.StatusForbidden {
		t.Fatalf("setup broken: anonymous POST /api/courses = %d (%s): courses is supposed to gate create with catalog:write — if this drifted, the contrast premise is stale", code, truncate(raw, 200))
	}
	for _, leg := range []struct {
		method string
		path   string
		body   string
		what   string
	}{
		{http.MethodPatch, "/api/lessons/" + lessonID, `{"title":"red-patch"}`, "PATCH"},
		{http.MethodDelete, "/api/lessons/" + lessonID, "", "DELETE"},
	} {
		if code, raw = api(anon, leg.method, leg.path, leg.body); code != http.StatusUnauthorized && code != http.StatusForbidden {
			t.Fatalf("setup broken: anonymous %s /api/lessons/<seeded> = %d (%s): update/delete are gated by the same access block — if this drifted, the contrast premise is stale", leg.what, code, truncate(raw, 200))
		}
	}
	if code, raw = api(anon, http.MethodGet, "/api/modules", ""); code != http.StatusOK {
		t.Fatalf("setup broken: anonymous GET /api/modules = %d (%s): the public outline read the finding's severity rests on is down — app or route drift, not the finding", code, truncate(raw, 200))
	}
}
