package render_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofastr/gofastr/kiln/render"
	"github.com/gofastr/gofastr/kiln/world"
	"github.com/gofastr/gofastr/framework"
)

// --- routes ----------------------------------------------------------

func TestApplyRouteRespondsJSON(t *testing.T) {
	app, _ := newTestApp(t)
	w := world.New()
	w.Routes = append(w.Routes, &world.Route{
		Method: "GET",
		Path:   "/health",
		Action: world.Action{
			Kind: world.ActionRespondJSON,
			Params: map[string]any{
				"status": float64(200),
				"body":   map[string]any{"ok": true},
			},
		},
	})
	if err := render.Apply(app, w); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	res, body := get(t, app.Router, "/health")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %q", res.StatusCode, body)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v body=%q", err, body)
	}
	if got["ok"] != true {
		t.Errorf("body = %v", got)
	}
}

// --- hooks ------------------------------------------------------------

func TestApplyHookValidatesOnCreate(t *testing.T) {
	app, db := newTestApp(t)
	w := world.New()
	w.Entities["posts"] = &world.Entity{
		Name: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
		},
	}
	// Refuse posts where title is "spam".
	w.Hooks = append(w.Hooks, &world.Hook{
		ID:     "h1",
		Entity: "posts",
		When:   "before_create",
		Action: world.Action{
			Kind: world.ActionValidate,
			Params: map[string]any{
				"expression": `entity.title != "spam"`,
				"message":    "title cannot be spam",
			},
		},
	})

	if err := render.Apply(app, w); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Allowed post passes through.
	good := bytes.NewBufferString(`{"title":"hello"}`)
	res := postJSON(t, app.Router, "/posts", good)
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		t.Errorf("good post status = %d", res.StatusCode)
	}

	// Spam post is rejected by the declarative hook.
	bad := bytes.NewBufferString(`{"title":"spam"}`)
	res = postJSON(t, app.Router, "/posts", bad)
	if res.StatusCode < 400 {
		t.Errorf("expected client error for spam, got %d", res.StatusCode)
	}
}

func TestApplyHookConditionGate(t *testing.T) {
	app, db := newTestApp(t)
	w := world.New()
	w.Entities["posts"] = &world.Entity{
		Name: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
			{Name: "status", Type: "string"},
		},
	}
	// Validate only when status is "draft".
	w.Hooks = append(w.Hooks, &world.Hook{
		ID:        "h1",
		Entity:    "posts",
		When:      "before_create",
		Condition: `entity.status == "draft"`,
		Action: world.Action{
			Kind: world.ActionValidate,
			Params: map[string]any{
				"expression": `len(entity.title) >= 5`,
				"message":    "draft title too short",
			},
		},
	})
	if err := render.Apply(app, w); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// status != draft → validation skipped.
	body := bytes.NewBufferString(`{"title":"hi","status":"published"}`)
	res := postJSON(t, app.Router, "/posts", body)
	if res.StatusCode >= 400 {
		t.Errorf("non-draft should pass, got %d", res.StatusCode)
	}

	// status == draft + short title → validation runs and fails.
	body = bytes.NewBufferString(`{"title":"hi","status":"draft"}`)
	res = postJSON(t, app.Router, "/posts", body)
	if res.StatusCode < 400 {
		t.Errorf("short draft should fail, got %d", res.StatusCode)
	}
}

// --- helpers ----------------------------------------------------------

func postJSON(t *testing.T, h http.Handler, path string, body io.Reader) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

// Sanity that the framework error path on a validation rejection ends up
// as 4xx in the body.
func TestHookRejectionIncludesMessage(t *testing.T) {
	app, db := newTestApp(t)
	w := world.New()
	w.Entities["posts"] = &world.Entity{
		Name: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
		},
	}
	w.Hooks = append(w.Hooks, &world.Hook{
		ID:     "h1",
		Entity: "posts",
		When:   "before_create",
		Action: world.Action{Kind: world.ActionValidate, Params: map[string]any{
			"expression": "false",
			"message":    "always-block-message",
		}},
	})
	if err := render.Apply(app, w); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	res := postJSON(t, app.Router, "/posts", bytes.NewBufferString(`{"title":"x"}`))
	out, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(out), "always-block-message") {
		t.Errorf("response missing message: %q", string(out))
	}
}
