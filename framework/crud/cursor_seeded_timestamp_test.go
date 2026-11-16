package crud_test

import (
	"database/sql"
	"fmt"
	"net/url"
	"slices"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// seededTimestamps are RFC 3339 "…Z" values a seed script or migration
// writes: with and without a fraction, two rows sharing an instant.
var seededTimestamps = []string{
	"2026-01-02T03:04:05Z",
	"2026-01-02T03:04:05.5Z",
	"2026-01-02T03:04:06Z",
	"2026-01-02T03:04:06Z",
	"2026-01-02T03:04:07.250Z",
	"2026-01-02T03:04:08Z",
	"2026-01-03T00:00:00Z",
}

func seededTimestampHandler(t *testing.T, database *sql.DB, columnType string, unmanaged bool) *crud.CrudHandler {
	t.Helper()
	if _, err := database.Exec(`CREATE TABLE logs (id TEXT PRIMARY KEY, created_at ` + columnType + ` NOT NULL, updated_at ` + columnType + `)`); err != nil {
		t.Fatal(err)
	}
	for index, value := range seededTimestamps {
		if _, err := database.Exec(`INSERT INTO logs (id, created_at, updated_at) VALUES ($1, $2, $3)`, fmt.Sprintf("m%d", index+1), value, value); err != nil {
			t.Fatal(err)
		}
	}
	definition := entity.Define("logs", entity.EntityConfig{
		Table: "logs", Unmanaged: unmanaged,
		Exposure:   &entity.ExposureConfig{Public: true},
		Pagination: &entity.PaginationConfig{CursorField: "created_at"},
	}.WithTimestamps(true))
	definition.SetDB(database)
	return crud.NewCrudHandler(definition, database).WithJSONCase(crud.CaseSnake)
}

// seededWalk pages two rows at a time in the given direction and returns
// every id the pages served.
func seededWalk(t *testing.T, handler *crud.CrudHandler, direction string) []string {
	t.Helper()
	var walked []string
	cursor := ""
	for range 10 {
		page, identifiers := timestampIDs(t, listTimestamps(handler, "limit=2&direction="+direction+"&cursor="+url.QueryEscape(cursor)))
		walked = append(walked, identifiers...)
		if !page.HasMore {
			return walked
		}
		cursor = page.Cursor
	}
	t.Fatalf("%s walk did not finish: %v", direction, walked)
	return nil
}

func servedOnce(t *testing.T, walk string, walked, want []string) {
	t.Helper()
	seen := map[string]int{}
	for _, id := range walked {
		seen[id]++
	}
	for _, id := range want {
		if seen[id] != 1 {
			t.Errorf("%s walk served %s %d times, want once: %v", walk, id, seen[id], walked)
		}
	}
	if len(walked) != len(want) {
		t.Errorf("%s walk served %d rows, want %d: %v", walk, len(walked), len(want), walked)
	}
}

// Seeded created_at text such as "2026-01-02T03:04:05Z" pages once per row on SQLite.
func TestCursorOverSeededTimestamps(t *testing.T) {
	for _, columnType := range []string{"TEXT", "DATETIME", "TIMESTAMP"} {
		for _, unmanaged := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/unmanaged=%v", columnType, unmanaged), func(t *testing.T) {
				database, err := sql.Open("sqlite3", ":memory:")
				if err != nil {
					t.Fatal(err)
				}
				database.SetMaxOpenConns(1)
				t.Cleanup(func() { database.Close() })
				handler := seededTimestampHandler(t, database, columnType, unmanaged)
				for _, id := range []string{"w1", "w2"} {
					if _, err := handler.CreateOne(t.Context(), map[string]any{"id": id}); err != nil {
						t.Fatal(err)
					}
				}
				_, sorted := timestampIDs(t, listTimestamps(handler, "limit=20&sort=created_at,id"))
				if len(sorted) != len(seededTimestamps)+2 {
					t.Fatalf("sorted list has %d rows, want %d", len(sorted), len(seededTimestamps)+2)
				}
				forward := seededWalk(t, handler, "forward")
				servedOnce(t, "forward", forward, sorted)
				if !slices.Equal(forward, sorted) {
					t.Errorf("forward walk %v, sorted list %v", forward, sorted)
				}
				servedOnce(t, "backward", seededWalk(t, handler, "backward"), sorted)
				keyOnly := url.QueryEscape(pagination.EncodeCursor("id", sorted[1]))
				if _, after := timestampIDs(t, listTimestamps(handler, "limit=2&cursor="+keyOnly)); !slices.Equal(after, sorted[2:4]) {
					t.Errorf("page after key-only cursor %s: %v, want %v", sorted[1], after, sorted[2:4])
				}
			})
		}
	}
}

// Postgres compares TIMESTAMPTZ as an instant, so the same seeded rows
// page in time order there with the canonical bind.
func TestCursorOverSeededTimestampsPostgres(t *testing.T) {
	handler := seededTimestampHandler(t, pgtest.DB(t), "TIMESTAMPTZ", false)
	want := []string{"m1", "m2", "m3", "m4", "m5", "m6", "m7"}
	forward := seededWalk(t, handler, "forward")
	if !slices.Equal(forward, want) {
		t.Errorf("forward walk %v, want %v", forward, want)
	}
	servedOnce(t, "backward", seededWalk(t, handler, "backward"), want)
}
