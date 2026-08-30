package protocol_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Property: no add_route argument can panic or wedge the live runtime.
//
// Kiln's tool API is unauthenticated and mutates the world IR over HTTP,
// so route.method and route.path are attacker-authored. The MCP
// descriptor (kiln/protocol/descriptors.go routeSchema) advertises
// method as enum("GET","POST","PUT","DELETE","PATCH"), but
// protocol.AddRoute (kiln/protocol/protocol.go:612-621) checks only
// non-empty + not-an-exact-duplicate, and journal replay's re-check
// (kiln/journal/replay.go:332-347) checks only action kind and
// duplicates. Everything else flows to render.applyRoutes
// (kiln/render/render.go:96-118) -> Router.Handle -> http.ServeMux
// pattern parsing, which PANICS on a lowercase method, a path without a
// leading '/', malformed '{' wildcard syntax, and on a conflict with
// kiln's own registration of GET /openapi.json (mounted in
// live.rebuild, kiln/live/live.go:201-207, whenever the registry is
// non-empty).
//
// The panic escapes live.Apply's rollback: restoreFromJournal
// (kiln/live/live.go:105-107) runs only on the error-return path, but
// journal.Apply at live.go:99 has already put the route in the
// in-memory world, so the poison entry survives in memory and every
// later Apply re-panics until ResetSession or process restart.
//
// The pinned property is the documented contract at the ingestion
// seam: an add_route whose pattern net/http cannot register must be
// REJECTED as a Result, never panic, never leave the route in the
// world, and never leave the runtime unable to apply the next edit.

func addRouteRecovered(t *testing.T, tools *protocol.Tools, method, path string) (res protocol.Result, panicked any) {
	t.Helper()
	defer func() { panicked = recover() }()
	res = tools.AddRoute(context.Background(), protocol.AddRouteArgs{
		Route: &world.Route{Method: method, Path: path},
	})
	return
}

func addEntityRecovered(t *testing.T, tools *protocol.Tools, name string) (res protocol.Result, panicked any) {
	t.Helper()
	defer func() { panicked = recover() }()
	res = tools.AddEntity(context.Background(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: name, Fields: []world.Field{{Name: "title", Type: "string"}}},
	})
	return
}

func TestAddRouteRejectsMuxPoisonPatterns(t *testing.T) {
	cases := []struct {
		name        string
		method      string
		path        string
		needsEntity bool // openapi.json conflict fires only when the registry is non-empty
	}{
		{"lowercase method", "get", "/lower", false},
		{"path without leading slash", "GET", "posts", false},
		{"malformed wildcard", "GET", "/x/{oops", false},
		{"conflicts with kiln openapi registration", "GET", "/openapi.json", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools := newTools(t)
			if tc.needsEntity {
				if er, p := addEntityRecovered(t, tools, "posts"); p != nil || !er.OK {
					t.Fatalf("baseline AddEntity: panicked=%v res=%+v", p, er)
				}
			}
			res, panicked := addRouteRecovered(t, tools, tc.method, tc.path)
			if panicked != nil {
				t.Errorf("SECURITY: add_route(%s %s) panicked out of the tool call: %v.\n"+
					"Attack: the unauthenticated tool API accepts any method/path string; "+
					"Router.Handle feeds it to net/http parsePattern, whose panic skips "+
					"live.Apply's restoreFromJournal rollback (kiln/live/live.go:105-107) "+
					"and leaves the poison route in the in-memory world, so every later "+
					"Apply re-panics until restart.", tc.method, tc.path, panicked)
			}
			if res.OK {
				t.Errorf("SECURITY: add_route(%s %s) returned OK for input outside the descriptor "+
					"contract (method enum GET/POST/PUT/DELETE/PATCH, slash-prefixed path): a "+
					"lowercase method registers on the mux but never matches a real request "+
					"(net/http matches methods case-sensitively and clients send uppercase), so "+
					"the tool confirms and journals a dead route; the entry must be rejected at "+
					"validation", tc.method, tc.path)
			}
			if got := len(tools.Live().Session().World.Routes); got != 0 {
				t.Errorf("poison route survived in the in-memory world after a rejected add_route: %d route(s) present", got)
			}
			// The runtime must remain usable: one bad add_route must not
			// wedge every subsequent world edit.
			follow, followPanicked := addEntityRecovered(t, tools, "comments")
			if followPanicked != nil {
				t.Errorf("SECURITY: world wedged: AddEntity after the rejected add_route panicked: %v.\n"+
					"The rejected route stays in the rebuilt world, so every later Apply "+
					"re-panics at route registration.", followPanicked)
			} else if !follow.OK {
				t.Errorf("AddEntity after rejected add_route = %+v, want OK", follow)
			}
		})
	}
}
