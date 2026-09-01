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

// Property: no add_page argument can panic or wedge the live runtime.
//
// The add_route pin above covers the ServeMux sink; add_page has its
// own pattern-consuming sink. protocol.AddPage (protocol.go:377-401)
// validates the tree, duplicates, and (conditionally) entity-name
// collisions, but never the path grammar. kiln/render.applyUIHostPages
// (uihost.go:60-64) registers every page verbatim as a core-ui screen,
// and Router.Screen (core-ui/app/router.go:55) normalizes "{id}" to
// ":id", then treats any ':' in the path as dynamic: a dynamic page
// panics because kiln's worldScreen (uihost.go:97-105) implements no
// SetParams (router.go:85-91), and a malformed dynamic segment panics
// inside validateDynamicSegments (router.go:223-255): catch-all not
// final, unknown constraint, "{p...:int}". The panic fires inside
// live.Apply's rebuild, skipping the restoreFromJournal rollback
// (kiln/live/live.go:105-107) exactly like the pinned add_route poison:
// journal.Apply has already installed the page in the in-memory world,
// so every later Apply re-panics until ResetSession or restart.

func addPageRecovered(t *testing.T, tools *protocol.Tools, path string) (res protocol.Result, panicked any) {
	t.Helper()
	defer func() { panicked = recover() }()
	res = tools.AddPage(context.Background(), protocol.AddPageArgs{
		Page: &world.Page{Path: path, Tree: world.Node{Kind: "div"}},
	})
	return
}

func TestAddPageRejectsScreenPoisonPaths(t *testing.T) {
	cases := []struct{ name, path string }{
		{"dynamic path without SetParams", "/users/{id}"},
		{"catch-all not final", "/docs/{path...}/edit"},
		{"unknown constraint", "/x/{v:string}"},
		{"malformed catch-all constraint", "/x/{p...:int}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools := newTools(t)
			res, panicked := addPageRecovered(t, tools, tc.path)
			if panicked != nil {
				t.Errorf("SECURITY: add_page(%s) panicked out of the tool call: %v.\n"+
					"Attack: the unauthenticated tool API accepts any page.path string; "+
					"applyUIHostPages registers it as a core-ui screen, whose router panics "+
					"on dynamic and malformed patterns. The panic skips live.Apply's "+
					"restoreFromJournal rollback (kiln/live/live.go:105-107) and leaves the "+
					"poison page in the in-memory world, so every later Apply re-panics "+
					"until restart.", tc.path, panicked)
			}
			if res.OK {
				t.Errorf("SECURITY: add_page(%s) returned OK for a path the screen router "+
					"cannot register: worldScreen implements no SetParams, so a dynamic "+
					"page drops its params silently at best; the entry must be rejected "+
					"at validation", tc.path)
			}
			if _, stuck := tools.Live().Session().World.Pages[tc.path]; stuck {
				t.Errorf("poison page %q survived in the in-memory world after a rejected add_page", tc.path)
			}
			follow, followPanicked := addEntityRecovered(t, tools, "comments")
			if followPanicked != nil {
				t.Errorf("SECURITY: world wedged: AddEntity after the rejected add_page panicked: %v.\n"+
					"The rejected page stays in the rebuilt world, so every later Apply "+
					"re-panics at screen registration.", followPanicked)
			} else if !follow.OK {
				t.Errorf("AddEntity after rejected add_page = %+v, want OK", follow)
			}
		})
	}
}

// Property: an add_page that collides with an entity's CRUD mount path
// must be rejected as a Result, never panic the rebuild.
//
// The entity/page collision guards in protocol.AddEntity and AddPage
// (protocol.go:301-307, 393-401) only run when app.api_prefix is empty
// and compare '/'+entity.Name. The panicking sink is neither: framework
// App.Mount (framework/app.go:1098-1114) unconditionally compares every
// uihost RoutePatterns() entry against entityMountPath = prefix+'/'+table
// (framework/app.go:581-583), where an empty Table defaults to the
// snake-cased entity name (framework/entity/entity.go:314-317). With the
// default prefix "api" (world.New), add_page "/api/posts" next to entity
// "posts" — or any table override colliding with any page — passes every
// protocol guard and panics inside live.Apply's rebuild, wedging the
// runtime like the pinned add_route poison. kiln/journal's replay pin
// covers the hand-authored-journal twin; kiln/live's pin covers boot.

func TestAddPageRejectsEntityMountCollisions(t *testing.T) {
	cases := []struct {
		name     string
		entity   *world.Entity
		pagePath string
	}{
		{"default table under api prefix",
			&world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
			"/api/posts"},
		{"table override",
			&world.Entity{Name: "stuff", Table: "clashes", Fields: []world.Field{{Name: "title", Type: "string"}}},
			"/api/clashes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools := newTools(t)
			base, basePanicked := func() (res protocol.Result, panicked any) {
				defer func() { panicked = recover() }()
				res = tools.AddEntity(context.Background(), protocol.AddEntityArgs{Entity: tc.entity})
				return
			}()
			if basePanicked != nil || !base.OK {
				t.Fatalf("baseline AddEntity: panicked=%v res=%+v", basePanicked, base)
			}
			res, panicked := addPageRecovered(t, tools, tc.pagePath)
			if panicked != nil {
				t.Errorf("SECURITY: add_page(%s) beside entity %q panicked out of the tool call: %v.\n"+
					"Attack: the collision guard only runs in bare-CRUD mode and compares "+
					"entity names, but App.Mount panics on prefix+'/'+table equality "+
					"(framework/app.go:1110-1112). The panic skips live.Apply's rollback "+
					"(kiln/live/live.go:105-107), the poison page stays in the in-memory "+
					"world, and every later Apply re-panics until restart.",
					tc.pagePath, tc.entity.Name, panicked)
			}
			if res.OK {
				t.Errorf("SECURITY: add_page(%s) returned OK although it collides with entity "+
					"%q's CRUD mount; the entry must be rejected at validation",
					tc.pagePath, tc.entity.Name)
			}
			if _, stuck := tools.Live().Session().World.Pages[tc.pagePath]; stuck {
				t.Errorf("colliding page %q survived in the in-memory world after a rejected add_page", tc.pagePath)
			}
			follow, followPanicked := addPageRecovered(t, tools, "/later")
			if followPanicked != nil {
				t.Errorf("SECURITY: world wedged: add_page(/later) after the rejected collision panicked: %v.\n"+
					"The colliding page stays in the rebuilt world, so every later Apply "+
					"re-panics at Mount.", followPanicked)
			} else if !follow.OK {
				t.Errorf("add_page(/later) after rejected collision = %+v, want OK", follow)
			}
		})
	}
}
