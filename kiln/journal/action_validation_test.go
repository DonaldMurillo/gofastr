package journal

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// The descriptor enum and agent prompt used to advertise create_entity and
// respond_query, but kiln/effect implemented neither. An action whose kind
// kiln can't run must be rejected when it enters the world IR, at authoring
// time, not deferred to a 500 from a hook or route that was never wired.
// These tests pin the scream; they use literal kind strings so they do not
// depend on the (now-deleted) const aliases.

func applyFails(t *testing.T, s *Session, e Entry, want string) {
	t.Helper()
	err := Apply(s, e)
	if err == nil {
		t.Fatalf("Apply(%s/%s): expected error, got nil", e.Kind, e.Op)
	}
	if want != "" && !strings.Contains(err.Error(), want) {
		t.Fatalf("Apply(%s/%s): error %q does not mention %q", e.Kind, e.Op, err.Error(), want)
	}
}

func TestAddHookRejectsUnsupportedActionKind(t *testing.T) {
	s := NewSession()
	applyFails(t, s, worldEdit(OpAddHook, AddHookPayload{Hook: &world.Hook{
		ID: "h1", Entity: "posts", When: "before_create",
		Action: world.Action{Kind: "create_entity"},
	}}), "create_entity")

	// A supported kind still applies.
	s2 := NewSession()
	mustApply(t, s2, worldEdit(OpAddEntity, AddEntityPayload{Entity: &world.Entity{Name: "posts"}}))
	mustApply(t, s2, worldEdit(OpAddHook, AddHookPayload{Hook: &world.Hook{
		ID: "h1", Entity: "posts", When: "before_create",
		Action: world.Action{Kind: world.ActionValidate, Params: map[string]any{"expression": "true", "message": "x"}},
	}}))
}

func TestAddRouteRejectsUnsupportedActionKind(t *testing.T) {
	s := NewSession()
	applyFails(t, s, worldEdit(OpAddRoute, AddRoutePayload{Route: &world.Route{
		Method: "GET", Path: "/x",
		Action: world.Action{Kind: "respond_query"},
	}}), "respond_query")

	// respond_json is a real response-producing kind and must apply.
	mustApply(t, s, worldEdit(OpAddRoute, AddRoutePayload{Route: &world.Route{
		Method: "GET", Path: "/y",
		Action: world.Action{Kind: world.ActionRespondJSON},
	}}))
}

func TestAddEntityRejectsUnsupportedEndpointAction(t *testing.T) {
	s := NewSession()
	applyFails(t, s, worldEdit(OpAddEntity, AddEntityPayload{Entity: &world.Entity{
		Name:   "posts",
		Fields: []world.Field{{Name: "title", Type: "string"}},
		Endpoints: []world.EntityEndpoint{{
			Method: "POST", Path: "{id}/publish",
			Action: world.Action{Kind: "bogus_kind"},
		}},
	}}), "bogus_kind")
}

func TestAddPageRejectsUnsupportedNodeAction(t *testing.T) {
	s := NewSession()
	applyFails(t, s, worldEdit(OpAddPage, AddPagePayload{Page: &world.Page{
		Path: "/",
		Tree: world.Node{
			Kind: "button",
			Actions: map[string]world.Action{
				"click": {Kind: "create_entity"},
			},
		},
	}}), "create_entity")

	// A supported node action still applies.
	mustApply(t, s, worldEdit(OpAddPage, AddPagePayload{Page: &world.Page{
		Path: "/ok",
		Tree: world.Node{
			Kind: "button",
			Actions: map[string]world.Action{
				"click": {Kind: world.ActionRespondJSON, Params: map[string]any{"status": 200}},
			},
		},
	}}))
}
