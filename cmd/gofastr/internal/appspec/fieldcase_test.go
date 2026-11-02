package appspec

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// A field or relation name that does not start with a lower-case letter
// (Title, HTTPCode, _note) is accepted. App specs used to refuse one: the
// generated typed repo writes through framework.MarshalEntity, which
// turned the struct's JSON key Title into title, a key CreateOne matched
// to no column, so the value was dropped. CreateOne now maps that key back
// to its column, which the second half pins through the same path the
// repo takes. Two names one Go field or one Postgres column would carry
// are refused instead.
func TestAppSpecAcceptsNamesTheRepoCanWrite(t *testing.T) {
	for _, name := range []string{"Title", "HTTPCode", "_note"} {
		bp := AppSpec{Entities: []framework.EntityDeclaration{
			{Name: "authors", Fields: []framework.FieldDeclaration{{Name: "email", Type: "string"}}},
			{Name: "posts", Fields: []framework.FieldDeclaration{{Name: name, Type: "string"}}},
		}}
		if err := ValidateAppSpec(bp); err != nil && strings.Contains(err.Error(), `"`+name+`"`) {
			t.Errorf("field %q refused: %v", name, err)
		}
		bp.Entities[1].Fields = []framework.FieldDeclaration{{Name: "headline", Type: "string"}, {Name: "author_id", Type: "string"}}
		bp.Entities[1].Relations = []framework.Relation{{Type: framework.RelManyToOne, Name: name, Entity: "authors", ForeignKey: []string{"author_id"}}}
		if err := ValidateAppSpec(bp); err != nil && strings.Contains(err.Error(), `"`+name+`"`) {
			t.Errorf("relation %q refused: %v", name, err)
		}
	}
	for _, pair := range [][2]string{{"Title", "title"}, {"_note", "note"}} {
		bp := AppSpec{Entities: []framework.EntityDeclaration{
			{Name: "posts", Fields: []framework.FieldDeclaration{{Name: pair[0], Type: "string"}, {Name: pair[1], Type: "string"}}},
		}}
		if err := ValidateAppSpec(bp); err == nil || !strings.Contains(err.Error(), "collides with field") {
			t.Errorf("fields %q and %q: %v, want a collision", pair[0], pair[1], err)
		}
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
	cfg, err := framework.EntityDeclaration{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "Title", Type: "string"}}}.Config()
	if err != nil {
		t.Fatal(err)
	}
	app.Entity("posts", cfg)
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	ent, err := app.Registry.Get("posts")
	if err != nil {
		t.Fatal(err)
	}
	body, err := framework.MarshalEntity(struct {
		Title *string `json:"Title,omitempty"`
	}{Title: new("kept")})
	if err != nil {
		t.Fatal(err)
	}
	row, err := framework.NewCrudHandler(ent, db).CreateOne(context.Background(), body)
	if err != nil || row["Title"] != "kept" {
		t.Fatalf("in-process create of a Title field: row %v, err %v", row, err)
	}
}
