package framework

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Including a relation that is also a key column replaces its value with
// the related row; the cursor still pages on the stored key.
func TestCursorIncludeOnKeyRelation(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		team := entity.Define("team", entity.EntityConfig{
			Table: "teams",
			Fields: []schema.Field{
				{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
				{Name: "name", Type: schema.String},
			},
		}.WithTimestamps(false))
		slot := entity.Define("slot", entity.EntityConfig{
			Table: "slots",
			Fields: []schema.Field{
				{Name: "team_id", Type: schema.Relation, To: "team", Required: true},
				{Name: "position", Type: schema.String, Required: true},
			},
			PrimaryKey: []string{"team_id", "position"},
		}.WithTimestamps(false))
		app := relApp(t, db, team, slot)
		tid := relID(t, app, "/teams", map[string]any{"name": "Owls"})
		for _, pos := range []string{"a", "b", "c"} {
			relDo(t, app, "", "POST", "/slots", map[string]any{"team_id": tid, "position": pos})
		}

		var seen []string
		cursor := ""
		for range 5 {
			rec := serveKeyedAs(t, app, "", "GET", "/slots?limit=1&include=team_id&cursor="+url.QueryEscape(cursor), nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("page after %v = %d %s", seen, rec.Code, rec.Body)
			}
			var page struct {
				Data    []map[string]any
				Cursor  string
				HasMore bool
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			for _, row := range page.Data {
				seen = append(seen, row["position"].(string))
			}
			if !page.HasMore {
				break
			}
			cursor = page.Cursor
		}
		if !slices.Equal(seen, []string{"a", "b", "c"}) {
			t.Fatalf("paged positions = %v, want [a b c]", seen)
		}
	})
}

// A belongs-to cascade needs only the target's key, so a parent keyed on
// two columns still creates its nested author.
func TestCascadeBelongsToCompositeParent(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		author := entity.Define("author", entity.EntityConfig{
			Table: "authors",
			Fields: []schema.Field{
				{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
				{Name: "name", Type: schema.String},
			},
		}.WithTimestamps(false))
		post := entity.Define("post", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "author_id", Type: schema.Int, Required: true},
				{Name: "slug", Type: schema.String, Required: true},
			},
			PrimaryKey: []string{"author_id", "slug"},
			Relations: []entity.Relation{
				entity.BelongsTo("author", "author", "author_id").WithCascadeWrite(true),
			},
		}.WithTimestamps(false))
		app := relApp(t, db, author, post)
		rec := serveKeyedAs(t, app, "", "POST", "/posts", map[string]any{"slug": "hello", "author": map[string]any{"name": "Ann"}})
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST nested author = %d %s", rec.Code, rec.Body)
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil || n != 1 {
			t.Fatalf("posts = %d (%v), want 1", n, err)
		}
	})
}

// SplitKey refuses an empty part, so a row with a blank key column could
// never be named again; the write is refused instead.
func TestCompositeKeyRefusesBlankPart(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		pair := entity.Define("pair", entity.EntityConfig{
			Table: "pairs",
			Fields: []schema.Field{
				{Name: "a", Type: schema.String},
				{Name: "b", Type: schema.String},
				{Name: "note", Type: schema.String},
			},
			PrimaryKey: []string{"a", "b"},
		}.WithTimestamps(false))
		app := relApp(t, db, pair)
		refused := func(code int) bool { return code == http.StatusBadRequest || code == http.StatusUnprocessableEntity }
		if rec := serveKeyedAs(t, app, "", "POST", "/pairs", map[string]any{"a": "", "b": "x"}); !refused(rec.Code) {
			t.Errorf("POST blank key part = %d %s, want a validation refusal", rec.Code, rec.Body)
		}
		relDo(t, app, "", "POST", "/pairs", map[string]any{"a": "k", "b": "x"})
		if rec := serveKeyedAs(t, app, "", "PATCH", "/pairs/k/x", map[string]any{"a": ""}); !refused(rec.Code) {
			t.Errorf("PATCH blank key part = %d %s, want a validation refusal", rec.Code, rec.Body)
		}
	})
}
