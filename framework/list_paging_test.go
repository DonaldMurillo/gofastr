package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

// walkCursor follows ?cursor= from the first page until hasMore is false and
// returns every id it saw, failing on a repeat or a walk that never ends.
func walkCursor(t *testing.T, ta *TestApp, path string, limit int) []string {
	t.Helper()
	var ids []string
	seen := map[string]bool{}
	cursor := ""
	for hop := 0; ; hop++ {
		if hop > 20 {
			t.Fatalf("cursor walk did not terminate after %d pages (ids=%v)", hop, ids)
		}
		resp := ta.Get(fmt.Sprintf("%s?cursor=%s&limit=%d", path, url.QueryEscape(cursor), limit))
		resp.AssertStatus(t, http.StatusOK)
		page := decodeCursorPage(t, resp.Body())
		for _, row := range page.Data {
			id := fmt.Sprint(row["id"])
			if seen[id] {
				t.Fatalf("page %d repeats id %s (ids so far %v)", hop, id, ids)
			}
			seen[id] = true
			ids = append(ids, id)
		}
		if !page.HasMore {
			return ids
		}
		if page.Cursor == "" {
			t.Fatalf("page %d has more rows but no cursor", hop)
		}
		cursor = page.Cursor
	}
}

// The setup docs/content/cursor-pagination.md recommends: timestamps on,
// keyset on created_at (alone, or with the id tiebreak). A time.Time used to
// reach the token as JSON text with its quotes, which SQLite compared as
// '"2026-…' and matched every row, so the walk never ended.
func TestCursorWalksCreatedAtKeyset(t *testing.T) {
	for _, tc := range []struct {
		name string
		pg   entity.PaginationConfig
	}{
		{"composite", entity.PaginationConfig{CursorFields: []string{"created_at", "id"}}},
		{"single", entity.PaginationConfig{CursorField: "created_at"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
				pg := tc.pg
				app := NewApp(WithDB(db), WithoutDefaultMiddleware())
				app.Entity("notes", entity.EntityConfig{
					Table:      "notes",
					Pagination: &pg,
					Fields:     []schema.Field{{Name: "title", Type: schema.String, Required: true}},
				}.WithTimestamps(true))
				if err := AutoMigrate(db, app.Registry); err != nil {
					t.Fatalf("automigrate: %v", err)
				}
				ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
				for i := range 5 {
					ta.Post("/notes", map[string]any{"title": fmt.Sprintf("n%d", i)}).AssertStatus(t, http.StatusCreated)
				}
				if ids := walkCursor(t, ta, "/notes", 2); len(ids) != 5 {
					t.Fatalf("walked %d rows, want 5: %v", len(ids), ids)
				}
			})
		})
	}
}

// A timestamp cursor value is the fixed-width layout the framework stores,
// so SQLite's text comparison and Postgres's TIMESTAMPTZ comparison agree.
func TestCursorTimestampTokenFixedWidth(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("notes", entity.EntityConfig{
			Table:      "notes",
			Pagination: &entity.PaginationConfig{CursorField: "created_at"},
			Fields:     []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		}.WithTimestamps(true))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		for i := range 3 {
			ta.Post("/notes", map[string]any{"title": fmt.Sprintf("n%d", i)}).AssertStatus(t, http.StatusCreated)
		}
		page := decodeCursorPage(t, ta.Get("/notes?cursor=&limit=1").Body())
		field, value, err := pagination.DecodeCursor(page.Cursor)
		if err != nil || field != "created_at" {
			t.Fatalf("decode cursor: field=%q err=%v", field, err)
		}
		// 2006-01-02T15:04:05.000000Z: 27 bytes, no quotes.
		if len(value) != 27 || strings.ContainsRune(value, '"') || !strings.HasSuffix(value, "Z") {
			t.Fatalf("cursor value %q is not the fixed-width UTC layout", value)
		}
	})
}

// A whole-second time is where a trimming layout (RFC3339Nano) goes wrong:
// the cursor "…07Z" sorts after the stored "…07.500000Z" as text ('.' <
// 'Z'), so SQLite skipped the half-second row.
func TestCursorWalksWholeSecondTimes(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("notes", entity.EntityConfig{
			Table:      "notes",
			Pagination: &entity.PaginationConfig{CursorFields: []string{"created_at", "id"}},
			Fields:     []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		}.WithTimestamps(true))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		base := time.Date(2026, 10, 27, 7, 5, 7, 0, time.UTC)
		for i := range 5 {
			at := base.Add(time.Duration(i) * 500 * time.Millisecond).Format("2006-01-02T15:04:05.000000Z07:00")
			if _, err := db.Exec("INSERT INTO notes(id, title, created_at, updated_at) VALUES ($1, $2, $3, $3)",
				fmt.Sprintf("n%d", i), "t", at); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		if ids := walkCursor(t, ta, "/notes", 1); len(ids) != 5 {
			t.Fatalf("walked %d rows, want 5: %v", len(ids), ids)
		}
	})
}

// tieTicketsApp seeds n tickets whose status takes three values, inserted in
// shuffled id order so neither heap order nor rowid order matches the key.
func tieTicketsApp(t *testing.T, db *sql.DB, n int) (*TestApp, *crud.CrudHandler) {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("tickets", entity.EntityConfig{
		Table:  "tickets",
		Fields: []schema.Field{{Name: "status", Type: schema.String, Required: true}},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	order := rand.New(rand.NewSource(7)).Perm(n)
	statuses := []string{"open", "closed", "pending"}
	for _, i := range order {
		if _, err := db.Exec("INSERT INTO tickets(id, status) VALUES ($1, $2)",
			fmt.Sprintf("t%04d", i), statuses[i%len(statuses)]); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	ch, err := app.CrudHandler("tickets")
	if err != nil {
		t.Fatalf("crud handler: %v", err)
	}
	return TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"}), ch
}

// walkOffset reads pages 1..pages of path (which carries its own query) and
// returns the ids in the order served.
func walkOffset(t *testing.T, ta *TestApp, path string, pages int) []string {
	t.Helper()
	var ids []string
	for p := 1; p <= pages; p++ {
		resp := ta.Get(fmt.Sprintf("%s&page=%d", path, p))
		resp.AssertStatus(t, http.StatusOK)
		if strings.Contains(path, "stream=true") {
			var body struct {
				Data []map[string]any `json:"data"`
			}
			if err := json.Unmarshal([]byte(resp.Body()), &body); err != nil {
				t.Fatalf("decode stream page %d: %v", p, err)
			}
			for _, row := range body.Data {
				ids = append(ids, fmt.Sprint(row["id"]))
			}
			continue
		}
		var body ListResponse
		if err := json.Unmarshal([]byte(resp.Body()), &body); err != nil {
			t.Fatalf("decode page %d: %v", p, err)
		}
		for _, row := range body.Data {
			ids = append(ids, fmt.Sprint(row["id"]))
		}
	}
	return ids
}

func assertDistinct(t *testing.T, ids []string, want int) {
	t.Helper()
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if len(ids) != want || len(seen) != want {
		t.Fatalf("served %d ids, %d distinct, want %d distinct", len(ids), len(seen), want)
	}
}

// Sorting on a column with ties used to leave the order inside a tie to the
// planner: Postgres's top-N sort served some rows on two pages and others on
// none. The primary key now breaks every tie.
func TestOffsetSortOnTiesCoversAll(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta, _ := tieTicketsApp(t, db, 300)
		assertDistinct(t, walkOffset(t, ta, "/tickets?sort=status&limit=10", 30), 300)
		assertDistinct(t, walkOffset(t, ta, "/tickets?sort=-status&limit=10", 30), 300)
		assertDistinct(t, walkOffset(t, ta, "/tickets?sort=status&limit=10&stream=true", 30), 300)
	})
}

// With no ?sort= the list had no ORDER BY at all; it now pages in key order.
func TestOffsetNoSortPagesInKeyOrder(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta, _ := tieTicketsApp(t, db, 300)
		for _, path := range []string{"/tickets?limit=10", "/tickets?limit=10&stream=true"} {
			ids := walkOffset(t, ta, path, 30)
			assertDistinct(t, ids, 300)
			for i := 1; i < len(ids); i++ {
				if ids[i-1] >= ids[i] {
					t.Fatalf("%s: id %s served before %s", path, ids[i-1], ids[i])
				}
			}
		}
	})
}

// An entity with timestamps lists unsorted rows oldest first, the order
// they were written, not the order of their random ids.
func TestNoSortListsOldestFirst(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("memos", entity.EntityConfig{
			Table:  "memos",
			Fields: []schema.Field{{Name: "body", Type: schema.String, Required: true}},
		})
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		var want []string
		for i, id := range []string{"m9", "m3", "m7", "m1", "m5"} {
			at := base.Add(time.Duration(i) * time.Minute).Format("2006-01-02T15:04:05.000000Z07:00")
			if _, err := db.Exec("INSERT INTO memos(id, body, created_at, updated_at) VALUES ($1, $2, $3, $3)", id, "x", at); err != nil {
				t.Fatalf("seed: %v", err)
			}
			want = append(want, id)
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		for _, path := range []string{"/memos?limit=2", "/memos?limit=2&stream=true"} {
			got := walkOffset(t, ta, path, 3)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("%s served %v, want oldest first %v", path, got, want)
			}
		}
	})
}

// ListAll (the admin list's reader) pages by Limit/Offset too.
func TestListAllSortOnTiesCoversAll(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := tieTicketsApp(t, db, 300)
		var ids []string
		for p := range 30 {
			rows, err := ch.ListAll(context.Background(), crud.ListOptions{
				Sorts: []filter.ParsedSort{{Field: "status"}},
				Limit: 10, Offset: p * 10,
			})
			if err != nil {
				t.Fatalf("ListAll page %d: %v", p, err)
			}
			for _, row := range rows {
				ids = append(ids, fmt.Sprint(row["id"]))
			}
		}
		assertDistinct(t, ids, 300)

		// A typed repository's Offset pages the same rows.
		type ticket struct {
			ID string `json:"id"`
		}
		ids = ids[:0]
		for p := range 30 {
			got, err := crud.NewTypedQuery[ticket](ch).Order(entity.NewStringColumn("status").Asc()).
				Limit(10).Offset(p * 10).Find(context.Background())
			if err != nil {
				t.Fatalf("typed page %d: %v", p, err)
			}
			for _, row := range got {
				ids = append(ids, row.ID)
			}
		}
		assertDistinct(t, ids, 300)
	})
}

// ListAll refuses a mistyped filter value the way the HTTP parser does.
func TestListAllFilterWrongTypeErrs(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := typedFilterApp(t, db)
		ch, err := ta.App.CrudHandler("gauges")
		if err != nil {
			t.Fatalf("crud handler: %v", err)
		}
		_, err = ch.ListAll(context.Background(), crud.ListOptions{
			Filters: []filter.ParsedFilter{{Field: "n", Op: filter.OpEq, Value: "abc"}},
		})
		if err == nil || !strings.Contains(err.Error(), "not a valid int") {
			t.Fatalf("ListAll n=abc: err = %v, want a not-a-valid-int refusal", err)
		}
		rows, err := ch.ListAll(context.Background(), crud.ListOptions{
			Filters: []filter.ParsedFilter{{Field: "n", Op: filter.OpEq, Value: "5"}},
		})
		if err != nil || len(rows) != 1 {
			t.Fatalf("ListAll n=5: %d rows, err %v; want 1", len(rows), err)
		}
	})
}

// typedFilterApp is an entity with one column of every type a filter value
// is checked against.
func typedFilterApp(t *testing.T, db *sql.DB) *TestApp {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("gauges", entity.EntityConfig{
		Table: "gauges",
		Fields: []schema.Field{
			{Name: "n", Type: schema.Int},
			{Name: "ratio", Type: schema.Float},
			{Name: "price", Type: schema.Decimal},
			{Name: "active", Type: schema.Bool},
			{Name: "due", Type: schema.Date},
			{Name: "status", Type: schema.String},
			{Name: "payload", Type: schema.JSON},
		},
	}.WithTimestamps(true))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
	ta.Post("/gauges", map[string]any{"n": 5, "ratio": 0.5, "price": "1.25", "active": true, "due": "2026-10-01", "status": "Open"}).
		AssertStatus(t, http.StatusCreated)
	return ta
}

// A value that is not of the column's type is a client error on both
// dialects. It used to reach the bind: Postgres answered 500 (22P02) and
// SQLite 200 with no rows.
func TestFilterValueWrongTypeIs400(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := typedFilterApp(t, db)
		where := func(js string) string { return "where=" + url.QueryEscape(js) }
		for _, tc := range []struct{ query, field string }{
			{"n=abc", "n"},
			{"n_gt=1.5", "n"},
			{"n_in=1,abc", "n"},
			{"n_ne=1e3", "n"},
			{"ratio=abc", "ratio"},
			{"ratio_gt=NaN", "ratio"},
			{"price=0x10", "price"},
			{"active=yes", "active"},
			{"active_in=true,maybe", "active"},
			{"due=2026-13-45", "due"},
			{"created_at_gt=yesterday", "created_at"},
			{"payload=not-json", "payload"},
			{"payload_in=1,{", "payload"},
			{where(`{"field":"n","value":"abc"}`), "n"},
			{where(`{"field":"n","op":"gt","value":"1.5"}`), "n"},
			{where(`{"field":"n","op":"in","values":["1","x"]}`), "n"},
			{where(`{"field":"active","value":"yes"}`), "active"},
		} {
			resp := ta.Get("/gauges?" + tc.query)
			if resp.Status() != http.StatusBadRequest {
				t.Errorf("?%s = %d, want 400 (%s)", tc.query, resp.Status(), resp.Body())
				continue
			}
			if !strings.Contains(resp.Body(), `\"`+tc.field+`\"`) {
				t.Errorf("?%s: refusal does not name %q: %s", tc.query, tc.field, resp.Body())
			}
		}
		// Well-typed values still filter.
		for _, q := range []string{"n=5", "n_gt=4", "n_in=5,6", "ratio_lt=1", "price=1.25", "active=true",
			"active=1", "due=2026-10-01", "created_at_gt=2020-01-01", "created_at_gt=2020-01-01T00:00:00Z"} {
			var body ListResponse
			resp := ta.Get("/gauges?" + q)
			resp.AssertStatus(t, http.StatusOK)
			if err := json.Unmarshal([]byte(resp.Body()), &body); err != nil || len(body.Data) != 1 {
				t.Errorf("?%s matched %d rows, want 1 (err=%v)", q, len(body.Data), err)
			}
		}
	})
}

// A tampered cursor value on an integer key is refused like a filter value.
func TestCursorTamperedIntKeyIs400(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("counters", entity.EntityConfig{
			Table:      "counters",
			PrimaryKey: []string{"seq"},
			Fields: []schema.Field{
				{Name: "seq", Type: schema.Int, AutoGenerate: schema.AutoIncrement, ReadOnly: true},
				{Name: "label", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		for i := range 3 {
			ta.Post("/counters", map[string]any{"label": fmt.Sprint(i)}).AssertStatus(t, http.StatusCreated)
		}
		ok := ta.Get("/counters?cursor=" + url.QueryEscape(pagination.EncodeCursor("seq", 1)) + "&limit=1")
		ok.AssertStatus(t, http.StatusOK)
		bad := ta.Get("/counters?cursor=" + url.QueryEscape(pagination.EncodeCursor("seq", "abc")) + "&limit=1")
		if bad.Status() != http.StatusBadRequest {
			t.Fatalf("tampered cursor = %d, want 400: %s", bad.Status(), bad.Body())
		}
	})
}

// _like is case-insensitive on both dialects, as ?q= is. Postgres LIKE used
// to be case-sensitive while SQLite's folded ASCII case.
func TestFilterLikeIgnoresCase(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := typedFilterApp(t, db)
		for _, q := range []string{
			"status_like=open", "status_like=OPEN", "status_like=pE",
			"where=" + url.QueryEscape(`{"field":"status","op":"like","value":"oPEN"}`),
		} {
			var body ListResponse
			resp := ta.Get("/gauges?" + q)
			resp.AssertStatus(t, http.StatusOK)
			if err := json.Unmarshal([]byte(resp.Body()), &body); err != nil || len(body.Data) != 1 {
				t.Errorf("?%s matched %d rows, want 1 (err=%v)", q, len(body.Data), err)
			}
		}
		// The wildcard escape still holds: % is a literal, not "anything".
		var body ListResponse
		resp := ta.Get("/gauges?status_like=" + url.QueryEscape("o%n"))
		resp.AssertStatus(t, http.StatusOK)
		if err := json.Unmarshal([]byte(resp.Body()), &body); err != nil || len(body.Data) != 0 {
			t.Errorf("?status_like=o%%n matched %d rows, want 0", len(body.Data))
		}
	})
}

// A field whose name ends in an operator suffix (opt_in, check_in) is a
// plain field when the cut name is not one. The top-level parser fell back
// to the whole name; ?include=rel(…) and ?rel.field= cut the suffix and
// refused the field as unknown.
func TestSuffixNamedFieldFiltersEverywhere(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("rosters", entity.EntityConfig{
			Table:     "rosters",
			Fields:    []schema.Field{{Name: "name", Type: schema.String, Required: true}},
			Relations: []entity.Relation{entity.HasMany("members", "members", "roster_id")},
		}.WithTimestamps(false))
		app.Entity("members", entity.EntityConfig{
			Table: "members",
			Fields: []schema.Field{
				{Name: "roster_id", Type: schema.String, Required: true},
				{Name: "opt_in", Type: schema.Bool},
				{Name: "check_in", Type: schema.String},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		for _, stmt := range []string{
			"INSERT INTO rosters(id, name) VALUES ('r1', 'one'), ('r2', 'two')",
			"INSERT INTO members(id, roster_id, opt_in, check_in) VALUES ('m1', 'r1', TRUE, 'Morning'), ('m2', 'r1', FALSE, 'noon'), ('m3', 'r2', FALSE, 'noon')",
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
		if n := count("/members?opt_in=true"); n != 1 {
			t.Fatalf("?opt_in=true matched %d members, want 1", n)
		}
		if n := count("/members?opt_in_in=true,false&check_in_like=noon"); n != 2 {
			t.Fatalf("?opt_in_in&check_in_like matched %d members, want 2", n)
		}
		if n := count("/rosters?members.opt_in=true"); n != 1 {
			t.Fatalf("?members.opt_in=true matched %d rosters, want 1", n)
		}
		if n := count("/rosters?members.check_in_like=MORN"); n != 1 {
			t.Fatalf("?members.check_in_like matched %d rosters, want 1", n)
		}
		resp := ta.Get("/rosters/r1?include=" + url.QueryEscape("members(opt_in=true,check_in_like=morn)"))
		resp.AssertStatus(t, http.StatusOK)
		var got map[string]any
		if err := json.Unmarshal([]byte(resp.Body()), singleMap(&got)); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if members, _ := got["members"].([]any); len(members) != 1 {
			t.Fatalf("include=members(opt_in=true,…) attached %d members, want 1: %v", len(members), got["members"])
		}
		// A mistyped value one hop away is the same 400.
		for _, path := range []string{
			"/rosters?members.opt_in=maybe",
			"/rosters/r1?include=" + url.QueryEscape("members(opt_in=maybe)"),
		} {
			if resp := ta.Get(path); resp.Status() != http.StatusBadRequest || !strings.Contains(resp.Body(), "not a valid bool") {
				t.Errorf("%s = %d, want 400 naming the type: %s", path, resp.Status(), resp.Body())
			}
		}
	})
}

// A Date keyset walks: the token carries the date the column holds, which
// the cursor check accepts. Both drivers read DATE as time.Time, and the
// token used to carry a full timestamp the Date check refused on page two.
func TestCursorWalksDateKeyset(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("shifts", entity.EntityConfig{
			Table:      "shifts",
			Pagination: &entity.PaginationConfig{CursorFields: []string{"due", "id"}},
			Fields:     []schema.Field{{Name: "due", Type: schema.Date, Required: true}},
		})
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		for _, d := range []string{"2026-10-27", "2026-10-28", "2026-10-28", "2026-10-29"} {
			ta.Post("/shifts", map[string]any{"due": d}).AssertStatus(t, http.StatusCreated)
		}
		if ids := walkCursor(t, ta, "/shifts", 1); len(ids) != 4 {
			t.Fatalf("walked %d rows, want 4: %v", len(ids), ids)
		}
	})
}

// A Timestamp the client wrote is stored in the layout the cursor carries,
// so SQLite's text comparison orders the two alike. "...00Z" stored raw
// sorted after the token "...00.000000Z" and the walk repeated page one.
func TestCursorWalksClientTimestamps(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("pings", entity.EntityConfig{
			Table:      "pings",
			Pagination: &entity.PaginationConfig{CursorField: "at"},
			Fields:     []schema.Field{{Name: "at", Type: schema.Timestamp, Required: true}},
		})
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		for _, at := range []string{"2026-10-27T00:00:00Z", "2026-10-27T02:00:01+02:00", "2026-10-27T00:00:02.5Z"} {
			ta.Post("/pings", map[string]any{"at": at}).AssertStatus(t, http.StatusCreated)
		}
		if ids := walkCursor(t, ta, "/pings", 1); len(ids) != 3 {
			t.Fatalf("walked %d rows, want 3: %v", len(ids), ids)
		}
	})
}
