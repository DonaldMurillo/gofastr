package framework

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func TestViewMountReportsReadOnly(t *testing.T) {
	app := NewApp(WithDB(memDB(t)), WithoutDefaultMiddleware())
	app.View(migrate.View{Name: "events_view", Select: "SELECT 'a' AS id", Columns: []migrate.Column{
		{Name: "id", Type: schema.String, PrimaryKey: true},
	}})
	view, err := app.Registry.Get("events_view")
	if err != nil {
		t.Fatal(err)
	}
	if !app.EntityCRUDMounted(view) || !app.EntityReadOnlyMounted(view) {
		t.Fatal("a columned view must report mounted read-only routes")
	}
	writable := entity.Define("events", entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
	app.Registry.Register(writable)
	if app.EntityReadOnlyMounted(writable) {
		t.Fatal("a writable entity reported read-only routes")
	}
	if NewApp().EntityReadOnlyMounted(writable) {
		t.Fatal("an unmounted entity reported read-only routes")
	}
}
