package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The write tools take what the HTTP route takes: a JSON field holding an
// array, null for an optional field, and a create without the owner
// column the server stamps. Each was refused as invalid params before the
// route ran.
func TestMCPWriteToolsMatchHTTP(t *testing.T) {
	installSecurityOwnerExtractor(t)

	db := setupDB(t, `CREATE TABLE mcp_policies (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL, note TEXT, level TEXT, features TEXT)`)
	ent := entity.Define("mcp_policies", makeEntityConfig("mcp_policies", "mcp_policies", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String, Required: true},
		{Name: "note", Type: schema.String},
		{Name: "level", Type: schema.Enum, Values: []string{"low", "high"}},
		{Name: "features", Type: schema.JSON},
	}))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	r := router.New()
	RegisterCrudRoutes(r, ch, "/mcp_policies")
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatalf("register mcp: %v", err)
	}
	orig, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	ctx := handler.SetUser(mcp.WithRequest(context.Background(), orig), &testUser{id: "alice"})

	call := func(tool, args string) mcp.Response {
		t.Helper()
		p, err := json.Marshal(map[string]any{"name": tool, "arguments": json.RawMessage(args)})
		if err != nil {
			t.Fatal(err)
		}
		return srv.HandleRequest(ctx, mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: p})
	}

	created := call("mcp_policies_create", `{"name": "base", "features": ["sso", "audit"]}`)
	if created.Error != nil {
		t.Fatalf("create without the stamped owner and with an array JSON field: %+v", created.Error)
	}
	var id, owner string
	if err := db.QueryRow(`SELECT id, user_id FROM mcp_policies`).Scan(&id, &owner); err != nil || owner != "alice" {
		t.Fatalf("stored row id %q owner %q, %v; want alice's row", id, owner, err)
	}
	if e := call("mcp_policies_update", `{"id": "`+id+`", "note": null, "level": null, "features": null}`).Error; e != nil {
		t.Fatalf("update setting optional fields to null: %+v", e)
	}
	// A required field stays required and non-null.
	if e := call("mcp_policies_create", `{"note": "x"}`).Error; e == nil {
		t.Errorf("create without the required name was accepted")
	}
	if e := call("mcp_policies_update", `{"id": "`+id+`", "name": null}`).Error; e == nil {
		t.Errorf("update nulling the required name was accepted")
	}
}
