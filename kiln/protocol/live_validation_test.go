package protocol_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Every input below is refused by journal replay or the render step, and
// the live tool used to let it through to Apply, which mapped the refusal
// to kind "internal" with the generic "internal error" text. The caller
// could not tell a fixable argument from a server fault. Each must answer
// kind "validation" with the reason.
func TestLiveEditGapsAnswerValidation(t *testing.T) {
	hook := func(id, entity, when, kind string) protocol.AddHookArgs {
		return protocol.AddHookArgs{Hook: &world.Hook{ID: id, Entity: entity, When: when, Action: world.Action{Kind: kind}}}
	}
	page := func(path string, actionKind string) protocol.AddPageArgs {
		tree := world.Node{Kind: "div"}
		if actionKind != "" {
			tree.Children = []world.Node{{Kind: "button", Actions: map[string]world.Action{"click": {Kind: actionKind}}}}
		}
		return protocol.AddPageArgs{Page: &world.Page{Path: path, Tree: tree}}
	}
	cases := []struct {
		name string
		call func(context.Context, *protocol.Tools) protocol.Result
		want string
	}{
		{"route lowercase method", func(ctx context.Context, tl *protocol.Tools) protocol.Result {
			return tl.AddRoute(ctx, protocol.AddRouteArgs{Route: &world.Route{Method: "get", Path: "/x"}})
		}, "method must be one of"},
		{"route relative path", func(ctx context.Context, tl *protocol.Tools) protocol.Result {
			return tl.AddRoute(ctx, protocol.AddRouteArgs{Route: &world.Route{Method: "GET", Path: "x"}})
		}, "must start with /"},
		{"route malformed pattern", func(ctx context.Context, tl *protocol.Tools) protocol.Result {
			return tl.AddRoute(ctx, protocol.AddRouteArgs{Route: &world.Route{Method: "GET", Path: "/x/{oops"}})
		}, "/x/{oops"},
		{"page brace param", func(ctx context.Context, tl *protocol.Tools) protocol.Result {
			return tl.AddPage(ctx, page("/users/{id}", ""))
		}, "/users/{id}"},
		{"hook bogus action", func(ctx context.Context, tl *protocol.Tools) protocol.Result {
			return tl.AddHook(ctx, hook("h1", "posts", "before_create", "bogus"))
		}, `"bogus" is not supported`},
		{"hook unknown when", func(ctx context.Context, tl *protocol.Tools) protocol.Result {
			return tl.AddHook(ctx, hook("h1", "posts", "whenever", "noop"))
		}, `unknown hook event "whenever"`},
		{"hook empty entity", func(ctx context.Context, tl *protocol.Tools) protocol.Result {
			return tl.AddHook(ctx, hook("h1", "", "before_create", "noop"))
		}, "missing entity"},
		{"page bogus node action", func(ctx context.Context, tl *protocol.Tools) protocol.Result {
			return tl.AddPage(ctx, page("/about", "bogus"))
		}, `"bogus" is not supported`},
		{"page relative path", func(ctx context.Context, tl *protocol.Tools) protocol.Result {
			return tl.AddPage(ctx, page("about", ""))
		}, "must start with /"},
		{"element patch bogus action", func(ctx context.Context, tl *protocol.Tools) protocol.Result {
			if res := tl.AddPage(ctx, page("/about", "")); !res.OK {
				return res
			}
			var root string
			tl.Live().ReadSession(func(s *journal.Session) { root = s.World.Pages["/about"].Tree.ID })
			return tl.UpdatePageElement(ctx, protocol.UpdatePageElementArgs{
				Path: "/about", ElementID: root,
				Patch: protocol.PageElementOp{Op: "append_child", Element: &world.Node{
					Kind: "button", Actions: map[string]world.Action{"click": {Kind: "bogus"}},
				}},
			})
		}, `"bogus" is not supported`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools := newTools(t)
			res := tc.call(t.Context(), tools)
			if res.OK {
				t.Fatalf("accepted, want a validation refusal")
			}
			if res.Kind != "validation" || !strings.Contains(res.Error, tc.want) {
				t.Errorf("got kind %q error %q, want kind validation naming %q", res.Kind, res.Error, tc.want)
			}
		})
	}
}

// An action runs on one surface: a hook runs a side effect (effect.Run),
// a route answers with a response (effect.Resolve). One shared kind set
// let a hook take respond_json and a route take validate, and both then
// failed on every request. Each surface is checked against its own set.
func TestActionKindCheckedPerSurface(t *testing.T) {
	tools := newTools(t)
	hook := tools.AddHook(t.Context(), protocol.AddHookArgs{Hook: &world.Hook{
		ID: "h1", Entity: "posts", When: "before_create", Action: world.Action{Kind: world.ActionRespondJSON},
	}})
	if hook.OK || hook.Kind != "validation" {
		t.Errorf("add_hook with respond_json: got %+v, want a validation refusal", hook)
	}
	route := tools.AddRoute(t.Context(), protocol.AddRouteArgs{Route: &world.Route{
		Method: "GET", Path: "/x", Action: world.Action{Kind: world.ActionValidate},
	}})
	if route.OK || route.Kind != "validation" {
		t.Errorf("add_route with validate: got %+v, want a validation refusal", route)
	}
	// The kinds each surface runs still land.
	if res := tools.AddHook(t.Context(), protocol.AddHookArgs{Hook: &world.Hook{
		ID: "h2", Entity: "posts", When: "before_create", Action: world.Action{Kind: world.ActionAudit},
	}}); !res.OK {
		t.Errorf("add_hook with audit refused: %+v", res)
	}
	if res := tools.AddRoute(t.Context(), protocol.AddRouteArgs{Route: &world.Route{
		Method: "GET", Path: "/y", Action: world.Action{Kind: world.ActionRespondJSON},
	}}); !res.OK {
		t.Errorf("add_route with respond_json refused: %+v", res)
	}
}
