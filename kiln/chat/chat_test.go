package chat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/chat"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func setup(t *testing.T) (*live.Live, *protocol.Tools) {
	t.Helper()
	d, cleanup, err := db.EphemeralSQLite("kiln-chat")
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
	srv := chat.New(l, tools)
	srv.Mount(l.Aux())
	l.SetFallbackHTML(chat.HostHTML())
	return l, tools
}

func TestHostFallbackOnUnmappedHTMLPath(t *testing.T) {
	l, _ := setup(t)
	for _, path := range []string{"/", "/anything", "/random/junk"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		l.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{"Kiln", "/__gofastr/runtime.js"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s body missing %q: %s", path, want, body)
			}
		}
	}
}

func TestNonHTMLPathStill404s(t *testing.T) {
	l, _ := setup(t)
	req := httptest.NewRequest(http.MethodGet, "/some/api/endpoint", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for JSON request, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWidgetCSSServed(t *testing.T) {
	l, _ := setup(t)
	req := httptest.NewRequest(http.MethodGet, "/kiln/chat/widget.css", nil)
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{".kiln-widget", ".kiln-panel", "backdrop-filter", ".kiln-corner-bottom-right"} {
		if !strings.Contains(body, want) {
			t.Errorf("widget.css missing %q", want)
		}
	}
}

func TestWorldEndpointReturnsJSON(t *testing.T) {
	l, tools := setup(t)
	tools.AddEntity(t.Context(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
	})
	req := httptest.NewRequest(http.MethodGet, "/kiln/world", nil)
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, rec.Body.String())
	}
	w, ok := got["world"].(map[string]any)
	if !ok {
		t.Fatalf("missing world: %v", got)
	}
	ents, _ := w["entities"].(map[string]any)
	if _, hit := ents["posts"]; !hit {
		t.Errorf("posts missing: %v", w)
	}
}

func TestStatusEndpointDefaults(t *testing.T) {
	l, tools := setup(t)
	tools.AddEntity(t.Context(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
	})
	tools.Chat(t.Context(), protocol.ChatArgs{Role: "user", Text: "hi"})
	tools.Chat(t.Context(), protocol.ChatArgs{Role: "assistant", Text: "hello!"})
	tools.ProposePlan(t.Context(), protocol.ProposePlanArgs{
		PlanID: "p-pending", Steps: []string{"think"},
	})

	req := httptest.NewRequest(http.MethodGet, "/kiln/status", nil)
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, rec.Body.String())
	}

	// Defaults: counts, last_user, last_assistant, pending_plans, recent.
	for _, want := range []string{"counts", "last_user", "last_assistant", "pending_plans", "recent"} {
		if _, ok := got[want]; !ok {
			t.Errorf("default response missing %q: %v", want, got)
		}
	}
	// Default does NOT include heavy fields.
	for _, unwanted := range []string{"world", "plans", "chat"} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("default response should not include %q (caller didn't ask): %v", unwanted, got)
		}
	}

	// counts shape sanity.
	counts, _ := got["counts"].(map[string]any)
	if counts["entities"].(float64) != 1 {
		t.Errorf("entities count = %v, want 1", counts["entities"])
	}
	if counts["plans"].(float64) != 1 {
		t.Errorf("plans count = %v, want 1", counts["plans"])
	}
	if counts["chat"].(float64) != 2 {
		t.Errorf("chat count = %v, want 2", counts["chat"])
	}

	// last_user / last_assistant shape sanity.
	lu, _ := got["last_user"].(map[string]any)
	if lu == nil || lu["message"] == nil {
		t.Errorf("last_user missing or empty: %v", got["last_user"])
	}
	la, _ := got["last_assistant"].(map[string]any)
	if la == nil || la["message"] == nil {
		t.Errorf("last_assistant missing: %v", got["last_assistant"])
	}

	// pending_plans contains the un-decided one.
	pending, _ := got["pending_plans"].([]any)
	if len(pending) != 1 {
		t.Errorf("pending_plans count = %d, want 1", len(pending))
	}
}

func TestStatusEndpointFieldsParam(t *testing.T) {
	l, tools := setup(t)
	tools.AddEntity(t.Context(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
	})

	// Only counts.
	req := httptest.NewRequest(http.MethodGet, "/kiln/status?fields=counts", nil)
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if _, ok := got["counts"]; !ok {
		t.Errorf("counts missing under fields=counts: %v", got)
	}
	if _, ok := got["recent"]; ok {
		t.Errorf("recent should not appear when not requested: %v", got)
	}
	if _, ok := got["last_user"]; ok {
		t.Errorf("last_user should not appear when not requested: %v", got)
	}

	// world only — heavy field, opt-in.
	req = httptest.NewRequest(http.MethodGet, "/kiln/status?fields=world", nil)
	rec = httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	got = map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	w, ok := got["world"].(map[string]any)
	if !ok {
		t.Fatalf("world missing under fields=world: %v", got)
	}
	if _, hit := w["entities"].(map[string]any); !hit {
		t.Errorf("world.entities missing: %v", w)
	}
}

func TestStatusEndpointRecentN(t *testing.T) {
	l, tools := setup(t)
	for i := 0; i < 25; i++ {
		tools.Chat(t.Context(), protocol.ChatArgs{Role: "user", Text: "msg"})
	}

	req := httptest.NewRequest(http.MethodGet, "/kiln/status?fields=recent&recent_n=5", nil)
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	recent, _ := got["recent"].([]any)
	if len(recent) != 5 {
		t.Errorf("recent length = %d, want 5", len(recent))
	}
}

func TestToolDispatchHappyPath(t *testing.T) {
	l, _ := setup(t)
	body := bytes.NewBufferString(`{"entity":{"name":"posts","fields":[{"name":"title","type":"string"}]}}`)
	req := httptest.NewRequest(http.MethodPost, "/kiln/tool/add_entity", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var res protocol.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !res.OK {
		t.Errorf("expected OK, got %+v", res)
	}
}

func TestToolDispatchUnknownTool(t *testing.T) {
	l, _ := setup(t)
	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/kiln/tool/not_real", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("expected non-200 for unknown tool, got %d", rec.Code)
	}
}

// Every descriptor must be reachable via the HTTP /kiln/tool/{name}
// surface. The two dispatch tables (kiln/agent/loop.go for in-process
// + MCP, kiln/chat/server.go for HTTP) are easy to update out of step
// — this test pins the invariant: if you add a descriptor, you also
// have to wire HTTP, otherwise the tool surface published in /kiln/world
// claims a tool that callers can't actually invoke from curl.
func TestEveryDescriptorReachableViaHTTP(t *testing.T) {
	l, tools := setup(t)
	for _, d := range tools.List() {
		t.Run(d.Name, func(t *testing.T) {
			// Send `{}` — most tools fail validation, which is fine. We
			// only care that the dispatcher recognized the tool name
			// and got far enough to invoke it. Status 400 with body
			// "unknown tool" is the failure mode this guards against.
			req := httptest.NewRequest(http.MethodPost, "/kiln/tool/"+d.Name, bytes.NewBufferString(`{}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			l.ServeHTTP(rec, req)
			body := rec.Body.String()
			if strings.Contains(body, "unknown tool") {
				t.Fatalf("HTTP dispatcher reports %q as unknown — wire it in kiln/chat/server.go's tool switch (it's in the descriptor table but not the HTTP path)", d.Name)
			}
		})
	}
}

func TestToolDispatchInvalidJSON(t *testing.T) {
	l, _ := setup(t)
	body := bytes.NewBufferString(`not json`)
	req := httptest.NewRequest(http.MethodPost, "/kiln/tool/add_entity", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("invalid JSON should fail, got %d", rec.Code)
	}
}

func TestChatMessageEndpoint(t *testing.T) {
	l, _ := setup(t)
	body := bytes.NewBufferString(`{"role":"user","text":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/kiln/chat/message", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChatPanelSurvivesRebuild(t *testing.T) {
	l, tools := setup(t)
	// Trigger a rebuild via a world edit.
	tools.AddEntity(t.Context(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "x", Fields: []world.Field{{Name: "y", Type: "string"}}},
	})
	// Panel assets and host fallback should still respond.
	for _, path := range []string{"/kiln/chat/widget.css"} {
		rec := httptest.NewRecorder()
		l.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("after rebuild, %s = %d", path, rec.Code)
		}
	}
	// Host fallback still serves on unmapped HTML paths.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("after rebuild, / fallback = %d", rec.Code)
	}
}

func TestSSEMountedOnAux(t *testing.T) {
	// SSE handler must be reachable via aux router so it survives rebuilds.
	// Run against a real httptest.Server so the connection can be closed.
	l, _ := setup(t)
	srv := httptest.NewServer(l)
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/.kiln/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Errorf(".kiln/events should be mounted on aux, got 404")
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	cancel() // close the stream
}
