package framework

import (
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestEndpointRouteOptions(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, mount string
		opts                      crud.CrudRouteOptions
		ui, refused               bool
	}{
		{"events", "GET", "/v2/items/_events", "/v2/items", crud.CrudRouteOptions{}, true, true},
		{"bulk", "POST", "/v2/items/_bulk", "/v2/items", crud.CrudRouteOptions{}, true, true},
		{"picker", "POST", "/v2/items/_pick", "/v2/items", crud.CrudRouteOptions{}, true, true},
		{"without_ui", "POST", "/v2/items/_bulk", "/v2/items", crud.CrudRouteOptions{}, false, false},
		{"without_crud", "GET", "/v2/items/_events", "", crud.CrudRouteOptions{}, true, false},
		{"llm", "GET", "/v2/items/llm.md", "/v2/items", crud.CrudRouteOptions{}, false, true},
		{"without_llm", "GET", "/v2/items/llm.md", "/v2/items", crud.CrudRouteOptions{NoLLMMD: true}, false, false},
		{"composite_key", "GET", "/v2/items/{slug}/{code}", "/v2/items", crud.CrudRouteOptions{Key: []string{"parent", "child"}}, true, true},
		{"single_key", "GET", "/v2/items/{slug}/{code}", "/v2/items", crud.CrudRouteOptions{}, true, false},
		{"states", "POST", "/v2/items/{row}/transitions/{move}", "/v2/items", crud.CrudRouteOptions{States: &entity.StatesConfig{Transitions: []entity.Transition{{Key: "close", From: []string{"open"}, To: "closed"}}}}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEntityEndpointRoutes(tc.mount, tc.opts, tc.ui, []Endpoint{{Method: tc.method, Path: tc.path}})
			if (err != nil) != tc.refused {
				t.Fatalf("route refusal = %v; want refused=%t", err, tc.refused)
			}
		})
	}
}

func TestEndpointRouteIndex(t *testing.T) {
	err := ValidateEntityEndpointRoutes("/api/items", crud.CrudRouteOptions{}, true, []Endpoint{{Method: "GET", Path: "/api/items/events/{id}"}, {Method: "GET", Path: "/api/items/{id}/history"}})
	var collision *EndpointRouteError
	if !errors.As(err, &collision) || collision.Index != 1 || !strings.Contains(collision.Error(), "overlaps") || errors.Unwrap(collision) == nil {
		t.Fatalf("second sibling must expose its slice index and cause: %v", err)
	}
}
