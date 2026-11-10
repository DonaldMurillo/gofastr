//go:build red

package framework

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// RED (2026-11-10 bug hunt): ?sort= on a nullable column orders the
// rows differently on SQLite and Postgres.
//
// crud emits a bare ORDER BY col ASC|DESC. SQLite sorts NULL below every
// value and Postgres above, so GET /posts?sort=-views ("most viewed
// first") opens with the rows that have no view count on Postgres and
// ends with them on SQLite; ?sort=views is the mirror image. An app
// built and tested on SQLite pages a different list in production, and
// with ?limit= the first page holds different rows. The cursor path
// already knows this (entity.Define refuses a nullable cursor column and
// cursor-pagination.md says why); the offset list, ?stream=true,
// ListAll, TypedQuery.Order and GroupCountAll's group order do not.
//
// Decision needed: which order is the contract, and what it may cost.
// Emitting NULLS FIRST / NULLS LAST makes both dialects agree (SQLite
// 3.30+ and Postgres both take it), but on Postgres an ORDER BY that
// names a null position other than the index's own (ASC NULLS LAST,
// DESC NULLS FIRST) cannot use a plain single-column index, so pinning
// SQLite's order there turns indexed sorts into sort nodes. The
// alternatives are pinning Postgres's order on SQLite (no index cost on
// Postgres; SQLite's planner handles NULLS LAST on ASC with a sort), or
// documenting the drift next to ?sort=.
func TestRedSortNullOrderMatchesAcrossDialects(t *testing.T) {
	if _, err := resolvePostgresOnce(); err != nil {
		t.Skipf("needs Postgres to compare against SQLite: %v", err)
	}
	orders := map[Dialect]map[string]string{}
	for _, d := range []Dialect{migrate.DialectSQLite, migrate.DialectPostgres} {
		db := openTestDB(t, d)
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table: "posts", Exposure: &entity.ExposureConfig{Public: true},
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "views", Type: schema.Int},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("%v: automigrate: %v", d, err)
		}
		ta := TestHarness(t, app)
		for _, body := range []map[string]any{
			{"title": "ten", "views": 10},
			{"title": "none"},
			{"title": "two", "views": 2},
		} {
			ta.Post("/posts", body).AssertStatus(t, http.StatusCreated)
		}
		orders[d] = map[string]string{}
		for _, sort := range []string{"views", "-views"} {
			resp := ta.Get("/posts?sort=" + sort)
			resp.AssertStatus(t, http.StatusOK)
			var page struct {
				Data []map[string]any `json:"data"`
			}
			if err := json.Unmarshal(resp.recorder.Body.Bytes(), &page); err != nil {
				t.Fatalf("%v ?sort=%s: %v", d, sort, err)
			}
			var titles []string
			for _, row := range page.Data {
				titles = append(titles, fmt.Sprint(row["title"]))
			}
			orders[d][sort] = strings.Join(titles, ",")
		}
	}
	for _, sort := range []string{"views", "-views"} {
		lite, pg := orders[migrate.DialectSQLite][sort], orders[migrate.DialectPostgres][sort]
		if lite != pg {
			t.Errorf("GET /posts?sort=%s orders the same rows differently: SQLite %s, Postgres %s", sort, lite, pg)
		}
	}
}
