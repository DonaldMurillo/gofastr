package crud

import (
	"context"
	"database/sql"
	"net/http"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A belongs-to cascade supplies a value after upsert's preflight validation.
// The final parent schema must refuse it just as CreateOne does, and the
// operation's transaction or savepoint must undo every nested write.
func TestUpsertValidatesCascadeForeignKey(t *testing.T) {
	for _, arm := range []string{"insert", "conflict create child", "conflict update child"} {
		for _, txMode := range []string{"owned", "context", "bound"} {
			t.Run(arm+"/"+txMode, func(t *testing.T) {
				eachDialect(t, func(t *testing.T, conn *sql.DB) {
					for _, stmt := range []string{
						`CREATE TABLE notes (id TEXT PRIMARY KEY, body TEXT)`,
						`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, note_id TEXT)`,
					} {
						if _, err := conn.Exec(stmt); err != nil {
							t.Fatal(err)
						}
					}
					notes := entity.Define("notes", entity.EntityConfig{
						Fields: []schema.Field{{Name: "id", Type: schema.String, Default: "bad"}, {Name: "body", Type: schema.String}},
					}.WithTimestamps(false))
					posts := entity.Define("posts", entity.EntityConfig{
						Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}, {Name: "note_id", Type: schema.String, Required: true, Pattern: "^ok-"}},
						Relations: []entity.Relation{entity.BelongsTo("lead_row", "notes", "note_id").WithCascadeWrite(true)},
					}.WithTimestamps(false))
					notes.SetDB(conn)
					posts.SetDB(conn)
					ch := NewCrudHandler(posts, conn).WithJSONCase(CaseSnake)
					ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"posts": posts, "notes": notes}}
					ctx := context.Background()
					_, createErr := ch.CreateOne(ctx, map[string]any{"id": "control", "lead_row": map[string]any{"id": "bad", "body": "nested"}})
					want := violationKeys(createErr)
					if createErr == nil || crudRefusal(ctx, createErr).code != http.StatusBadRequest || !reflect.DeepEqual(want, map[string][]string{"note_id": {"validation.pattern"}}) {
						t.Fatalf("create control = %v (%v), want note_id validation and HTTP 400", createErr, want)
					}
					var count int
					if err := conn.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&count); err != nil || count != 0 {
						t.Fatalf("create left %d children (%v), want none", count, err)
					}
					conflict := arm != "insert"
					if conflict {
						if _, err := conn.Exec(`INSERT INTO posts (id,title,note_id) VALUES ('parent','before','ok-before')`); err != nil {
							t.Fatal(err)
						}
					}
					child := map[string]any{"body": "changed"}
					if arm != "conflict create child" {
						child["id"] = "bad"
					}
					if arm == "conflict update child" {
						if _, err := conn.Exec(`INSERT INTO notes (id,body) VALUES ('bad','before-child')`); err != nil {
							t.Fatal(err)
						}
					}
					// The conflict arm creates a child when no id is supplied. Its
					// declared default supplies "bad", so this also detects an orphan
					// created before parent validation. An explicit id updates the
					// existing child and must roll that edit back instead.
					var tx *sql.Tx
					if txMode != "owned" {
						var err error
						tx, err = conn.BeginTx(ctx, nil)
						if err != nil {
							t.Fatal(err)
						}
						defer tx.Rollback()
						if txMode == "context" {
							ctx = db.WithTx(ctx, tx)
						} else {
							ch.DB = tx
						}
					}
					_, err := ch.UpsertOne(ctx, map[string]any{"id": "parent", "title": "after", "lead_row": child})
					if got := violationKeys(err); err == nil || crudRefusal(ctx, err).code != http.StatusBadRequest || !reflect.DeepEqual(got, want) {
						t.Errorf("upsert = %v (%v), want create's HTTP 400 and violations %v", err, got, want)
					}
					if tx != nil {
						// Handling the refusal and committing the caller's transaction
						// must not retain the refused operation's nested writes.
						if _, err := tx.Exec(`INSERT INTO notes (id,body) VALUES ('ok-unrelated','keep')`); err != nil {
							t.Fatal(err)
						}
						if err := tx.Commit(); err != nil {
							t.Fatal(err)
						}
					}
					var badCount int
					if err := conn.QueryRow(`SELECT COUNT(*) FROM notes WHERE id = 'bad'`).Scan(&badCount); err != nil {
						t.Fatal(err)
					}
					wantBad := 0
					if arm == "conflict update child" {
						wantBad = 1
					}
					if badCount != wantBad {
						t.Errorf("refused upsert left %d bad children, want %d", badCount, wantBad)
					}
					if wantBad == 1 {
						var body string
						if err := conn.QueryRow(`SELECT body FROM notes WHERE id = 'bad'`).Scan(&body); err != nil || body != "before-child" {
							t.Errorf("child = %q (%v), want before-child", body, err)
						}
					}
					if conflict {
						var title, noteID string
						if err := conn.QueryRow(`SELECT title,note_id FROM posts WHERE id = 'parent'`).Scan(&title, &noteID); err != nil || title != "before" || noteID != "ok-before" {
							t.Errorf("parent = %q/%q (%v), want before/ok-before", title, noteID, err)
						}
					} else {
						if err := conn.QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&count); err != nil || count != 0 {
							t.Errorf("refused insert left %d parents (%v)", count, err)
						}
					}
					// A valid cascade still writes either arm, proving the refusal is
					// about the generated FK rather than a blanket cascade refusal.
					ch.DB = conn
					notes.Config.Fields[0].Default = "ok-child"
					row, err := ch.UpsertOne(context.Background(), map[string]any{"id": "parent", "title": "valid", "lead_row": map[string]any{"body": "valid-child"}})
					if err != nil || row["note_id"] != "ok-child" {
						t.Fatalf("valid cascade = %v (%v), want ok-child", row, err)
					}
				})
			})
		}
	}
}
