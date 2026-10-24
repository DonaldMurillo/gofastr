package crud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A nested filter refusal keeps its "nested filter" frame around the
// operator's own message, both in the caller's locale, and answers the
// operator's code.
func TestNestedFilterErrorKeepsContext(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE nfusers (id TEXT PRIMARY KEY, age INTEGER)`,
		`CREATE TABLE nfposts (id TEXT PRIMARY KEY, author_id TEXT)`,
	)
	users := entity.Define("nfusers", entity.EntityConfig{
		Name: "nfusers", Table: "nfusers",
		Fields: []schema.Field{{Name: "age", Type: schema.Int}},
	}.WithTimestamps(false))
	posts := entity.Define("nfposts", entity.EntityConfig{
		Name: "nfposts", Table: "nfposts",
		Fields:    []schema.Field{{Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "nfusers", "author_id")},
	}.WithTimestamps(false))
	posts.SetDB(db)
	ch := NewCrudHandler(posts, db)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"nfusers": users, "nfposts": posts}}

	cat := i18n.NewMapCatalog()
	cat.Set("es", "error.nested_filter", i18n.Message{Text: "filtro anidado {{key}}: {{detail}}"})
	cat.Set("es", "filter.op_type_mismatch", i18n.Message{Text: "el operador {{op}} no filtra {{field}}"})
	req := withTestUser(httptest.NewRequest("GET", "/nfposts?author.age_like=x", nil), "u1")
	ctx := i18n.WithContext(req.Context(), i18n.Locale{Tag: "es"})
	req = req.WithContext(i18n.WithTranslator(ctx, i18n.NewTranslator(cat, "en")))
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var body struct{ Error, Reason string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := `filtro anidado "author.age_like": el operador "like" no filtra "age"`
	if body.Error != want || body.Reason != "op_type_mismatch" {
		t.Fatalf("body = %+v, want %q and op_type_mismatch", body, want)
	}
}

// A parser refusal reaches the body in the caller's locale, with the
// parser's own code beside it.
func TestListFilterErrorTranslates(t *testing.T) {
	ch, _ := covFaultNotes(t)
	cat := i18n.NewMapCatalog()
	cat.Set("es", "filter.unknown_filter", i18n.Message{Text: "filtro desconocido {{key}}"})
	req := withTestUser(httptest.NewRequest("GET", "/notes?title_zz=x", nil), "u1")
	ctx := i18n.WithContext(req.Context(), i18n.Locale{Tag: "es"})
	req = req.WithContext(i18n.WithTranslator(ctx, i18n.NewTranslator(cat, "en")))
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body struct{ Error, Reason string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != `filtro desconocido "title_zz"` || body.Reason != "unknown_filter" {
		t.Fatalf("body = %+v, want the Spanish text and unknown_filter", body)
	}
}
