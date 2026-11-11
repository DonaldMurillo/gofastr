package crud_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

type timestampPage struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	Cursor  string `json:"cursor"`
	HasMore bool   `json:"hasMore"`
}

// legacyTimestampHandler serves an Unmanaged entity over a fresh SQLite
// table whose at column (of columnType) holds the given text, one row per
// value, ids m1, m2, ….
func legacyTimestampHandler(t *testing.T, columnType string, stored []string) *crud.CrudHandler {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`CREATE TABLE logs (id TEXT PRIMARY KEY, at ` + columnType + ` NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for index, value := range stored {
		if _, err := database.Exec(`INSERT INTO logs (id, at) VALUES (?, ?)`, fmt.Sprintf("m%d", index+1), value); err != nil {
			t.Fatal(err)
		}
	}
	definition := entity.Define("logs", entity.EntityConfig{
		Table: "logs", Unmanaged: true,
		Fields:     []schema.Field{{Name: "at", Type: schema.Timestamp, Required: true}},
		Exposure:   &entity.ExposureConfig{Public: true},
		Pagination: &entity.PaginationConfig{CursorFields: []string{"at", "id"}},
	}.WithTimestamps(false))
	definition.SetDB(database)
	return crud.NewCrudHandler(definition, database).WithJSONCase(crud.CaseSnake)
}

func listTimestamps(handler *crud.CrudHandler, rawQuery string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/logs?"+rawQuery, nil)
	response := httptest.NewRecorder()
	handler.List()(response, request)
	return response
}

func timestampIDs(t *testing.T, response *httptest.ResponseRecorder) (timestampPage, []string) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("list: %d %s", response.Code, response.Body.String())
	}
	var page timestampPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	var identifiers []string
	for _, row := range page.Data {
		identifiers = append(identifiers, row.ID)
	}
	return page, identifiers
}

// cursorWalk pages through the list two rows at a time and returns every
// id in the order the pages served them.
func cursorWalk(t *testing.T, handler *crud.CrudHandler) []string {
	t.Helper()
	var walked []string
	cursor := ""
	for range 10 {
		page, identifiers := timestampIDs(t, listTimestamps(handler, "limit=2&cursor="+url.QueryEscape(cursor)))
		walked = append(walked, identifiers...)
		if !page.HasMore {
			return walked
		}
		cursor = page.Cursor
	}
	t.Fatalf("cursor walk did not finish: %v", walked)
	return nil
}

func canonicalTimestamps(t *testing.T, values []string) []string {
	t.Helper()
	var canonical []string
	for _, value := range values {
		parsed, err := time.Parse(time.DateTime, value)
		if err != nil {
			t.Fatal(err)
		}
		canonical = append(canonical, schema.FormatTimestamp(parsed))
	}
	return canonical
}

// An Unmanaged SQLite table another writer filled stores timestamps in
// its own layout. The keyset binds the anchor row's stored text, which is
// what ORDER BY sorts, so a cursor walk serves the sorted list once each:
// rebinding it in schema.TimestampLayout skipped m3,m4 of the
// space-separated rows and repeated a row per page of the RFC 3339 ones.
// A comparison filter cannot know the stored layout, so it refuses a
// column holding another one with a 500 naming the field instead of
// dropping or adding rows; canonical rows filter as before.
func TestLegacyTimestampLayouts(t *testing.T) {
	space := []string{"2026-03-03 08:00:00", "2026-03-03 09:00:00", "2026-03-03 10:00:00", "2026-03-03 11:00:00", "2026-03-04 08:00:00"}
	canonical := canonicalTimestamps(t, space)
	type layout struct {
		stored []string
		after  string
	}
	layouts := map[string]layout{
		"canonical": {canonical, space[1]},
		"space":     {space, space[1]},
		"rfc3339":   {[]string{"2026-01-01T12:00:00Z", "2026-01-02T12:00:00Z", "2026-01-03T12:00:00Z", "2026-01-04T12:00:00Z", "2026-01-05T12:00:00Z"}, "2026-01-02T12:00:00Z"},
		"sqlite":    {[]string{"2026-03-03 08:00:00+00:00", "2026-03-03 09:00:00+00:00", "2026-03-03 10:00:00+00:00", "2026-03-03 11:00:00+00:00", "2026-03-04 08:00:00+00:00"}, "2026-03-03T09:00:00Z"},
		"mixed":     {[]string{space[0], canonical[1], space[2], canonical[3], space[4]}, space[1]},
	}
	for _, columnType := range []string{"TEXT", "DATETIME"} {
		for _, name := range slices.Sorted(maps.Keys(layouts)) {
			stored, after := layouts[name].stored, layouts[name].after
			t.Run(columnType+"/"+name, func(t *testing.T) {
				handler := legacyTimestampHandler(t, columnType, stored)
				_, sorted := timestampIDs(t, listTimestamps(handler, "limit=10&sort=at,id"))
				if walked := cursorWalk(t, handler); !slices.Equal(walked, sorted) {
					t.Errorf("cursor walk %v, sorted list %v", walked, sorted)
				}
				keyOnly := pagination.EncodeCursor("id", sorted[1])
				if _, after := timestampIDs(t, listTimestamps(handler, "limit=2&cursor="+url.QueryEscape(keyOnly))); !slices.Equal(after, sorted[2:4]) {
					t.Errorf("page after key-only cursor %s: %v, want %v", sorted[1], after, sorted[2:4])
				}
				for _, rawQuery := range []string{
					"limit=10&sort=at&at_gt=" + url.QueryEscape(after),
					"limit=10&sort=at&where=" + url.QueryEscape(fmt.Sprintf(`{"field":"at","op":"gt","value":%q}`, after)),
				} {
					response := listTimestamps(handler, rawQuery)
					if name == "canonical" {
						if _, filtered := timestampIDs(t, response); !slices.Equal(filtered, []string{"m3", "m4", "m5"}) {
							t.Errorf("%s: %v, want m3 m4 m5", rawQuery, filtered)
						}
						continue
					}
					if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "timestamp_layout") {
						t.Errorf("%s: %d %s, want 500 timestamp_layout", rawQuery, response.Code, response.Body.String())
					}
				}
				_, err := handler.ListAll(t.Context(), crud.ListOptions{Filters: []filter.ParsedFilter{{Field: "at", Op: filter.OpGt, Value: after}}})
				if (name == "canonical") != (err == nil) {
					t.Errorf("ListAll at > %s: err = %v", after, err)
				}
			})
		}
	}
}

// A cursor value the anchor's stored text could not have held is still
// a 400 on a stored-text keyset, as it is on a canonical one.
func TestLegacyTimestampCursorRefusesNonTimestamp(t *testing.T) {
	handler := legacyTimestampHandler(t, "TEXT", []string{"2026-03-03 08:00:00", "2026-03-03 09:00:00"})
	cursor := pagination.EncodeMultiCursor([]string{"at", "id"}, map[string]any{"at": "yesterday", "id": "m1"})
	if response := listTimestamps(handler, "limit=1&cursor="+url.QueryEscape(cursor)); response.Code != http.StatusBadRequest {
		t.Errorf("cursor at=yesterday: %d %s, want 400", response.Code, response.Body.String())
	}
}

// Postgres compares a TIMESTAMPTZ column as an instant, so an Unmanaged
// table there keeps the canonical keyset and filter: no stored-text read
// and no layout check, which would be a SQLite GLOB.
func TestLegacyTimestampPostgresUnchanged(t *testing.T) {
	database := pgtest.DB(t)
	if _, err := database.Exec(`CREATE TABLE logs (id TEXT PRIMARY KEY, at TIMESTAMPTZ NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for index, value := range []string{"2026-03-03 08:00:00+00", "2026-03-03 09:00:00+00", "2026-03-03 10:00:00+00", "2026-03-03 11:00:00+00", "2026-03-04 08:00:00+00"} {
		if _, err := database.Exec(`INSERT INTO logs (id, at) VALUES ($1, $2)`, fmt.Sprintf("m%d", index+1), value); err != nil {
			t.Fatal(err)
		}
	}
	definition := entity.Define("logs", entity.EntityConfig{
		Table: "logs", Unmanaged: true,
		Fields:     []schema.Field{{Name: "at", Type: schema.Timestamp, Required: true}},
		Exposure:   &entity.ExposureConfig{Public: true},
		Pagination: &entity.PaginationConfig{CursorFields: []string{"at", "id"}},
	}.WithTimestamps(false))
	definition.SetDB(database)
	handler := crud.NewCrudHandler(definition, database).WithJSONCase(crud.CaseSnake)
	want := []string{"m1", "m2", "m3", "m4", "m5"}
	if walked := cursorWalk(t, handler); !slices.Equal(walked, want) {
		t.Errorf("cursor walk %v, want %v", walked, want)
	}
	if _, filtered := timestampIDs(t, listTimestamps(handler, "limit=10&sort=at&at_gt="+url.QueryEscape("2026-03-03 09:00:00"))); !slices.Equal(filtered, want[2:]) {
		t.Errorf("at_gt: %v, want %v", filtered, want[2:])
	}
}

// A TypedQuery binds a TimestampColumn comparison in schema.TimestampLayout
// too, so on a column holding "2026-03-03 10:00:00" its Gt(09:30) missed
// that row on Find and UpdateAll touched nothing, while ListAll refused.
// Every typed surface refuses the same way, through And, Or and Not; the
// canonical table still filters, and a comparison on another column or an
// IsNull test runs no check.
func TestTypedQueryRefusesLegacyTimestamps(t *testing.T) {
	space := []string{"2026-03-03 09:00:00", "2026-03-03 10:00:00"}
	at := entity.NewTimestampColumn("at")
	bound := time.Date(2026, 3, 3, 9, 30, 0, 0, time.UTC)
	surfaces := map[string]func(*crud.CrudHandler, entity.Condition) (int, error){
		"Find": func(h *crud.CrudHandler, c entity.Condition) (int, error) {
			rows, err := crud.NewTypedQuery[map[string]any](h).Where(c).Find(t.Context())
			return len(rows), err
		},
		"First": func(h *crud.CrudHandler, c entity.Condition) (int, error) {
			_, err := crud.NewTypedQuery[map[string]any](h).Where(c).First(t.Context())
			return 1, err
		},
		"Count": func(h *crud.CrudHandler, c entity.Condition) (int, error) {
			return crud.NewTypedQuery[map[string]any](h).Where(c).Count(t.Context())
		},
		"Exists": func(h *crud.CrudHandler, c entity.Condition) (int, error) {
			ok, err := crud.NewTypedQuery[map[string]any](h).Where(c).Exists(t.Context())
			if ok {
				return 1, err
			}
			return 0, err
		},
		"UpdateAll": func(h *crud.CrudHandler, c entity.Condition) (int, error) {
			return crud.NewTypedQuery[map[string]any](h).Where(c).UpdateAll(t.Context(), map[string]any{"at": "2026-03-03T12:00:00.000000Z"})
		},
		"DeleteAll": func(h *crud.CrudHandler, c entity.Condition) (int, error) {
			return crud.NewTypedQuery[map[string]any](h).Where(c).DeleteAll(t.Context())
		},
	}
	conditions := map[string]entity.Condition{
		"gt":  at.Gt(bound),
		"or":  entity.Or(at.Lt(bound), entity.NewStringColumn("id").Eq("m9")),
		"not": entity.Not(entity.And(at.Gte(bound))),
	}
	for _, surface := range slices.Sorted(maps.Keys(surfaces)) {
		for _, name := range slices.Sorted(maps.Keys(conditions)) {
			t.Run(surface+"/"+name, func(t *testing.T) {
				_, err := surfaces[surface](legacyTimestampHandler(t, "TEXT", space), conditions[name])
				if err == nil || !strings.Contains(err.Error(), "outside schema.TimestampLayout") {
					t.Errorf("err = %v, want the timestamp layout refusal", err)
				}
			})
		}
		t.Run(surface+"/canonical", func(t *testing.T) {
			n, err := surfaces[surface](legacyTimestampHandler(t, "TEXT", canonicalTimestamps(t, space)), at.Gt(bound))
			if err != nil || n != 1 {
				t.Errorf("canonical Gt(09:30): n = %d, err = %v, want 1 row", n, err)
			}
		})
		t.Run(surface+"/unchecked", func(t *testing.T) {
			if _, err := surfaces[surface](legacyTimestampHandler(t, "TEXT", space), entity.And(at.IsNotNull(), entity.NewStringColumn("id").Eq("m2"))); err != nil {
				t.Errorf("no timestamp comparison: err = %v", err)
			}
		})
	}
}
