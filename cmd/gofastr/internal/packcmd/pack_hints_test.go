package packcmd

import (
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/settings"
)

func TestPackedEndpointKeepsHints(t *testing.T) {
	e := settings.Endpoint{Name: "clear", Entity: "posts", Method: "DELETE", Path: "{id}/cache", Handler: "entities/posts.go#Clear", MCP: true,
		Hints: settings.EndpointHints{Title: "Clear the cache", Destructive: settings.HintFalse, OpenWorld: settings.HintTrue}}
	want := map[string]any{"title": "Clear the cache", "destructive": false, "open_world": true}
	if got := endpointToMap(e)["hints"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("packed hints = %#v, want %#v", got, want)
	}
	e.Hints = settings.EndpointHints{}
	if _, present := endpointToMap(e)["hints"]; present {
		t.Fatal("an endpoint with no hints packs a hints key")
	}
}
