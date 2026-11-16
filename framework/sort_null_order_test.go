package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// Every read surface that takes a sort orders a nullable column the same
// way on SQLite and Postgres: nulls last ascending, first descending.
func TestSortNullOrderMatchesAcrossDialects(t *testing.T) {
	const asc, desc = "two,ten,none", "none,ten,two"
	type row struct {
		Title string `json:"title"`
	}
	views := entity.NewIntColumn("views")
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table: "posts", Exposure: &entity.ExposureConfig{Public: true},
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "views", Type: schema.Int},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		ta := TestHarness(t, app)
		for _, body := range []map[string]any{
			{"title": "ten", "views": 10},
			{"title": "none"},
			{"title": "two", "views": 2},
		} {
			ta.Post("/posts", body).AssertStatus(t, http.StatusCreated)
		}
		ch, err := app.CrudHandler("posts")
		if err != nil {
			t.Fatalf("crud handler: %v", err)
		}
		ctx := context.Background()
		titles := func(rows []map[string]any) string {
			var out []string
			for _, r := range rows {
				out = append(out, fmt.Sprint(r["title"]))
			}
			return strings.Join(out, ",")
		}
		httpList := func(extra string) func(bool) (string, error) {
			return func(descending bool) (string, error) {
				sort := "views"
				if descending {
					sort = "-views"
				}
				resp := ta.Get("/posts?sort=" + sort + extra)
				if resp.recorder.Code != http.StatusOK {
					return "", fmt.Errorf("status %d: %s", resp.recorder.Code, resp.Body())
				}
				var page struct {
					Data []map[string]any `json:"data"`
				}
				if err := json.Unmarshal(resp.recorder.Body.Bytes(), &page); err != nil {
					return "", err
				}
				return titles(page.Data), nil
			}
		}
		sites := []struct {
			name string
			read func(descending bool) (string, error)
		}{
			{"offset list", httpList("")},
			{"stream", httpList("&stream=true")},
			{"ListAll", func(descending bool) (string, error) {
				rows, err := ch.ListAll(ctx, crud.ListOptions{Sorts: []filter.ParsedSort{{Field: "views", Desc: descending}}})
				return titles(rows), err
			}},
			{"TypedQuery.Order", func(descending bool) (string, error) {
				o := views.Asc()
				if descending {
					o = views.Desc()
				}
				found, err := crud.NewTypedQuery[row](ch).Order(o).Find(ctx)
				var out []string
				for _, r := range found {
					out = append(out, r.Title)
				}
				return strings.Join(out, ","), err
			}},
			{"dsl order()", func(descending bool) (string, error) {
				dir := "ASC"
				if descending {
					dir = "DESC"
				}
				qb, err := dsl.BuildDSLQuery(app.Registry, "posts.order(views "+dir+")")
				if err != nil {
					return "", err
				}
				stmt, args := qb.Build()
				rows, err := db.QueryContext(ctx, stmt, args...)
				if err != nil {
					return "", err
				}
				defer rows.Close()
				var out []string
				for rows.Next() {
					var id, title string
					var v sql.NullInt64
					if err := rows.Scan(&id, &title, &v); err != nil {
						return "", err
					}
					out = append(out, title)
				}
				return strings.Join(out, ","), rows.Err()
			}},
		}
		for _, site := range sites {
			for _, descending := range []bool{false, true} {
				want := asc
				if descending {
					want = desc
				}
				got, err := site.read(descending)
				if err != nil {
					t.Errorf("%s (desc=%v): %v", site.name, descending, err)
				} else if got != want {
					t.Errorf("%s (desc=%v) = %s, want %s", site.name, descending, got, want)
				}
			}
		}

		groups, err := ch.GroupCountAll(ctx, "views", crud.ListOptions{}, 0)
		if err != nil {
			t.Fatalf("GroupCountAll: %v", err)
		}
		var got []string
		for _, g := range groups {
			got = append(got, fmt.Sprint(g.Value))
		}
		if s := strings.Join(got, ","); s != "2,10,<nil>" {
			t.Errorf("GroupCountAll groups = %s, want 2,10,<nil>", s)
		}
	})
}
