package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestEntityFromFileRegistersJSONDeclaration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "posts.json")
	if err := os.WriteFile(path, []byte(`{
		"name": "posts",
		"table": "posts",
		"fields": [
			{"name": "title", "type": "string", "required": true, "max": 120},
			{"name": "status", "type": "enum", "values": ["draft", "published"]}
		],
		"soft_delete": true,
		"crud": true
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	ent, err := app.EntityFromFile(path)
	if err != nil {
		t.Fatalf("EntityFromFile: %v", err)
	}
	if ent.GetName() != "posts" {
		t.Fatalf("entity name = %q", ent.GetName())
	}
	if _, err := app.Registry.Get("posts"); err != nil {
		t.Fatalf("registry missing posts: %v", err)
	}
	if _, ok := ent.Schema().FieldByName("deleted_at"); !ok {
		t.Fatal("soft delete field was not injected")
	}
	status, ok := ent.Schema().FieldByName("status")
	if !ok || status.Type != schema.Enum || len(status.Values) != 2 {
		t.Fatalf("status field not decoded correctly: %#v", status)
	}
}

func TestEntitiesFromDirLoadsDeclarationsInDirectory(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"posts.json": `{"name":"posts","fields":[{"name":"title","type":"string"}]}`,
		"users.json": `{"name":"users","fields":[{"name":"email","type":"string","required":true}]}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	app := NewApp()
	if err := app.EntitiesFromDir(dir); err != nil {
		t.Fatalf("EntitiesFromDir: %v", err)
	}
	for name := range files {
		entityName := name[:len(name)-len(filepath.Ext(name))]
		if _, err := app.Registry.Get(entityName); err != nil {
			t.Fatalf("registry missing %s: %v", entityName, err)
		}
	}
}

func TestEntityMCPToolsCRUDLifecycle(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTable(t, db)

		app := NewApp(WithDB(db))
		app.Entity("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "body", Type: schema.Text},
				{Name: "status", Type: schema.String},
			},
			MCP: true,
		})

		createResult := callMCPTool(t, app.MCP, "posts_create", map[string]any{
			"title":  "Hello MCP",
			"body":   "Created through a tool",
			"status": "draft",
		})
		created := createResult.(map[string]any)
		id, ok := created["id"].(string)
		if !ok || id == "" {
			t.Fatalf("create result missing id: %#v", createResult)
		}

		getResult := callMCPTool(t, app.MCP, "posts_get", map[string]any{"id": id})
		got := getResult.(map[string]any)
		if got["title"] != "Hello MCP" {
			t.Fatalf("get title = %#v", got["title"])
		}

		updateResult := callMCPTool(t, app.MCP, "posts_update", map[string]any{"id": id, "title": "Hello MCP", "status": "published"})
		updated := updateResult.(map[string]any)
		if updated["status"] != "published" {
			t.Fatalf("updated status = %#v", updated["status"])
		}

		listResult := callMCPTool(t, app.MCP, "posts_list", map[string]any{"limit": 10})
		list := listResult.(map[string]any)
		data := list["data"].([]any)
		if len(data) != 1 {
			t.Fatalf("list data len = %d", len(data))
		}

		deleteResult := callMCPTool(t, app.MCP, "posts_delete", map[string]any{"id": id})
		deleted := deleteResult.(map[string]any)
		if deleted["deleted"] != true {
			t.Fatalf("delete result = %#v", deleted)
		}
	})
}

func TestCustomEndpointHTTPAndMCPRegistration(t *testing.T) {
	app := NewApp()
	app.Entity("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		CRUD:   boolPtr(false),
		Endpoints: []entity.Endpoint{
			{
				Method:      http.MethodPost,
				Path:        "{id}/publish",
				Name:        "posts_publish",
				Description: "Publish a post",
				MCP:         true,
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte(r.PathValue("id") + ":published"))
				}),
				MCPHandler: func(ctx context.Context, params map[string]any) (any, error) {
					return map[string]any{"id": params["id"], "status": "published"}, nil
				},
			},
		},
	})

	resp := TestHarness(t, app).Request(http.MethodPost, "/posts/post-1/publish", nil).Execute()
	resp.AssertStatus(t, http.StatusOK).AssertBodyContains(t, "post-1:published")

	result := callMCPTool(t, app.MCP, "posts_publish", map[string]any{"id": "post-1"})
	toolResult := result.(map[string]any)
	if toolResult["status"] != "published" {
		t.Fatalf("tool result = %#v", toolResult)
	}
}

func callMCPTool(t *testing.T, server *mcp.Server, name string, params map[string]any) any {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"name": name, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	resp := server.HandleRequest(context.Background(), mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  raw,
	})
	if resp.Error != nil {
		t.Fatalf("MCP tool %s failed: %v", name, resp.Error)
	}
	resultData, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal MCP result: %v", err)
	}
	var result mcpToolsCallResult
	if err := json.Unmarshal(resultData, &result); err != nil {
		t.Fatalf("decode MCP envelope: %v", err)
	}
	var out any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("decode MCP result: %v", err)
	}
	return out
}

type mcpToolsCallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}
