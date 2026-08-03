package framework

import (
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// The bug this closes was not in the rendering alone — it was that a valid
// declaration could not boot. schema.validateJSON accepts a Go map, the insert
// path json.Marshals it, and only the DDL rendered Go's debug form, so
// AutoMigrate failed on Postgres (JSONB rejects `map[a:1]`) while SQLite's TEXT
// column stored the literal text and looked fine.
//
// Asserting the rendered string is necessary but not sufficient: the question
// an operator has is whether the app starts. This runs the real migration on
// both dialects.
func TestAutoMigrateAcceptsGoShapedJSONDefaults(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		app := NewApp(WithDB(db))
		if err := app.TryEntity("jsondefaults", EntityConfig{
			Table: "jsondefaults",
			Fields: []schema.Field{
				{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
				{Name: "name", Type: schema.String, Required: true},
				{Name: "flags", Type: schema.JSON, Default: map[string]any{"beta": true}},
				{Name: "tags", Type: schema.JSON, Default: []any{"a", "b"}},
			},
		}); err != nil {
			t.Fatalf("registration refused a declaration the write path handles: %v", err)
		}
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate on %s: %v", dialect, err)
		}
	})
}
