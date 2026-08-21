package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// The entity CRUD MCP tools work by re-dispatching through the router, which
// is what makes them inherit auth, owner scoping and tenant scoping instead of
// re-implementing them. That only holds if they dispatch to the path the
// routes are actually mounted at.
//
// App.Entity sets crudHandler.BasePath from the API prefix; GroupEntity never
// set it, so a grouped entity's tools dispatched to "/widgets" while the
// routes lived at "/api/widgets". Fail-closed (a 404, not a data leak) but the
// tools were simply broken for every grouped entity.
func TestGroupEntityMCPToolsReachGroupedRoutes(t *testing.T) {
	db := openMemDB(t)
	app := NewApp(WithDB(db))
	g := app.Group("/api")
	app.GroupEntity(g, "widgets", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{MCP: true, Public: true},
		Fields:   []schema.Field{{Name: "name", Type: schema.String}},
	})
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	if _, err := app.MCP.CallTool(context.Background(), "widgets_list", map[string]any{}); err != nil {
		t.Fatalf("widgets_list on a grouped entity: %v", err)
	}
}

// The same bug on a nested group: the dispatch path must be the FULL composed
// prefix, not just the innermost one.
func TestNestedGroupEntityMCPToolsReachRoutes(t *testing.T) {
	db := openMemDB(t)
	app := NewApp(WithDB(db))
	g := app.Group("/api").Group("/v2")
	app.GroupEntity(g, "gadgets", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{MCP: true, Public: true},
		Fields:   []schema.Field{{Name: "name", Type: schema.String}},
	})
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	if _, err := app.MCP.CallTool(context.Background(), "gadgets_list", map[string]any{}); err != nil {
		t.Fatalf("gadgets_list under /api/v2: %v", err)
	}
}

func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// ":memory:" with the default pool hands out a fresh empty database per
	// connection. Pin to one.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}
