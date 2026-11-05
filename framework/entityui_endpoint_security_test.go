package framework

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
)

func TestUIEndpointRefusalIsAtomic(t *testing.T) {
	for _, endpoint := range []struct{ method, path string }{{"POST", "_bulk"}, {"GET", "_export.csv"}, {"POST", "_pick"}} {
		for _, grouped := range []bool{false, true} {
			for _, uiFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/grouped=%v/uiFirst=%v", endpoint.path, grouped, uiFirst), func(t *testing.T) {
					database, err := sql.Open("sqlite", ":memory:")
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = database.Close() })
					app := NewApp(WithDB(database), WithoutDefaultMiddleware())
					config := entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}}
					app.Entity("clean", config)
					group := app.Group("/v1")
					register := func(config entity.EntityConfig) (err error) {
						defer func() {
							if recovered := recover(); recovered != nil {
								err = fmt.Errorf("%v", recovered)
							}
						}()
						if grouped {
							app.GroupEntity(group, "notes", config)
							return nil
						}
						return app.TryEntity("notes", config)
					}
					config.Endpoints = []entity.Endpoint{{Method: endpoint.method, Path: endpoint.path, Handler: http.HandlerFunc(noContent)}}
					if uiFirst {
						app.EntityUI(entityui.Extensions{})
						before := len(app.router.Routes())
						if err := register(config); err == nil || !strings.Contains(err.Error(), "EntityUI") {
							t.Fatalf("endpoint conflict = %v, want EntityUI preflight refusal", err)
						}
						if _, err := app.Registry.Get("notes"); err == nil || len(app.router.Routes()) != before {
							t.Fatal("refused entity left its registry entry or routes published")
						}
						config.Endpoints = nil
						if err := register(config); err != nil {
							t.Fatalf("corrected registration failed: %v", err)
						}
						return
					}
					if err := register(config); err != nil {
						t.Fatal(err)
					}
					before := len(app.router.Routes())
					var recovered any
					func() {
						defer func() { recovered = recover() }()
						app.EntityUI(entityui.Extensions{})
					}()
					if recovered == nil {
						t.Fatal("EntityUI accepted an existing reserved endpoint")
					}
					if app.entityUI != nil || len(app.router.Routes()) != before {
						t.Fatal("refused EntityUI left its pointer or routes published")
					}
				})
			}
		}
	}
}

func TestUIPageCollisionIsAtomic(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	app := NewApp(WithDB(database), WithoutDefaultMiddleware())
	app.EntityUI(entityui.Extensions{})
	app.Router().Post("/notes/_bulk", http.HandlerFunc(noContent))
	before := len(app.router.Routes())
	err = app.TryEntity("notes", entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
	if err == nil || len(app.router.Routes()) != before {
		t.Fatalf("existing page conflict = %v, route count = %d, want %d", err, len(app.router.Routes()), before)
	}
	if _, err := app.Registry.Get("notes"); err == nil {
		t.Fatal("page collision published the entity")
	}
}

func TestUIUnownedVersionKeepsEndpoint(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	app := NewApp(WithDB(database), WithoutDefaultMiddleware())
	app.EntityUI(entityui.Extensions{})
	config := entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}}
	app.GroupEntity(app.Group("/v1"), "notes", config)
	config.Endpoints = []entity.Endpoint{{Method: "POST", Path: "_bulk", Handler: http.HandlerFunc(noContent)}}
	app.GroupEntity(app.Group("/v2"), "notes", config)
	if _, err := app.Registry.GetVersioned("notes", "/v2"); err != nil {
		t.Fatal(err)
	}
}
