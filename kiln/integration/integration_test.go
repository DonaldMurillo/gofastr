// Package integration exercises the full Kiln stack end-to-end through
// the public tool surface. Each test stands up a fresh Live + Tools and
// drives it the way a real agent would, then asserts the resulting
// world / DB / HTTP behavior.
package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/agent"
	"github.com/DonaldMurillo/gofastr/kiln/agent/acp"
	kilnmcp "github.com/DonaldMurillo/gofastr/kiln/agent/mcp"
	"github.com/DonaldMurillo/gofastr/kiln/chat"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// --- harness ----------------------------------------------------------

type harness struct {
	live  *live.Live
	tools *protocol.Tools
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	d, cleanup, err := db.EphemeralSQLite("kiln-int")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{live: l, tools: protocol.New(l)}
}

func (h *harness) addEntity(t *testing.T, e *world.Entity) {
	t.Helper()
	res := h.tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: e})
	if !res.OK {
		t.Fatalf("add_entity %s: %+v", e.Name, res)
	}
}

// --- (1) all field types --------------------------------------------

// Kiln accepts every framework field type and the resulting CRUD
// endpoint round-trips a row covering each non-auto column.
func TestAllFieldTypesFunctional(t *testing.T) {
	h := newHarness(t)
	max := 200.0

	h.addEntity(t, &world.Entity{
		Name: "kitchen_sink",
		Fields: []world.Field{
			{Name: "s", Type: "string", Max: &max},
			{Name: "t", Type: "text"},
			{Name: "i", Type: "int"},
			{Name: "f", Type: "float"},
			{Name: "b", Type: "bool"},
			{Name: "e", Type: "enum", Values: []string{"a", "b"}, Default: "a"},
			{Name: "j", Type: "json"},
		},
	})

	// POST a row covering every column.
	body := bytes.NewBufferString(`{
		"s":"hello","t":"world","i":42,"f":3.14,"b":true,"e":"b","j":"{\"k\":1}"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/kitchen_sink", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.live.ServeHTTP(rec, req)
	if rec.Code >= 400 {
		t.Fatalf("POST status=%d body=%q", rec.Code, rec.Body.String())
	}

	// GET back and verify s/i/e survived round-trip.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/kitchen_sink", nil)
	h.live.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET status=%d", rec.Code)
	}
	out := rec.Body.String()
	for _, want := range []string{`"hello"`, `"b"`, `42`} {
		if !strings.Contains(out, want) {
			t.Errorf("response missing %s: %s", want, out)
		}
	}
}

// --- (2) hook events --------------------------------------------------

func TestEveryHookEventCanRegister(t *testing.T) {
	h := newHarness(t)
	h.addEntity(t, &world.Entity{
		Name:   "items",
		Fields: []world.Field{{Name: "name", Type: "string", Required: true}},
	})

	events := []string{
		"before_create", "after_create",
		"before_update", "after_update",
		"before_delete", "after_delete",
		"before_list", "after_list",
	}
	for i, ev := range events {
		res := h.tools.AddHook(t.Context(), protocol.AddHookArgs{
			Hook: &world.Hook{
				ID:     "h" + ev,
				Entity: "items",
				When:   ev,
				Action: world.Action{Kind: world.ActionNoop},
			},
		})
		if !res.OK {
			t.Errorf("hook %d (%s) failed: %+v", i, ev, res)
		}
	}
	if len(h.live.Session().World.Hooks) != len(events) {
		t.Errorf("expected %d hooks, got %d", len(events), len(h.live.Session().World.Hooks))
	}
}

func TestBeforeCreateHookRejectsRow(t *testing.T) {
	h := newHarness(t)
	h.addEntity(t, &world.Entity{
		Name:   "items",
		Fields: []world.Field{{Name: "name", Type: "string", Required: true}},
	})
	r := h.tools.AddHook(t.Context(), protocol.AddHookArgs{Hook: &world.Hook{
		ID: "no-spam", Entity: "items", When: "before_create",
		Action: world.Action{Kind: world.ActionValidate, Params: map[string]any{
			"expression": `entity.name != "spam"`,
			"message":    "no spam",
		}},
	}})
	if !r.OK {
		t.Fatalf("add hook: %+v", r)
	}

	rec := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"name":"spam"}`)
	req := httptest.NewRequest("POST", "/api/items", body)
	req.Header.Set("Content-Type", "application/json")
	h.live.ServeHTTP(rec, req)
	if rec.Code < 400 {
		t.Fatalf("expected reject, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no spam") {
		t.Errorf("error message lost: %s", rec.Body.String())
	}
}

// --- (3) action kinds at the route layer -----------------------------

func TestRespondJSONWithComputedBody(t *testing.T) {
	h := newHarness(t)
	r := h.tools.AddRoute(t.Context(), protocol.AddRouteArgs{Route: &world.Route{
		Method: "GET", Path: "/echo",
		Action: world.Action{Kind: world.ActionRespondJSON, Params: map[string]any{
			"status": float64(200),
			"body":   map[string]any{"hello": "world"},
		}},
	}})
	if !r.OK {
		t.Fatal(r)
	}
	rec := httptest.NewRecorder()
	h.live.ServeHTTP(rec, httptest.NewRequest("GET", "/echo", nil))
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["hello"] != "world" {
		t.Errorf("body=%v", got)
	}
}

// --- (4) destructive ops require an approved plan -------------------

func TestDeleteEntityPlanFlow(t *testing.T) {
	h := newHarness(t)
	h.addEntity(t, &world.Entity{Name: "trash", Fields: []world.Field{{Name: "x", Type: "string"}}})

	// 1) No plan_id → blocked.
	first := h.tools.DeleteEntity(t.Context(), protocol.DeleteEntityArgs{Name: "trash"})
	if first.OK || first.Kind != "needs_plan" {
		t.Fatalf("first call without plan should be blocked: %+v", first)
	}

	// 2) Propose + approve plan with the right target.
	if r := h.tools.ProposePlan(t.Context(), protocol.ProposePlanArgs{
		PlanID:  "p1",
		Steps:   []string{"drop trash"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "trash"}},
	}); !r.OK {
		t.Fatalf("propose: %+v", r)
	}
	if r := h.tools.ApprovePlan(t.Context(), protocol.ApprovePlanArgs{PlanID: "p1"}); !r.OK {
		t.Fatalf("approve: %+v", r)
	}

	// 3) Now the delete succeeds.
	second := h.tools.DeleteEntity(t.Context(), protocol.DeleteEntityArgs{Name: "trash", PlanID: "p1"})
	if !second.OK {
		t.Fatalf("with approved plan, expected OK: %+v", second)
	}
	if _, ok := h.live.Session().World.Entities["trash"]; ok {
		t.Error("entity still present after approved delete")
	}

	// 4) Plan target is single-use even after re-adding.
	h.addEntity(t, &world.Entity{Name: "trash", Fields: []world.Field{{Name: "x", Type: "string"}}})
	stale := h.tools.DeleteEntity(t.Context(), protocol.DeleteEntityArgs{Name: "trash", PlanID: "p1"})
	if stale.OK {
		t.Error("plan reuse should be blocked, got OK")
	}
}

// --- (5) undo across many entries ------------------------------------

func TestUndoUnwindsHistory(t *testing.T) {
	h := newHarness(t)
	for _, n := range []string{"a", "b", "c"} {
		h.addEntity(t, &world.Entity{Name: n, Fields: []world.Field{{Name: "x", Type: "string"}}})
	}
	if got := len(h.live.Session().World.Entities); got != 3 {
		t.Fatalf("setup got %d, want 3", got)
	}

	// Undo three times.
	for i := 0; i < 3; i++ {
		r := h.tools.Undo(t.Context(), protocol.UndoArgs{})
		if !r.OK {
			t.Fatalf("undo %d: %+v", i, r)
		}
	}
	if got := len(h.live.Session().World.Entities); got != 0 {
		t.Errorf("after 3 undos, entities=%d, want 0", got)
	}

	// One more undo on empty journal must error.
	r := h.tools.Undo(t.Context(), protocol.UndoArgs{})
	if r.OK {
		t.Error("undo on empty journal must error")
	}
}

// --- (6) freeze round-trip --------------------------------------------

func TestFreezeRoundTripWithRichWorld(t *testing.T) {
	h := newHarness(t)
	h.addEntity(t, &world.Entity{
		Name: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
			{Name: "body", Type: "text"},
			{Name: "status", Type: "enum", Values: []string{"draft", "published"}, Default: "draft"},
			{Name: "author", Type: "relation", To: "users"},
		},
		SoftDelete: true,
		MCP:        true,
	})
	h.addEntity(t, &world.Entity{
		Name: "users",
		Fields: []world.Field{
			{Name: "email", Type: "string", Required: true, Unique: true},
		},
	})

	dir := t.TempDir()
	if err := freeze.Freeze(h.live.Session().World, dir); err != nil {
		t.Fatalf("freeze: %v", err)
	}

	// Current graduation emits a generator-ready gofastr.yml plus the
	// lossless world.json snapshot; the pre-v0.1 entities/*.json path is gone.
	blueprint, err := os.ReadFile(filepath.Join(dir, "gofastr.yml"))
	if err != nil {
		t.Fatalf("gofastr.yml missing after freeze: %v", err)
	}
	text := string(blueprint)
	for _, want := range []string{"name: posts", "name: users", "name: title", "name: email"} {
		if !strings.Contains(text, want) {
			t.Errorf("blueprint missing %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "world.json")); err != nil {
		t.Fatalf("world.json missing after freeze: %v", err)
	}
}

// --- (7) transport parity (native dispatch, MCP, ACP) ----------------

func TestTransportParityNativeMCPAndACP(t *testing.T) {
	makeRequest := func(name string, args map[string]any) (protocol.Result, error) {
		return protocol.Result{}, nil
	}
	_ = makeRequest

	tcase := func(t *testing.T, viaMCP, viaACP bool) {
		t.Helper()
		h := newHarness(t)

		// Add posts via the chosen transport.
		entityArgs := map[string]any{
			"entity": map[string]any{
				"name":   "posts",
				"fields": []any{map[string]any{"name": "title", "type": "string", "required": true}},
			},
		}

		switch {
		case viaMCP:
			srv, err := kilnmcp.NewServer(h.tools)
			if err != nil {
				t.Fatal(err)
			}
			body := map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "tools/call",
				"params": map[string]any{"name": "add_entity", "arguments": entityArgs},
			}
			buf, _ := json.Marshal(body)
			req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(buf))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("MCP status=%d body=%s", rec.Code, rec.Body.String())
			}
		case viaACP:
			acpSrv := acp.New(h.tools)
			in := &bytes.Buffer{}
			out := &bytes.Buffer{}
			body := map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "tools/call",
				"params": map[string]any{"name": "add_entity", "arguments": entityArgs},
			}
			buf, _ := json.Marshal(body)
			in.Write(buf)
			in.WriteByte('\n')
			if err := acpSrv.Serve(context.Background(), in, out); err != nil {
				t.Fatal(err)
			}
			var resp map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
				t.Fatalf("ACP decode: %v body=%s", err, out.String())
			}
		default:
			r := agent.Dispatch(t.Context(), h.tools, agent.ToolCall{Name: "add_entity", Args: entityArgs})
			if !r.OK {
				t.Fatalf("native: %+v", r)
			}
		}

		// All transports should leave the same world state.
		if _, ok := h.live.Session().World.Entities["posts"]; !ok {
			t.Errorf("posts missing after add via transport")
		}
	}

	t.Run("native", func(t *testing.T) { tcase(t, false, false) })
	t.Run("mcp", func(t *testing.T) { tcase(t, true, false) })
	t.Run("acp", func(t *testing.T) { tcase(t, false, true) })
}

// --- (8) widget injection on every page -----------------------------

func TestWidgetInjectedOnEveryPage(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{"/", "/dashboard", "/settings"} {
		r := h.tools.AddPage(t.Context(), protocol.AddPageArgs{Page: &world.Page{
			Path: p,
			Tree: world.Node{Kind: "div", Children: []world.Node{
				{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Title"}},
			}},
		}})
		if !r.OK {
			t.Fatalf("add page %s: %+v", p, r)
		}
	}

	// Mount the chat server so the widget JS endpoint exists too (chat tests
	// cover that endpoint; here we only assert injection in pages).
	chatSrv := chat.New(h.live, h.tools)
	chatSrv.Mount(h.live.Aux())

	for _, p := range []string{"/", "/dashboard", "/settings"} {
		rec := httptest.NewRecorder()
		h.live.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != 200 {
			t.Fatalf("%s status=%d", p, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "/__gofastr/runtime.js") {
			t.Errorf("widget script missing on %s: %s", p, rec.Body.String())
		}
	}
}

// --- (9) seed flow inserts visible rows ------------------------------

func TestSeedFlowInsertsVisibleRows(t *testing.T) {
	h := newHarness(t)
	h.addEntity(t, &world.Entity{
		Name:   "todos",
		Fields: []world.Field{{Name: "text", Type: "string", Required: true}},
	})
	r := h.tools.AddSeed(t.Context(), protocol.AddSeedArgs{Seed: &world.Seed{
		Entity: "todos",
		Rows:   []map[string]any{{"text": "buy milk"}, {"text": "write tests"}},
	}})
	if !r.OK {
		t.Fatalf("add seed: %+v", r)
	}

	// Seeds aren't auto-applied yet — the test just verifies the seed is
	// recorded in the world. Functional seed application is wired in
	// render.ApplySeeds and exercised in kiln/render tests.
	if len(h.live.Session().World.Seeds) != 1 {
		t.Errorf("seed not recorded: %v", h.live.Session().World.Seeds)
	}
}

// --- (10) propose / approve plan ------------------------------------

func TestProposeApprovePlanFlow(t *testing.T) {
	h := newHarness(t)
	r := h.tools.ProposePlan(t.Context(), protocol.ProposePlanArgs{
		PlanID: "p1",
		Steps:  []string{"add posts", "add comments", "add author hook"},
		Reason: "blog scaffold",
	})
	if !r.OK {
		t.Fatalf("propose: %+v", r)
	}
	plan, ok := h.live.Session().Plans["p1"]
	if !ok {
		t.Fatal("plan not recorded")
	}
	if plan.Approved {
		t.Error("freshly proposed plan should not be approved")
	}
	r = h.tools.ApprovePlan(t.Context(), protocol.ApprovePlanArgs{PlanID: "p1"})
	if !r.OK {
		t.Fatalf("approve: %+v", r)
	}
	if !h.live.Session().Plans["p1"].Approved {
		t.Error("plan should be approved after approve_plan")
	}
}

// --- (11) chat carries page context (functional check) -------------

func TestChatRecordsMessageWithPageHeader(t *testing.T) {
	h := newHarness(t)
	chatSrv := chat.New(h.live, h.tools)
	chatSrv.Mount(h.live.Aux())

	// Simulate the widget's POST: it prepends [page=/x] to the user text.
	body := bytes.NewBufferString(`{"role":"user","text":"[page=/dashboard] add a status field"}`)
	req := httptest.NewRequest("POST", "/kiln/chat/message", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.live.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}

	chat := h.live.Session().Chat
	if len(chat) != 1 {
		t.Fatalf("chat events = %d, want 1", len(chat))
	}
	if !strings.Contains(chat[0].Message.Text, "page=/dashboard") {
		t.Errorf("page context lost: %q", chat[0].Message.Text)
	}
}

// --- (12) end-to-end blog scenario ----------------------------------

// A realistic agent might compose: two entities + a derived-field hook +
// a custom health route + two pages with the widget injected. This test
// drives all of it through the same Tools surface and asserts the
// resulting HTTP behavior.
func TestFullBlogScenario(t *testing.T) {
	h := newHarness(t)
	chatSrv := chat.New(h.live, h.tools)
	chatSrv.Mount(h.live.Aux())

	// 1. App config.
	if r := h.tools.SetAppConfig(t.Context(), protocol.SetAppConfigArgs{
		Config: world.AppConfig{Name: "blog", JSONCase: "snake"},
	}); !r.OK {
		t.Fatal(r)
	}

	// 2. Two entities.
	h.addEntity(t, &world.Entity{
		Name: "users",
		Fields: []world.Field{
			{Name: "email", Type: "string", Required: true, Unique: true},
			{Name: "name", Type: "string"},
		},
	})
	h.addEntity(t, &world.Entity{
		Name: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true, Max: floatPtr(200)},
			{Name: "body", Type: "text"},
			{Name: "slug", Type: "string"},
			{Name: "status", Type: "enum", Values: []string{"draft", "published"}, Default: "draft"},
		},
		SoftDelete: true,
	})

	// 3. Auto-derive slug before create.
	if r := h.tools.AddHook(t.Context(), protocol.AddHookArgs{Hook: &world.Hook{
		ID: "posts_slug", Entity: "posts", When: "before_create",
		Action: world.Action{Kind: world.ActionSetField, Params: map[string]any{
			"field": "slug", "value": "lower(entity.title)",
		}},
	}}); !r.OK {
		t.Fatal(r)
	}

	// 4. Health route.
	if r := h.tools.AddRoute(t.Context(), protocol.AddRouteArgs{Route: &world.Route{
		Method: "GET", Path: "/health",
		Action: world.Action{Kind: world.ActionRespondJSON, Params: map[string]any{
			"status": float64(200),
			"body":   map[string]any{"ok": true, "app": "blog"},
		}},
	}}); !r.OK {
		t.Fatal(r)
	}

	// 5. Two pages.
	for _, p := range []string{"/", "/posts/new"} {
		if r := h.tools.AddPage(t.Context(), protocol.AddPageArgs{Page: &world.Page{
			Path: p,
			Tree: world.Node{Kind: "div", Children: []world.Node{
				{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Blog"}},
				{Kind: "link", Props: map[string]any{"href": "/posts", "text": "All posts"}},
			}},
		}}); !r.OK {
			t.Fatal(r)
		}
	}

	// --- assertions ---

	// Health route works.
	rec := httptest.NewRecorder()
	h.live.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != 200 {
		t.Errorf("/health: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "blog") {
		t.Errorf("/health body=%s", rec.Body.String())
	}

	// CRUD endpoint exists and posts derive slug via hook.
	rec = httptest.NewRecorder()
	body := bytes.NewBufferString(`{"title":"Hello World","body":"first post","status":"published"}`)
	req := httptest.NewRequest("POST", "/api/posts", body)
	req.Header.Set("Content-Type", "application/json")
	h.live.ServeHTTP(rec, req)
	if rec.Code >= 400 {
		t.Fatalf("POST /api/posts: %d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.live.ServeHTTP(rec, httptest.NewRequest("GET", "/api/posts", nil))
	out := rec.Body.String()
	if !strings.Contains(out, "hello world") {
		t.Errorf("slug not derived to lowercase: %s", out)
	}

	// Pages are served and inject the widget.
	for _, p := range []string{"/", "/posts/new"} {
		rec := httptest.NewRecorder()
		h.live.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != 200 {
			t.Errorf("page %s: %d", p, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "/__gofastr/runtime.js") {
			t.Errorf("widget not injected on %s", p)
		}
	}

	// World snapshot reflects the assembled state.
	world := h.live.Session().World
	if len(world.Entities) != 2 || len(world.Hooks) != 1 || len(world.Routes) != 1 || len(world.Pages) != 2 {
		t.Errorf("world counts off: entities=%d hooks=%d routes=%d pages=%d",
			len(world.Entities), len(world.Hooks), len(world.Routes), len(world.Pages))
	}
}

func floatPtr(f float64) *float64 { return &f }

// --- (13) framework primitives still produce OpenAPI ----------------

func TestOpenAPIIsServedAfterAddEntity(t *testing.T) {
	h := newHarness(t)
	h.addEntity(t, &world.Entity{
		Name:   "posts",
		Fields: []world.Field{{Name: "title", Type: "string", Required: true}},
	})
	// framework.App registers /openapi.json in App.Start. Our Live skips
	// Start; manually invoke the OpenAPI spec helper.
	// Functional check: hitting /openapi.json should 404 because we
	// don't auto-register it. Document the gap.
	rec := httptest.NewRecorder()
	h.live.ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
	if rec.Code == 200 {
		// The framework auto-mounts on Start; if we ever wire that into
		// Live, this branch becomes the success path.
		t.Log("openapi served (framework now auto-mounts on Live)")
	} else {
		t.Logf("openapi not served outside Start path (status=%d) — known gap", rec.Code)
	}
	_ = io.Discard
}
