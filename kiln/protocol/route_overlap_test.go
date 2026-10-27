package protocol_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Two routes that are each valid but match some of the same paths with
// neither more specific: the mux panics registering the second, and the
// agent used to get kind "internal". It gets a conflict naming the route.
func TestOverlappingRouteIsConflict(t *testing.T) {
	tools := newTools(t)
	ctx := context.Background()
	if r := tools.AddRoute(ctx, protocol.AddRouteArgs{Route: &world.Route{Method: "GET", Path: "/x/{id}"}}); !r.OK {
		t.Fatalf("first route: %+v", r)
	}
	r := tools.AddRoute(ctx, protocol.AddRouteArgs{Route: &world.Route{Method: "GET", Path: "/{name}/y"}})
	if r.OK || r.Kind != "conflict" || !strings.Contains(r.Error, "GET /x/{id}") {
		t.Fatalf("overlapping route = %+v, want conflict naming GET /x/{id}", r)
	}
	// A route that is more specific than an existing one still registers.
	if r := tools.AddRoute(ctx, protocol.AddRouteArgs{Route: &world.Route{Method: "GET", Path: "/x/new"}}); !r.OK {
		t.Fatalf("more specific route: %+v", r)
	}
}

// A page on an entity's CRUD mount under the api prefix: replay refused
// it and the agent got kind "internal". The live check compared entity
// names with an empty prefix only; it now asks for the mount path.
func TestPageOnCRUDMountIsConflict(t *testing.T) {
	tools := newTools(t)
	ctx := context.Background()
	if r := tools.AddEntity(ctx, protocol.AddEntityArgs{Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}}); !r.OK {
		t.Fatalf("add_entity: %+v", r)
	}
	r := tools.AddPage(ctx, protocol.AddPageArgs{Page: &world.Page{Path: "/api/posts", Tree: world.Node{Kind: "div"}}})
	if r.OK || r.Kind != "conflict" || !strings.Contains(r.Error, `"posts"`) {
		t.Fatalf("page on CRUD mount = %+v, want conflict naming posts", r)
	}
}

// update_entity can move the mount by changing the table.
func TestEntityTableOntoPageIsConflict(t *testing.T) {
	tools := newTools(t)
	ctx := context.Background()
	fields := []world.Field{{Name: "title", Type: "string"}}
	if r := tools.AddEntity(ctx, protocol.AddEntityArgs{Entity: &world.Entity{Name: "posts", Fields: fields}}); !r.OK {
		t.Fatalf("add_entity: %+v", r)
	}
	if r := tools.AddPage(ctx, protocol.AddPageArgs{Page: &world.Page{Path: "/api/articles", Tree: world.Node{Kind: "div"}}}); !r.OK {
		t.Fatalf("add_page: %+v", r)
	}
	target := []journal.PlanTarget{{Op: "update_entity", Name: "posts"}}
	if r := tools.ProposePlan(ctx, protocol.ProposePlanArgs{PlanID: "p", Steps: []string{"rename"}, Targets: target}); !r.OK {
		t.Fatalf("propose_plan: %+v", r)
	}
	if r := tools.ApprovePlan(ctx, protocol.ApprovePlanArgs{PlanID: "p"}); !r.OK {
		t.Fatalf("approve_plan: %+v", r)
	}
	r := tools.UpdateEntity(ctx, protocol.UpdateEntityArgs{PlanID: "p", Entity: &world.Entity{Name: "posts", Table: "articles", Fields: fields}})
	if r.OK || r.Kind != "conflict" {
		t.Fatalf("table onto page = %+v, want conflict", r)
	}
}

// The mirror: an entity whose CRUD mount lands on an existing page.
func TestEntityOnPagePathIsConflict(t *testing.T) {
	tools := newTools(t)
	ctx := context.Background()
	if r := tools.AddPage(ctx, protocol.AddPageArgs{Page: &world.Page{Path: "/api/posts", Tree: world.Node{Kind: "div"}}}); !r.OK {
		t.Fatalf("add_page: %+v", r)
	}
	r := tools.AddEntity(ctx, protocol.AddEntityArgs{Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}})
	if r.OK || r.Kind != "conflict" {
		t.Fatalf("entity on page path = %+v, want conflict", r)
	}
}
