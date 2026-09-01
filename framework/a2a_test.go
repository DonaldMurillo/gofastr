package framework

// a2a_test.go pins the WithA2A wiring end to end over the real HTTP
// surface: the /a2a mount behind the app router's middleware chain, the
// derived entity skills and their data-part contract, owner scoping on
// the re-dispatched tool call (the exact identity contract
// agent_identity_test.go pins for /mcp), the agent role's forwarding,
// and the call gate reaching A2A tool calls.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/a2a"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// ──────────────────────────────────────────────────────────────────────
// Test doubles
// ──────────────────────────────────────────────────────────────────────

// a2aTestUser is the minimal auth.User the owner extractor reads.
type a2aTestUser struct{ id string }

func (u *a2aTestUser) GetID() string { return u.id }

// a2aTestTokens maps bearer credentials to user ids.
var a2aTestTokens = map[string]string{
	"tok-alice": "u-alice",
	"tok-bob":   "u-bob",
}

// a2aBearerAuth resolves the caller from an Authorization: Bearer header
// the way battery/auth's middlewares do: identity comes from the
// transport, and a request with no resolvable credential is ANONYMOUS —
// the ctx user is shadowed with nil, never inherited from an outer
// layer. Without that shadowing, a re-dispatched in-process request
// would inherit the caller the original request already established and
// no test could ever prove the credential actually rode along.
func a2aBearerAuth(tokens map[string]string) router.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var user any
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				if id, ok := tokens[strings.TrimPrefix(h, "Bearer ")]; ok {
					user = &a2aTestUser{id: id}
				}
			}
			next.ServeHTTP(w, r.WithContext(handler.SetUser(r.Context(), user)))
		})
	}
}

// installA2AOwnerExtractor wires framework/owner against the a2aTestUser
// stored on ctx, mirroring battery/auth's init-time extractor.
func installA2AOwnerExtractor(t *testing.T) {
	t.Helper()
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		raw, ok := handler.GetUser(ctx)
		if !ok || raw == nil {
			return nil, false
		}
		if u, ok := raw.(*a2aTestUser); ok {
			return u.GetID(), true
		}
		return nil, false
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })
}

// a2aEchoSkill is a hand-written skill: echoes text parts into one
// artifact and completes.
func a2aEchoSkill() a2a.Skill {
	return a2a.Skill{
		ID:          "echo",
		Name:        "Echo",
		Description: "Echoes the message's text parts into one artifact.",
		Tags:        []string{"test"},
		Handler: func(ctx context.Context, t a2a.TaskContext) error {
			if err := t.Working(); err != nil {
				return err
			}
			var texts []string
			for _, p := range t.Message().Parts {
				if p.Text != nil {
					texts = append(texts, *p.Text)
				}
			}
			if err := t.Artifact(a2a.Artifact{
				Name:  "echo",
				Parts: []a2a.Part{a2a.TextPart(strings.Join(texts, " "))},
			}, false); err != nil {
				return err
			}
			return t.Complete()
		},
	}
}

// ──────────────────────────────────────────────────────────────────────
// Environment
// ──────────────────────────────────────────────────────────────────────

// a2aTestEnv is a started app with bearer auth, an owner-scoped notes
// entity (CRUD + MCP exposure), and the A2A exchange mounted at /a2a.
type a2aTestEnv struct {
	app  *App
	addr string
	db   *sql.DB
}

func newA2ATestEnv(t *testing.T, cfg A2AConfig, opts ...AppOption) *a2aTestEnv {
	t.Helper()
	installA2AOwnerExtractor(t)
	db := sqliteDB(t)
	r := router.New()
	r.Use(a2aBearerAuth(a2aTestTokens))
	allOpts := append([]AppOption{
		WithDB(db),
		WithoutDefaultMiddleware(),
		WithRouter(r),
		WithA2A(cfg),
	}, opts...)
	app := NewApp(allOpts...)
	crudTrue := true
	app.Entity("notes", entity.EntityConfig{
		Table: "notes",
		Scope: &entity.ScopeConfig{OwnerField: "user_id"},
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "user_id", Type: schema.String},
		},
		Exposure: &entity.ExposureConfig{CRUD: &crudTrue, MCP: true},
	}.WithTimestamps(false))
	addr, _ := startOnRandomPort(t, app)
	return &a2aTestEnv{app: app, addr: addr, db: db}
}

func (e *a2aTestEnv) seedNote(t *testing.T, id, userID, title string) {
	t.Helper()
	if _, err := e.db.Exec(`INSERT INTO notes (id, user_id, title) VALUES (?, ?, ?)`,
		id, userID, title); err != nil {
		t.Fatalf("seed note %s: %v", id, err)
	}
}

// a2aRequest POSTs one JSON-RPC payload to the exchange. bearer may be
// empty for the anonymous posture.
func (e *a2aTestEnv) a2aRequest(t *testing.T, bearer, payload string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+e.addr+"/a2a", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// a2aSendMessage builds a SendMessage params object whose single data
// part carries the entity-skill contract.
func a2aSendMessage(operation string, arguments map[string]any) string {
	data, _ := json.Marshal(map[string]any{
		"skill":     "entity.notes",
		"operation": operation,
		"arguments": arguments,
	})
	msg, _ := json.Marshal(map[string]any{
		"messageId": "m1",
		"role":      "ROLE_USER",
		"parts":     []map[string]any{{"data": json.RawMessage(data)}},
	})
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":%s}}`, msg)
}

// a2aTask decodes the JSON-RPC result's task.
func a2aTask(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var resp struct {
		Result struct {
			Task map[string]any `json:"task"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response %s: %v", body, err)
	}
	if resp.Error != nil {
		t.Fatalf("JSON-RPC error %d: %s (body %s)", resp.Error.Code, resp.Error.Message, body)
	}
	if resp.Result.Task == nil {
		t.Fatalf("no task in result: %s", body)
	}
	return resp.Result.Task
}

func taskState(task map[string]any) string {
	status, _ := task["status"].(map[string]any)
	state, _ := status["state"].(string)
	return state
}

// collectNoteTitles walks a decoded artifact payload gathering every
// object with a "title" (the CRUD list envelope shape is not the
// contract under test; whose rows it holds is).
func collectNoteTitles(v any, out *[]string) {
	switch node := v.(type) {
	case map[string]any:
		if title, ok := node["title"].(string); ok {
			*out = append(*out, title)
		}
		for _, val := range node {
			collectNoteTitles(val, out)
		}
	case []any:
		for _, val := range node {
			collectNoteTitles(val, out)
		}
	case string:
		if trimmed := strings.TrimSpace(node); strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var inner any
			if err := json.Unmarshal([]byte(trimmed), &inner); err == nil {
				collectNoteTitles(inner, out)
			}
		}
	}
}

func artifactTitles(t *testing.T, task map[string]any) []string {
	t.Helper()
	var titles []string
	arts, _ := task["artifacts"].([]any)
	for _, a := range arts {
		art, _ := a.(map[string]any)
		parts, _ := art["parts"].([]any)
		for _, p := range parts {
			var decoded any
			switch part := p.(type) {
			case map[string]any:
				decoded = part
			case string: // a text part embedding JSON
				if err := json.Unmarshal([]byte(part), &decoded); err != nil {
					continue
				}
			}
			collectNoteTitles(decoded, &titles)
		}
	}
	return titles
}

// ──────────────────────────────────────────────────────────────────────
// The mount
// ──────────────────────────────────────────────────────────────────────

// WithA2A mounts /a2a: a SendMessage to a hand-written skill returns a
// COMPLETED task with the artifact; GET is refused with 405; a caller
// with no principal gets HTTP 401 and JSON-RPC -31401.
func TestA2A_MountsExchange(t *testing.T) {
	env := newA2ATestEnv(t, A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}})
	env.seedNote(t, "n1", "u-alice", "alice note")

	code, body := env.a2aRequest(t, "tok-alice",
		`{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"m1","role":"ROLE_USER","metadata":{"skill":"echo"},"parts":[{"text":"hello exchange"}]}}}`)
	if code != http.StatusOK {
		t.Fatalf("SendMessage: got %d body %s", code, body)
	}
	task := a2aTask(t, body)
	if got := taskState(task); got != "TASK_STATE_COMPLETED" {
		t.Fatalf("task state = %s, want TASK_STATE_COMPLETED (body %s)", got, body)
	}
	arts, _ := task["artifacts"].([]any)
	if len(arts) != 1 {
		t.Fatalf("artifacts = %d, want 1 (body %s)", len(arts), body)
	}

	// GET → 405 (POST is the only transport method).
	resp, err := http.Get("http://" + env.addr + "/a2a")
	if err != nil {
		t.Fatalf("GET /a2a: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /a2a = %d, want 405", resp.StatusCode)
	}

	// No principal → 401 with the A2A auth code, before any method runs.
	code, body = env.a2aRequest(t, "", `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"role":"ROLE_USER","parts":[{"text":"x"}]}}}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous SendMessage = %d, want 401 (body %s)", code, body)
	}
	var respErr struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &respErr); err != nil {
		t.Fatalf("decode 401 body %s: %v", body, err)
	}
	if respErr.Error.Code != a2a.CodeUnauthenticated {
		t.Fatalf("anonymous error code = %d, want %d (-31401)", respErr.Error.Code, a2a.CodeUnauthenticated)
	}
}

// ──────────────────────────────────────────────────────────────────────
// Entity skills: the data-part contract and owner scoping
// ──────────────────────────────────────────────────────────────────────

// The derived entity.notes skill runs the entity's MCP tools with the
// caller's credentials: alice's list returns exactly alice's rows, and
// an unknown operation is REJECTED naming the operations.
func TestA2A_EntitySkillOwnerScopedRoundTrip(t *testing.T) {
	env := newA2ATestEnv(t, A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}})
	env.seedNote(t, "n1", "u-alice", "alice first")
	env.seedNote(t, "n2", "u-alice", "alice second")
	env.seedNote(t, "n3", "u-bob", "bob only")

	code, body := env.a2aRequest(t, "tok-alice", a2aSendMessage("list", map[string]any{}))
	if code != http.StatusOK {
		t.Fatalf("SendMessage list: got %d body %s", code, body)
	}
	task := a2aTask(t, body)
	if got := taskState(task); got != "TASK_STATE_COMPLETED" {
		t.Fatalf("task state = %s, want TASK_STATE_COMPLETED (body %s)", got, body)
	}
	titles := artifactTitles(t, task)
	if len(titles) != 2 || !titleSet(titles)["alice first"] || !titleSet(titles)["alice second"] {
		t.Fatalf("SECURITY: alice's A2A list returned %v, want exactly her 2 rows (bob's row must be invisible)", titles)
	}

	// Unknown operation → REJECTED with the operations named.
	code, body = env.a2aRequest(t, "tok-alice", a2aSendMessage("purge", map[string]any{}))
	if code != http.StatusOK {
		t.Fatalf("SendMessage purge: got %d body %s", code, body)
	}
	task = a2aTask(t, body)
	if got := taskState(task); got != "TASK_STATE_REJECTED" {
		t.Fatalf("unknown operation state = %s, want TASK_STATE_REJECTED (body %s)", got, body)
	}
	if !strings.Contains(string(body), "list") || !strings.Contains(string(body), "get") {
		t.Fatalf("rejection must name the operations: %s", body)
	}
}

// titleSet renders titles as a set for membership checks.
func titleSet(titles []string) map[string]bool {
	set := make(map[string]bool, len(titles))
	for _, s := range titles {
		set[s] = true
	}
	return set
}

// The mutation guard's downstream precondition: an in-process MCP call
// with no request on its context is anonymous, and owner scoping must
// refuse it. This is exactly the call the entity-skill handler makes
// when the mcp.WithRequest wrap is deleted from framework/a2a.go, which
// is how the round-trip guard above is proven red.
func TestA2A_EntityToolCallFailsAnonymous(t *testing.T) {
	env := newA2ATestEnv(t, A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}})
	env.seedNote(t, "n1", "u-alice", "alice only")
	if _, err := env.app.MCP.CallTool(context.Background(), "notes_list", map[string]any{}); err == nil {
		t.Fatal("anonymous in-process notes_list succeeded; owner scoping is not fail-closed on the re-dispatch path")
	}
}

// ──────────────────────────────────────────────────────────────────────
// Skills for the card
// ──────────────────────────────────────────────────────────────────────

// A2ASkills returns the hand-written and the derived entity skills,
// sorted by id; nil when WithA2A was never called.
func TestA2A_SkillsForCard(t *testing.T) {
	installA2AOwnerExtractor(t)
	db := sqliteDB(t)
	app := NewApp(WithDB(db), WithA2A(A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}}))
	crudTrue := true
	app.Entity("notes", entity.EntityConfig{
		Table:    "notes",
		Fields:   []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		Exposure: &entity.ExposureConfig{CRUD: &crudTrue, MCP: true},
	}.WithTimestamps(false))

	skills := app.A2ASkills()
	var ids []string
	for _, sk := range skills {
		ids = append(ids, sk.ID)
	}
	want := []string{"echo", "entity.notes"}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("skill ids = %v, want %v (sorted by id)", ids, want)
	}
	for _, sk := range skills {
		if sk.ID == "entity.notes" {
			if sk.Name != "Notes records" {
				t.Errorf("entity skill name = %q, want %q", sk.Name, "Notes records")
			}
			// The description is the data-part contract: one line per
			// operation, with the tool's input-schema property keys.
			for _, op := range []string{"list", "get", "create", "update", "delete"} {
				if !strings.Contains(sk.Description, "\n"+op+"(") {
					t.Errorf("description missing %s line: %q", op, sk.Description)
				}
			}
			if len(sk.Examples) == 0 || !strings.Contains(sk.Examples[0], `"operation":"list"`) {
				t.Errorf("examples must include a list data part: %v", sk.Examples)
			}
		}
	}

	if got := NewApp().A2ASkills(); got != nil {
		t.Fatalf("A2ASkills without WithA2A = %v, want nil", got)
	}
}

// ──────────────────────────────────────────────────────────────────────
// Agent role
// ──────────────────────────────────────────────────────────────────────

// RoleAgent serves POST /a2a (a skill call completes through the
// forwarded router and its middleware) and does NOT serve entity REST
// routes, mirroring TestAgentRole_EntityRouteNotServed.
func TestA2A_AgentRoleServesExchange(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	env := newA2ATestEnv(t, A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}}, WithRole(RoleAgent))

	code, body := env.a2aRequest(t, "tok-alice",
		`{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"m1","role":"ROLE_USER","metadata":{"skill":"echo"},"parts":[{"text":"over the agent role"}]}}}`)
	if code != http.StatusOK {
		t.Fatalf("SendMessage on agent role: got %d body %s", code, body)
	}
	if task := a2aTask(t, body); taskState(task) != "TASK_STATE_COMPLETED" {
		t.Fatalf("agent role task state = %s (body %s)", taskState(task), body)
	}

	resp, err := http.Get("http://" + env.addr + "/api/notes")
	if err != nil {
		t.Fatalf("GET /api/notes: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/api/notes on agent role = %d, want 404 (entity routes must not be served)", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────────────
// The call gate
// ──────────────────────────────────────────────────────────────────────

// A tool the server's call gate hides (a disabled module's) is refused
// through A2A too: the entity handler dispatches via a.MCP.CallTool,
// which runs the gate, and the task ends FAILED with the gate's refusal
// instead of returning rows. The gate is installed AFTER Start so the
// skill was built with the tool visible (the pre-Start case is hiding:
// ListTools drops it and the operation is never advertised).
func TestA2A_GatedToolRefused(t *testing.T) {
	env := newA2ATestEnv(t, A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}})
	env.seedNote(t, "n1", "u-alice", "alice only")

	env.app.MCP.SetCallGate(func(name string) error {
		if name == "notes_list" {
			return fmt.Errorf("module disabled")
		}
		return nil
	})
	t.Cleanup(func() { env.app.MCP.SetCallGate(nil) })

	code, body := env.a2aRequest(t, "tok-alice", a2aSendMessage("list", map[string]any{}))
	if code != http.StatusOK {
		t.Fatalf("SendMessage: got %d body %s", code, body)
	}
	task := a2aTask(t, body)
	if got := taskState(task); got != "TASK_STATE_FAILED" {
		t.Fatalf("gated tool task state = %s, want TASK_STATE_FAILED (body %s)", got, body)
	}
	if !strings.Contains(string(body), "tool unavailable") {
		t.Fatalf("task must carry the gate's refusal: %s", body)
	}
}
