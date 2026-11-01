package crud

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// A JSON column takes no filter (2026-10-27 bug bash D17). Equality
// compared the stored text on SQLite and the parsed JSONB value on
// Postgres, so {"seats":5.0} found the stored {"seats":5} on one engine
// and not on the other. Both refuse every spelling of it now.
func TestJSONFilterRefusedOnBothDialects(t *testing.T) {
	cfg := entity.EntityConfig{
		Name: "jf_plans", Table: "jf_plans",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "features", Type: schema.JSON},
		},
	}.WithTimestamps(false)
	dialects := []struct {
		name  string
		setup func(t *testing.T) *sql.DB
		ddl   string
	}{
		{"sqlite", func(t *testing.T) *sql.DB { return setupDB(t) }, `CREATE TABLE jf_plans (id TEXT PRIMARY KEY, name TEXT, features TEXT)`},
		{"postgres", pgtest.DB, `CREATE TABLE jf_plans (id TEXT PRIMARY KEY, name TEXT, features JSONB)`},
	}
	for _, d := range dialects {
		t.Run(d.name, func(t *testing.T) {
			db := d.setup(t)
			if _, err := db.Exec(d.ddl); err != nil {
				t.Fatalf("exec DDL: %v", err)
			}
			if _, err := db.Exec(`INSERT INTO jf_plans (id, name, features) VALUES ('p1', 'pro', '{"seats":5}')`); err != nil {
				t.Fatalf("seed: %v", err)
			}
			ent := entity.Define(cfg.Name, cfg)
			ent.SetDB(db)
			installSecurityOwnerExtractor(t)
			ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
			for _, q := range []string{
				"features=" + url.QueryEscape(`{"seats":5.0}`),
				"features=" + url.QueryEscape(`{"seats":5}`),
				"features_ne=" + url.QueryEscape(`{"seats":5}`),
				"features_in=" + url.QueryEscape(`5`),
				"where=" + url.QueryEscape(`{"field":"features","value":{"seats":5}}`),
			} {
				req := withTestUser(httptest.NewRequest(http.MethodGet, "/jf_plans?"+q, nil), "u1")
				rec := httptest.NewRecorder()
				ch.List()(rec, req)
				if rec.Code != http.StatusBadRequest {
					t.Errorf("?%s = %d, want 400; body=%s", q, rec.Code, rec.Body.String())
				}
			}
		})
	}
}
