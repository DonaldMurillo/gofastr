package framework

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The grouped Exposure sub-config is documented authoritative, so route/MCP
// wiring must read the normalized config, not the raw flat fields.
func TestExposureCRUDFalseSkipsRoutes(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	crud := false
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("widgets", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "name", Type: schema.String}},
		Exposure: &entity.ExposureConfig{CRUD: &crud},
	}.WithTimestamps(false))

	for _, rt := range app.router.Routes() {
		if strings.HasPrefix(rt.Pattern, "/widgets") {
			t.Fatalf("Exposure.CRUD=false but CRUD route mounted: %s", rt.Pattern)
		}
	}
}

func TestExposureCRUDFalseSkipsGroupRoutes(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	crud := false
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	g := app.Group("/internal")
	app.GroupEntity(g, "widgets", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "name", Type: schema.String}},
		Exposure: &entity.ExposureConfig{CRUD: &crud},
	}.WithTimestamps(false))

	for _, rt := range app.router.Routes() {
		if strings.HasPrefix(rt.Pattern, "/internal/widgets") {
			t.Fatalf("Exposure.CRUD=false but group CRUD route mounted: %s", rt.Pattern)
		}
	}
}

func TestExposureMCPWithoutCRUDRejected(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	crud := false
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	err = app.TryEntity("gadgets", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "name", Type: schema.String}},
		Exposure: &entity.ExposureConfig{MCP: true, CRUD: &crud},
	}.WithTimestamps(false))
	if err == nil {
		t.Fatal("expected MCP=true/CRUD=false contradiction error, got nil")
	}
	if !strings.Contains(err.Error(), "MCP") {
		t.Fatalf("error should name the MCP/CRUD contradiction, got: %v", err)
	}
}
