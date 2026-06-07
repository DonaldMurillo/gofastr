package framework

import (
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestAPIPrefix_MCPToolsReachPrefixedRoutes verifies that entity MCP tools work
// when the app mounts CRUD under an API prefix (WithAPIPrefix). The MCP tool
// handlers dispatch in-process against the app router; if they build the bare
// "/table" path while the routes live at "/api/v1/table", every tool 404s.
func TestAPIPrefix_MCPToolsReachPrefixedRoutes(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTable(t, db)

		app := NewApp(WithDB(db), WithAPIPrefix("/api/v1"))
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
			"title":  "Prefixed",
			"body":   "via MCP under /api/v1",
			"status": "draft",
		})
		created := createResult.(map[string]any)
		id, ok := created["id"].(string)
		if !ok || id == "" {
			t.Fatalf("create result missing id (MCP did not reach the prefixed route): %#v", createResult)
		}

		listResult := callMCPTool(t, app.MCP, "posts_list", map[string]any{"limit": 10})
		data := listResult.(map[string]any)["data"].([]any)
		if len(data) != 1 {
			t.Fatalf("list under prefix returned %d rows, want 1", len(data))
		}

		getResult := callMCPTool(t, app.MCP, "posts_get", map[string]any{"id": id})
		if getResult.(map[string]any)["title"] != "Prefixed" {
			t.Fatalf("get under prefix = %#v", getResult)
		}
	})
}
