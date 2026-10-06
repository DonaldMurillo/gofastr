package crud

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// The operator/type rule (filter.CheckOpType) holds on every filter
// surface, not just ?where=: a like on a Bool or a range on a Bool is
// refused one relation hop away and inside an include scope too.

func TestNestedFilterRefusesMistypedOp(t *testing.T) {
	posts, reg := boolBindFixtures()
	for _, key := range []string{"author.active_like", "author.active_gt"} {
		_, err := parseNestedFiltersValues(url.Values{key: {"1"}}, posts, reg)
		if err == nil || !strings.Contains(err.Error(), `cannot filter "active"`) {
			t.Errorf("%s: want a type refusal, got %v", key, err)
		}
	}
	if _, err := parseNestedFiltersValues(url.Values{"author.name_like": {"a"}}, posts, reg); err != nil {
		t.Errorf("like on a string must pass: %v", err)
	}
}

func TestInProcessNestedRefusesMistypedOp(t *testing.T) {
	posts, reg := boolBindFixtures()
	_, err := resolveNestedFilters(posts, reg, []NestedFilter{{Relation: "author", Field: "active", Op: filter.OpLike, Value: "t"}})
	if err == nil || !strings.Contains(err.Error(), `cannot filter "active"`) {
		t.Fatalf("want a type refusal, got %v", err)
	}
	if _, err := resolveNestedFilters(posts, reg, []NestedFilter{{Relation: "author", Field: "name", Op: filter.OpLike, Value: "a"}}); err != nil {
		t.Fatalf("like on a string must pass: %v", err)
	}
}

func TestListMistypedOpAnswers400(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE tallies (id TEXT PRIMARY KEY, count INTEGER, label TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("tallies", entity.EntityConfig{
		Fields: []schema.Field{{Name: "count", Type: schema.Int}, {Name: "label", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db)
	for q, want := range map[string]int{"count_like=5": http.StatusBadRequest, "label_like=a": http.StatusOK} {
		rec := httptest.NewRecorder()
		ch.List()(rec, withTestUser(httptest.NewRequest(http.MethodGet, "/tallies?"+q, nil), "u1"))
		if rec.Code != want {
			t.Errorf("?%s: got %d, want %d (%s)", q, rec.Code, want, rec.Body.String())
		}
	}
}

func TestScopedFilterRefusesMistypedOp(t *testing.T) {
	fields := []schema.Field{
		{Name: "published", Type: schema.Bool},
		{Name: "views", Type: schema.Int, WireName: "hits"},
		{Name: "title", Type: schema.String},
	}
	for _, raw := range []string{"published_lt=true", "views_like=3", "hits_like=3"} {
		_, err := parseScopedFilters(raw, fields, "posts")
		if err == nil || !strings.Contains(err.Error(), "cannot filter") {
			t.Errorf("%s: want a type refusal, got %v", raw, err)
		}
	}
	if _, err := parseScopedFilters("hits_like=3", fields, "posts"); err == nil || !strings.Contains(err.Error(), `"hits"`) {
		t.Errorf("refusal must name the key the caller sent, got %v", err)
	}
	if _, err := parseScopedFilters("title_like=a,views_gt=2", fields, "posts"); err != nil {
		t.Errorf("suited operators must pass: %v", err)
	}
}
