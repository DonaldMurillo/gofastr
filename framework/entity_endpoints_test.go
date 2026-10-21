package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestEntityEndpointsJoinRegistration(t *testing.T) {
	app := NewApp()
	app.EntityEndpoints("licenses", entity.Endpoint{Method: "POST", Path: "{id}/revoke", Name: "revoke_license", Public: true, Handler: http.HandlerFunc(noContent)})
	app.Entity("licenses", entity.EntityConfig{Fields: []schema.Field{{Name: "name", Type: schema.String}}})

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest("POST", "/licenses/abc/revoke", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("queued endpoint not mounted: %d", rec.Code)
	}
	ent, _ := app.Registry.Get("licenses")
	if len(ent.Config.Endpoints) != 1 || ent.Config.Endpoints[0].Name != "revoke_license" {
		t.Fatalf("registry config lacks the endpoint: %+v", ent.Config.Endpoints)
	}
	if len(app.pendingEndpoints) != 0 {
		t.Fatal("a registered entity's endpoints are still queued; Start would refuse")
	}
}

func TestEntityEndpointsAfterRegistrationPanics(t *testing.T) {
	app := NewApp()
	app.Entity("licenses", entity.EntityConfig{Fields: []schema.Field{{Name: "name", Type: schema.String}}})
	defer func() {
		if recover() == nil {
			t.Fatal("a late EntityEndpoints call was silently dropped")
		}
	}()
	app.EntityEndpoints("licenses", entity.Endpoint{Method: "POST", Path: "{id}/revoke", Handler: http.HandlerFunc(noContent)})
}

// A queued endpoint that collides is refused with the entity, and stays
// queued for the corrected retry.
func TestEntityEndpointsCollisionKeepsQueue(t *testing.T) {
	app := NewApp()
	app.EntityEndpoints("licenses", entity.Endpoint{Method: "POST", Path: "{id}/revoke", Handler: http.HandlerFunc(noContent)})
	app.Router().Handle("POST", "/licenses/{id}/revoke", http.HandlerFunc(noContent))
	err := app.TryEntity("licenses", entity.EntityConfig{Fields: []schema.Field{{Name: "name", Type: schema.String}}})
	if err == nil || !strings.Contains(err.Error(), "revoke") {
		t.Fatalf("collision not refused: %v", err)
	}
	if len(app.pendingEndpoints["licenses"]) != 1 {
		t.Fatal("a refused registration dropped the queue")
	}
}

func TestStartRefusesUnclaimedEndpoints(t *testing.T) {
	app := NewApp()
	app.EntityEndpoints("licences", entity.Endpoint{Method: "POST", Path: "{id}/revoke", Handler: http.HandlerFunc(noContent)})
	err := app.Start("127.0.0.1:0")
	if err == nil || !strings.Contains(err.Error(), "licences") {
		t.Fatalf("Start ignored endpoints for an unregistered entity: %v", err)
	}
}
