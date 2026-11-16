package framework

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

func sweepApp3PostsApp(t *testing.T, opts ...AppOption) *App {
	t.Helper()
	app := NewApp(append([]AppOption{WithDB(openMemDB(t)), WithoutDefaultMiddleware()}, opts...)...)
	app.Entity("posts", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{Public: true},
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
	})
	if err := AutoMigrate(app.DB, app.Registry); err != nil {
		t.Fatal(err)
	}
	return app
}

// An app built without WithOutbox must leave the handler's Outbox nil, so
// a create publishes on the bus instead of staging to a nil outbox.
func TestSweepApp3EntityWithoutOutboxEmitsOnBus(t *testing.T) {
	app := sweepApp3PostsApp(t)
	got := make(chan event.Event, 1)
	app.Events().On(event.EntityCreated, func(_ context.Context, e event.Event) error {
		select {
		case got <- e:
		default:
		}
		return nil
	})
	ch := app.MustCrudHandler("posts")
	if ch.Outbox != nil {
		t.Fatalf("handler Outbox = %#v without WithOutbox, want nil", ch.Outbox)
	}
	if _, err := ch.CreateOne(context.Background(), map[string]any{"title": "a"}); err != nil {
		t.Fatalf("CreateOne: %v", err)
	}
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("no EntityCreated event reached the bus")
	}
}

// CrudHandlerForEntity refuses an entity that shares the registered one's
// name and version but is not the instance the registry holds.
func TestSweepApp3CrudHandlerForEntityRefusesLookalike(t *testing.T) {
	app := sweepApp3PostsApp(t)
	lookalike := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
	if _, err := app.CrudHandlerForEntity(lookalike); err == nil {
		t.Fatal("CrudHandlerForEntity built a handler for an entity the registry does not hold")
	}
	ent, err := app.Registry.Get("posts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.CrudHandlerForEntity(ent); err != nil {
		t.Fatalf("registered instance refused: %v", err)
	}
}

// An unversioned entity's handler addresses the app's API prefix, where
// its routes are mounted.
func TestSweepApp3CrudHandlerForEntityTakesAPIPrefix(t *testing.T) {
	app := sweepApp3PostsApp(t, WithAPIPrefix("/api"))
	ent, err := app.Registry.Get("posts")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := app.CrudHandlerForEntity(ent)
	if err != nil {
		t.Fatal(err)
	}
	if ch.BasePath != "/api" {
		t.Fatalf("BasePath = %q, want %q", ch.BasePath, "/api")
	}
}

// A route registered without a method claims every method, so an entity
// whose CRUD routes land on its path is refused before anything mounts.
func TestSweepApp3MethodlessRouteBlocksEntity(t *testing.T) {
	app := atomicTestApp(t)
	app.Router().HandleFunc("", "/posts", func(http.ResponseWriter, *http.Request) {})
	err := app.TryEntity("posts", EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("TryEntity over a method-less /posts: err = %v, want the collision diagnostic", err)
	}
	if _, gerr := app.Registry.Get("posts"); gerr == nil {
		t.Error("refused entity stayed in the registry")
	}
}

// A trailing {x...} wildcard is a different shape from a one-segment
// wildcard, so a catch-all route beside the entity's {id} routes is no
// collision.
func TestSweepApp3CatchAllRouteBesideEntityRegisters(t *testing.T) {
	app := atomicTestApp(t)
	app.Router().HandleFunc("GET", "/posts/{rest...}", func(http.ResponseWriter, *http.Request) {})
	if err := app.TryEntity("posts", EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}}); err != nil {
		t.Fatalf("TryEntity beside GET /posts/{rest...}: %v", err)
	}
}
