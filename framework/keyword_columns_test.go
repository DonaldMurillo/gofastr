package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// A Go-declared entity may name its fields with SQL keywords. The migrator
// quotes such a name (query.QuoteFolded) and every statement CRUD writes
// must quote it the same way: written bare, INSERT INTO t (order, …) is a
// syntax error on both engines, and SELECT user FROM t on Postgres reads
// the session user, not the column. Each step below goes through the
// generated HTTP routes, on SQLite and on Postgres, and touches a keyword
// column in a different statement shape: the insert, the key read, a
// filter, a sort, the owner, tenant and soft-delete scopes, an include
// whose relation is keyed on a keyword column, a keyset cursor ordered by
// one, and a table whose own name is a keyword.

type kwUser struct{ ID string }

func (u kwUser) GetID() string { return u.ID }

func kwApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("kwitems", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "order", Type: schema.Int, Required: true},
			{Name: "group", Type: schema.String},
			{Name: "user", Type: schema.String},
			{Name: "end", Type: schema.String},
			{Name: "desc", Type: schema.String},
			{Name: "limit", Type: schema.Int},
		},
		Scope: &entity.ScopeConfig{
			SoftDelete:  true,
			OwnerField:  "user",
			MultiTenant: true,
			TenantField: "end",
		},
		SearchFields: []string{"desc", "group"},
		Pagination:   &entity.PaginationConfig{CursorField: "order", MaxListLimit: 2},
		Relations:    []entity.Relation{entity.HasMany("lines", "order", "order")},
	}.WithTimestamps(false))
	// The child's own table is a keyword, and so is its foreign key.
	app.Entity("order", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "order", Type: schema.String},
			{Name: "select", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("item", "kwitems", "order")},
		Exposure:  &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		if u, ok := handler.GetUser(ctx); ok {
			if k, ok := u.(kwUser); ok {
				return k.ID, true
			}
		}
		return nil, false
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })
	return app
}

// kwDo serves one request as user alice in tenant t1 and decodes the JSON
// body (nil when there is none), a record envelope's data unwrapped.
func kwDo(t *testing.T, app *App, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	ctx := handler.SetUser(req.Context(), kwUser{ID: "alice"})
	ctx = tenant.SetTenantID(ctx, "t1")
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req.WithContext(ctx))
	var out map[string]any
	if s := strings.TrimSpace(rec.Body.String()); s != "" {
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			t.Fatalf("%s %s = %d, body not JSON: %s", method, path, rec.Code, s)
		}
	}
	if data, ok := out["data"].(map[string]any); ok {
		out = data
	}
	if rec.Code >= 400 {
		t.Fatalf("%s %s = %d: %s", method, path, rec.Code, rec.Body.String())
	}
	return rec.Code, out
}

// kwRows reads the data array of a list response.
func kwRows(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	raw, ok := out["data"].([]any)
	if !ok {
		t.Fatalf("list response has no data array: %v", out)
	}
	rows := make([]map[string]any, len(raw))
	for i, r := range raw {
		rows[i] = r.(map[string]any)
	}
	return rows
}

// kwField reads one field of each row, in order, as its JSON text.
func kwField(rows []map[string]any, f string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		b, _ := json.Marshal(r[f])
		out[i] = string(b)
	}
	return out
}

func TestKeywordColumnsCRUD(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := kwApp(t, db)

		var ids []string
		for _, b := range []string{
			`{"order":3,"group":"g1","desc":"c","limit":30}`,
			`{"order":1,"group":"g1","desc":"a","limit":10}`,
			`{"order":2,"group":"g2","desc":"b","limit":20}`,
		} {
			code, row := kwDo(t, app, http.MethodPost, "/kwitems", b)
			if code != http.StatusCreated {
				t.Fatalf("create = %d, want 201: %v", code, row)
			}
			if row["user"] != "alice" || row["end"] != "t1" {
				t.Fatalf("create did not stamp owner and tenant: %v", row)
			}
			ids = append(ids, row["id"].(string))
		}

		_, got := kwDo(t, app, http.MethodGet, "/kwitems/"+ids[0], "")
		if got["order"] != float64(3) || got["group"] != "g1" || got["user"] != "alice" || got["desc"] != "c" {
			t.Fatalf("get = %v", got)
		}

		_, list := kwDo(t, app, http.MethodGet, "/kwitems?sort=-order&page=1", "")
		if o := kwField(kwRows(t, list), "order"); !slices.Equal(o, []string{"3", "2"}) {
			t.Fatalf("sort=-order page 1 = %v, want [3 2]", o)
		}
		_, list = kwDo(t, app, http.MethodGet, "/kwitems?group=g1&sort=desc", "")
		if d := kwField(kwRows(t, list), "desc"); !slices.Equal(d, []string{`"a"`, `"c"`}) {
			t.Fatalf("group=g1 sort=desc = %v, want [a c]", d)
		}
		_, list = kwDo(t, app, http.MethodGet, "/kwitems?order[gt]=1&user=alice&end=t1&desc[in]=b,c", "")
		if d := kwField(kwRows(t, list), "desc"); len(d) != 2 {
			t.Fatalf("filter on keyword columns = %v, want 2 rows", d)
		}

		where := url.QueryEscape(`{"or":[{"field":"limit","op":"gte","value":"30"},{"field":"desc","op":"like","value":"A"}]}`)
		_, list = kwDo(t, app, http.MethodGet, "/kwitems?where="+where+"&sort=-limit", "")
		if l := kwField(kwRows(t, list), "limit"); !slices.Equal(l, []string{"30", "10"}) {
			t.Fatalf("?where= on keyword columns = %v, want [30 10]", l)
		}
		_, list = kwDo(t, app, http.MethodGet, "/kwitems?q=g2", "")
		if d := kwField(kwRows(t, list), "desc"); !slices.Equal(d, []string{`"b"`}) {
			t.Fatalf("?q=g2 over keyword search fields = %v, want [b]", d)
		}

		// Keyset pages ordered by order, two rows per page (MaxListLimit).
		var seen []string
		cursor := ""
		for range 4 {
			_, page := kwDo(t, app, http.MethodGet, "/kwitems?cursor="+url.QueryEscape(cursor), "")
			seen = append(seen, kwField(kwRows(t, page), "order")...)
			if more, _ := page["hasMore"].(bool); !more {
				break
			}
			cursor, _ = page["cursor"].(string)
		}
		if !slices.Equal(seen, []string{"1", "2", "3"}) {
			t.Fatalf("cursor pages ordered by order = %v, want [1 2 3]", seen)
		}

		_, upd := kwDo(t, app, http.MethodPatch, "/kwitems/"+ids[1], `{"order":9,"group":"g9","desc":"z","limit":90}`)
		if upd["order"] != float64(9) || upd["group"] != "g9" || upd["limit"] != float64(90) {
			t.Fatalf("patch = %v", upd)
		}
		_, upd = kwDo(t, app, http.MethodPut, "/kwitems/"+ids[1], `{"order":8,"group":"g8","desc":"y","limit":80}`)
		if upd["order"] != float64(8) || upd["desc"] != "y" {
			t.Fatalf("put = %v", upd)
		}

		// A line keyed to item ids[0] through its keyword column "order".
		code, line := kwDo(t, app, http.MethodPost, "/order", `{"order":"`+ids[0]+`","select":"first"}`)
		if code != http.StatusCreated {
			t.Fatalf("create line = %d: %v", code, line)
		}
		_, inc := kwDo(t, app, http.MethodGet, "/kwitems/"+ids[0]+"?include=lines", "")
		lines, _ := inc["lines"].([]any)
		if len(lines) != 1 || lines[0].(map[string]any)["select"] != "first" {
			t.Fatalf("include=lines = %v", inc)
		}
		_, list = kwDo(t, app, http.MethodGet, "/kwitems?lines.select=first", "")
		if d := kwField(kwRows(t, list), "desc"); !slices.Equal(d, []string{`"c"`}) {
			t.Fatalf("nested filter through the keyword foreign key = %v, want [c]", d)
		}
		_, list = kwDo(t, app, http.MethodGet, "/order?include=item&select=first", "")
		rows := kwRows(t, list)
		item, _ := rows[0]["item"].(map[string]any)
		if len(rows) != 1 || item["desc"] != "c" {
			t.Fatalf("include=item = %v", rows)
		}
		_, list = kwDo(t, app, http.MethodGet, "/kwitems?include=lines&sort=order", "")
		if rows := kwRows(t, list); len(rows) != 2 || list["total"] != float64(3) || len(rows[1]["lines"].([]any)) != 1 {
			t.Fatalf("list with include = %v", list)
		}
		code, _ = kwDo(t, app, http.MethodDelete, "/order/"+line["id"].(string), "")
		if code != http.StatusNoContent {
			t.Fatalf("delete line = %d", code)
		}

		// Soft delete hides the row; ?trashed=only finds it.
		code, _ = kwDo(t, app, http.MethodDelete, "/kwitems/"+ids[2], "")
		if code != http.StatusNoContent {
			t.Fatalf("delete = %d, want 204", code)
		}
		_, list = kwDo(t, app, http.MethodGet, "/kwitems?sort=order", "")
		if o := kwField(kwRows(t, list), "order"); !slices.Equal(o, []string{"3", "8"}) {
			t.Fatalf("after soft delete = %v, want [3 8]", o)
		}
		_, list = kwDo(t, app, http.MethodGet, "/kwitems?trashed=true&sort=order", "")
		if o := kwField(kwRows(t, list), "order"); !slices.Equal(o, []string{"2", "3"}) || list["total"] != float64(3) {
			t.Fatalf("trashed=true = %v (total %v), want [2 3] of 3", o, list["total"])
		}

		code, _ = kwDo(t, app, http.MethodPost, "/kwitems/_batch", `{"items":[{"order":4,"group":"g4","desc":"d","limit":40}]}`)
		if code >= 300 {
			t.Fatalf("batch create = %d", code)
		}
		_, list = kwDo(t, app, http.MethodGet, "/kwitems?group=g4", "")
		if o := kwField(kwRows(t, list), "order"); !slices.Equal(o, []string{"4"}) {
			t.Fatalf("after batch create = %v, want [4]", o)
		}
	})
}
