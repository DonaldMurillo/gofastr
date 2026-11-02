package appspec

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// A field or relation name that does not start with a lower-case letter
// (Title, HTTPCode, _note) cannot be written through the generated typed
// repo: framework.MarshalEntity turns the struct's JSON keys back into
// columns through snake_case, which lowers Title to title, a column that
// does not exist, so Create returned the row with the field empty. The
// HTTP clients spell the server's key (appspec.WireKey); the in-process
// path has no spelling that survives, so the validator refuses the name.
func TestAppSpecRefusesNamesTheRepoCannotWrite(t *testing.T) {
	for _, name := range []string{"Title", "HTTPCode", "_note"} {
		bp := AppSpec{Entities: []framework.EntityDeclaration{
			{Name: "authors", Fields: []framework.FieldDeclaration{{Name: "email", Type: "string"}}},
			{Name: "posts", Fields: []framework.FieldDeclaration{{Name: name, Type: "string"}}},
		}}
		err := ValidateAppSpec(bp)
		if err == nil || !strings.Contains(err.Error(), `field "`+name+`"`) || !strings.Contains(err.Error(), "lower-case letter") {
			t.Errorf("field %q: %v", name, err)
		}
		bp.Entities[1].Fields = []framework.FieldDeclaration{{Name: "headline", Type: "string"}, {Name: "author_id", Type: "string"}}
		bp.Entities[1].Relations = []framework.Relation{{Type: framework.RelManyToOne, Name: name, Entity: "authors", ForeignKey: []string{"author_id"}}}
		err = ValidateAppSpec(bp)
		if err == nil || !strings.Contains(err.Error(), `relation "`+name+`"`) || !strings.Contains(err.Error(), "lower-case letter") {
			t.Errorf("relation %q: %v", name, err)
		}
	}

	// The reason still holds: a struct keyed by the server's wire key Title
	// loses the value on the in-process create. When MarshalEntity learns
	// to map such a key back to its column, this fails and the rule can go.
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
	}{Title: new("kept?")})
	if err != nil {
		t.Fatal(err)
	}
	row, err := framework.NewCrudHandler(ent, db).CreateOne(context.Background(), body)
	if err == nil && row["Title"] == "kept?" {
		t.Fatal("the in-process create now keeps a Title field: the lower-case-first rule can be relaxed")
	}
}
