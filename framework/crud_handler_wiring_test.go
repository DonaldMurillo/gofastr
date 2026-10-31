package framework

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// CrudHandlerForEntity said it was wired like CrudHandler(name) and set
// neither ChildHooks, ImageDeriver, FieldImageDerivers nor
// StripUploadMetadata, so the entityui back office, which builds its
// handler through it, saved images with no renditions and cascaded
// without child hooks (2026-10-27 S5, S21). Every field but BasePath,
// which each sets for its own mount, must match.
func TestCrudHandlerForEntityWiredLikeCrudHandler(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := NewApp(WithDB(db), WithoutDefaultMiddleware(),
		WithImagePipeline(stubDeriver{"all"}),
		WithImagePipelineFor("posts", "cover", stubDeriver{"cover"}),
		WithStripUploadMetadata())
	app.Entity("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}, {Name: "cover", Type: schema.String}},
	})

	byName, err := app.CrudHandler("posts")
	if err != nil {
		t.Fatal(err)
	}
	ent, err := app.Registry.Get("posts")
	if err != nil {
		t.Fatal(err)
	}
	byEnt, err := app.CrudHandlerForEntity(ent)
	if err != nil {
		t.Fatal(err)
	}
	a, b := reflect.ValueOf(byName).Elem(), reflect.ValueOf(byEnt).Elem()
	for i := range a.NumField() {
		f := a.Type().Field(i)
		if !f.IsExported() || f.Name == "BasePath" {
			continue
		}
		x, y := a.Field(i), b.Field(i)
		if f.Type.Kind() == reflect.Func {
			if x.IsNil() != y.IsNil() {
				t.Errorf("%s: CrudHandler set=%v, CrudHandlerForEntity set=%v", f.Name, !x.IsNil(), !y.IsNil())
			}
			continue
		}
		if !reflect.DeepEqual(x.Interface(), y.Interface()) {
			t.Errorf("%s differs: CrudHandler %v, CrudHandlerForEntity %v", f.Name, x.Interface(), y.Interface())
		}
	}
	if byEnt.ImageDeriver == nil || byEnt.FieldImageDerivers["cover"] == nil || !byEnt.StripUploadMetadata || byEnt.ChildHooks == nil {
		t.Error("CrudHandlerForEntity left the image pipeline, metadata strip or child hooks unset")
	}
}
