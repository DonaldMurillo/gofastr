package framework

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An entity may declare stage beside stage_in (and status beside
// status_ne): every column stays reachable through the bracket form, and
// the bare key that reads two ways answers 400 on every surface instead
// of filtering whichever column the suffix rule picked. Registration
// used to refuse the pair, because before the bracket form one of the
// two columns could never be filtered.
func TestBracketFiltersReachCollidingFields(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("rosters", entity.EntityConfig{
			Table:     "rosters",
			Fields:    []schema.Field{{Name: "name", Type: schema.String, Required: true}},
			Relations: []entity.Relation{entity.HasMany("members", "members", "roster_id")},
		}.WithTimestamps(false))
		if err := app.TryEntity("members", entity.EntityConfig{
			Table: "members",
			Fields: []schema.Field{
				{Name: "roster_id", Type: schema.String, Required: true},
				{Name: "stage", Type: schema.String},
				{Name: "stage_in", Type: schema.String},
			},
		}.WithTimestamps(false)); err != nil {
			t.Fatalf("an entity with stage and stage_in must register: %v", err)
		}
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		for _, stmt := range []string{
			"INSERT INTO rosters(id, name) VALUES ('r1', 'one'), ('r2', 'two')",
			`INSERT INTO members(id, roster_id, stage, stage_in) VALUES ('m1', 'r1', 'a', 'Morning'), ('m2', 'r1', 'b', 'noon'), ('m3', 'r2', 'a', 'noon')`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		count := func(path string) int {
			t.Helper()
			resp := ta.Get(path)
			resp.AssertStatus(t, http.StatusOK)
			var body ListResponse
			if err := json.Unmarshal([]byte(resp.Body()), &body); err != nil {
				t.Fatalf("%s: decode: %v", path, err)
			}
			return len(body.Data)
		}
		for _, tc := range []struct {
			path string
			want int
		}{
			{"/members?stage_in[eq]=noon", 2},
			{"/members?stage_in%5Beq%5D=noon", 2},
			{"/members?stage[in]=a", 2},
			{"/members?stage[in]=a,b", 3},
			{"/members?stage[eq]=b", 1},
			{"/members?stage=b", 1},
			{"/members?stage_in[eq]=noon&stage[eq]=a", 1},
			{"/members?stage_in[like]=MORN", 1},
			{"/members?stage_in[eq]=noon&cursor=", 2},
			{"/members?stage[in]=a&stream=true", 2},
			{"/rosters?members.stage_in[eq]=Morning", 1},
			{"/rosters?members.stage[in]=b", 1},
			{"/rosters?members.stage[in]=a", 2},
		} {
			if n := count(tc.path); n != tc.want {
				t.Errorf("%s matched %d rows, want %d", tc.path, n, tc.want)
			}
		}
		includeCount := func(clause string) int {
			t.Helper()
			resp := ta.Get("/rosters/r1?include=" + url.QueryEscape(clause))
			resp.AssertStatus(t, http.StatusOK)
			var got map[string]any
			if err := json.Unmarshal([]byte(resp.Body()), singleMap(&got)); err != nil {
				t.Fatalf("decode: %v", err)
			}
			members, _ := got["members"].([]any)
			return len(members)
		}
		if n := includeCount("members(stage_in[eq]=noon)"); n != 1 {
			t.Errorf("include=members(stage_in[eq]=noon) attached %d, want 1", n)
		}
		if n := includeCount("members(stage[in]=a|b)"); n != 2 {
			t.Errorf("include=members(stage[in]=a|b) attached %d, want 2", n)
		}
		if n := includeCount("members(stage[in]=a|b,stage_in[eq]=noon)"); n != 1 {
			t.Errorf("include=members(stage[in]=a|b,stage_in[eq]=noon) attached %d, want 1", n)
		}
		// The bare key reads two ways on every surface: 400 naming both
		// bracket spellings, never a guess.
		for _, path := range []string{
			"/members?stage_in=noon",
			"/members?stage_in=noon&cursor=",
			"/members?stage_in=noon&stream=true",
			"/rosters?members.stage_in=noon",
			"/rosters/r1?include=" + url.QueryEscape("members(stage_in=noon)"),
		} {
			resp := ta.Get(path)
			body := resp.Body()
			if resp.Status() != http.StatusBadRequest || !strings.Contains(body, "stage_in[eq]") || !strings.Contains(body, "stage[in]") {
				t.Errorf("%s = %d, want 400 naming stage_in[eq] and stage[in]: %s", path, resp.Status(), body)
			}
		}
		// An unknown operator word is a 400 on every surface too.
		for _, path := range []string{
			"/members?stage_in[within]=noon",
			"/rosters?members.stage_in[within]=noon",
			"/rosters/r1?include=" + url.QueryEscape("members(stage_in[within]=noon)"),
		} {
			if resp := ta.Get(path); resp.Status() != http.StatusBadRequest || !strings.Contains(resp.Body(), "within") {
				t.Errorf("%s = %d, want 400 naming the operator: %s", path, resp.Status(), resp.Body())
			}
		}
	})
}

// GroupEntity registers the same colliding pair App.Entity does.
func TestGroupEntityAcceptsCollidingFilterKeys(t *testing.T) {
	app := atomicTestApp(t)
	g := app.Group("/billing")
	app.GroupEntity(g, "invoices", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "status", Type: schema.String},
			{Name: "status_ne", Type: schema.String},
		},
	})
}
