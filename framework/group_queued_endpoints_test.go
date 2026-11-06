package framework

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestGroupClaimsQueuedEndpoints(t *testing.T) {
	app := NewApp()
	group := app.Group("/v1")
	queued := entity.Endpoint{Method: "POST", Path: "{id}/revoke", Public: true, Handler: http.HandlerFunc(noContent)}
	app.EntityEndpoints("licenses", queued)
	config := entity.EntityConfig{
		Fields:    []schema.Field{{Name: "name", Type: schema.String}},
		Endpoints: []entity.Endpoint{{Method: "POST", Path: "{id}/renew", Public: true, Handler: http.HandlerFunc(noContent)}},
	}
	app.GroupEntity(group, "licenses", config)
	for _, action := range []string{"revoke", "renew"} {
		recorder := httptest.NewRecorder()
		app.Router().ServeHTTP(recorder, httptest.NewRequest("POST", "/v1/licenses/abc/"+action, nil))
		if recorder.Code != http.StatusNoContent {
			t.Errorf("%s status = %d, want 204", action, recorder.Code)
		}
	}
	if err := app.checkQueuedEndpointsClaimed(); err != nil || len(config.Endpoints) != 1 {
		t.Fatalf("group must claim its queue without modifying the caller's config: %v", err)
	}
}

func TestGroupRefusalKeepsEndpointQueue(t *testing.T) {
	app := NewApp()
	group := app.Group("/v1")
	endpoint := entity.Endpoint{Method: "POST", Path: "{id}/revoke", Public: true, Handler: http.HandlerFunc(noContent)}
	app.EntityEndpoints("licenses", endpoint)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("duplicate endpoint was accepted")
			}
		}()
		app.GroupEntity(group, "licenses", entity.EntityConfig{Endpoints: []entity.Endpoint{endpoint}})
	}()
	if len(app.pendingEndpoints["licenses"]) != 1 || len(app.Registry.All()) != 0 || app.checkQueuedEndpointsClaimed() == nil {
		t.Fatal("failed grouped registration consumed the queue or registered the entity")
	}
	app.GroupEntity(group, "licenses", entity.EntityConfig{})
	if err := app.checkQueuedEndpointsClaimed(); err != nil {
		t.Fatalf("successful retry did not claim queued endpoints: %v", err)
	}
}

// One entity registered into two version groups gets the queued endpoints
// in both: the queue belongs to the name, not to its first registration.
func TestEveryGroupGetsQueuedEndpoints(t *testing.T) {
	app := NewApp()
	queued := entity.Endpoint{Method: "POST", Path: "{id}/revoke", Public: true, Handler: http.HandlerFunc(noContent)}
	app.EntityEndpoints("licenses", queued)
	config := entity.EntityConfig{Fields: []schema.Field{{Name: "name", Type: schema.String}}}
	app.GroupEntity(app.Group("/v1"), "licenses", config)
	app.GroupEntity(app.Group("/v2"), "licenses", config)
	for _, prefix := range []string{"/v1", "/v2"} {
		recorder := httptest.NewRecorder()
		app.Router().ServeHTTP(recorder, httptest.NewRequest("POST", prefix+"/licenses/abc/revoke", nil))
		if recorder.Code != http.StatusNoContent {
			t.Errorf("%s revoke status = %d, want 204", prefix, recorder.Code)
		}
	}
	if err := app.checkQueuedEndpointsClaimed(); err != nil {
		t.Fatalf("claimed queue reported as unregistered: %v", err)
	}
}
