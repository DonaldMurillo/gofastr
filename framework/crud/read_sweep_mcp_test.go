package crud

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The list tool forwards only the params its schema advertises: an
// operator the field's type refuses, and ?q= on an entity with no
// SearchFields, are dropped (a wider page, never a narrower one).
func TestMCPListToolDropsUnadvertisedParams(t *testing.T) {
	ent := entity.Define("flags", entity.EntityConfig{Name: "flags", Table: "flags",
		Fields: []schema.Field{{Name: "on", Type: schema.Bool}}}.WithTimestamps(false))
	ch := &CrudHandler{Entity: ent}
	var query string
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	if _, err := ch.listTool(router)(context.Background(), map[string]any{"on_gt": true, "q": "x", "on": true}); err != nil {
		t.Fatal(err)
	}
	if query != "on=true" {
		t.Fatalf("forwarded %q, want on=true alone", query)
	}
}

// The write tools of an entity whose MCP exposure only the dev loop
// implied are marked dev-implied, so the bind guard withdraws them; its
// reads stay, and an entity that opted in itself is never marked.
func TestMCPDevImpliedMarksWriteToolsOnly(t *testing.T) {
	register := func(exposure *entity.ExposureConfig) []string {
		ent := entity.Define("notes", entity.EntityConfig{Name: "notes", Table: "notes",
			Fields: []schema.Field{{Name: "body", Type: schema.String}}, Exposure: exposure}.WithTimestamps(false))
		srv := mcp.NewServer()
		if err := RegisterEntityMCPTools(srv, &CrudHandler{Entity: ent}, http.NewServeMux()); err != nil {
			t.Fatal(err)
		}
		return srv.UnregisterDevImpliedTools()
	}
	if got := strings.Join(register(nil), ","); got != "notes_create,notes_delete,notes_update" {
		t.Fatalf("dev-implied withdrew %q, want the three write tools", got)
	}
	if got := register(&entity.ExposureConfig{MCP: true}); len(got) != 0 {
		t.Fatalf("an explicit Exposure.MCP entity had %v withdrawn", got)
	}
}
