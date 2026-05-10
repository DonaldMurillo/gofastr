package protocol_test

import (
	"context"
	"testing"

	"github.com/gofastr/gofastr/kiln/protocol"
	"github.com/gofastr/gofastr/kiln/world"
)

// makeDashboard adds a small page to the world and returns the live
// page so tests can read the assigned _id values without re-fetching.
func makeDashboard(t *testing.T, tools *protocol.Tools) *world.Page {
	t.Helper()
	page := &world.Page{
		Path: "/dashboard",
		Tree: world.Node{
			Kind: "div",
			Children: []world.Node{
				{Kind: "heading", Props: map[string]any{"level": 1, "text": "Dashboard"}},
				{Kind: "div", Children: []world.Node{
					{Kind: "link", Props: map[string]any{"href": "/posts", "text": "Open posts"}},
					{Kind: "link", Props: map[string]any{"href": "/users", "text": "Open users"}},
				}},
			},
		},
	}
	res := tools.AddPage(context.Background(), protocol.AddPageArgs{Page: page})
	if !res.OK {
		t.Fatalf("AddPage: %+v", res)
	}
	live := tools.Live().Session().World.Pages["/dashboard"]
	if live == nil {
		t.Fatal("dashboard not in world after AddPage")
	}
	return live
}

func TestAddPage_AssignsIDsAndVersionOne(t *testing.T) {
	tools := newTools(t)
	page := makeDashboard(t, tools)
	if page.Version != 1 {
		t.Errorf("Version = %d, want 1 on fresh add_page", page.Version)
	}
	if page.Tree.ID == "" {
		t.Error("root id missing — every node should have a stable _id after add_page")
	}
	for _, c := range page.Tree.Children {
		if c.ID == "" {
			t.Errorf("child %q has no _id", c.Kind)
		}
	}
	// Nested children too.
	for _, link := range page.Tree.Children[1].Children {
		if link.ID == "" {
			t.Errorf("nested link has no _id")
		}
	}
}

func TestAddPage_PreservesUserProvidedIDs(t *testing.T) {
	tools := newTools(t)
	page := &world.Page{
		Path: "/x",
		Tree: world.Node{
			ID:   "my-root",
			Kind: "div",
			Children: []world.Node{
				{ID: "user-link", Kind: "link", Props: map[string]any{"href": "/", "text": "home"}},
				{Kind: "paragraph"}, // unidentified — should be auto-assigned
			},
		},
	}
	if res := tools.AddPage(context.Background(), protocol.AddPageArgs{Page: page}); !res.OK {
		t.Fatalf("AddPage: %+v", res)
	}
	live := tools.Live().Session().World.Pages["/x"]
	if live.Tree.ID != "my-root" {
		t.Errorf("root id = %q, want preserved %q", live.Tree.ID, "my-root")
	}
	if live.Tree.Children[0].ID != "user-link" {
		t.Errorf("user-link id = %q, want preserved", live.Tree.Children[0].ID)
	}
	if live.Tree.Children[1].ID == "" {
		t.Error("paragraph id missing — should have been auto-assigned")
	}
}

func TestUpdatePageElement_SetProps(t *testing.T) {
	tools := newTools(t)
	page := makeDashboard(t, tools)
	link := &page.Tree.Children[1].Children[0] // first link, /posts

	res := tools.UpdatePageElement(context.Background(), protocol.UpdatePageElementArgs{
		Path:      "/dashboard",
		ElementID: link.ID,
		Patch: protocol.PageElementOp{
			Op:       "set_props",
			SetProps: map[string]any{"href": "/posts/list"},
		},
	})
	if !res.OK {
		t.Fatalf("UpdatePageElement: %+v", res)
	}

	updated := tools.Live().Session().World.Pages["/dashboard"]
	if updated.Version != 2 {
		t.Errorf("Version = %d, want 2 after one update", updated.Version)
	}
	got := updated.Tree.Children[1].Children[0].Props["href"]
	if got != "/posts/list" {
		t.Errorf("href = %v, want /posts/list", got)
	}
	// set_props is a merge, so other props should still exist.
	if updated.Tree.Children[1].Children[0].Props["text"] != "Open posts" {
		t.Errorf("text was wiped by set_props (should merge, not replace)")
	}
}

func TestUpdatePageElement_ReplaceProps(t *testing.T) {
	tools := newTools(t)
	page := makeDashboard(t, tools)
	link := &page.Tree.Children[1].Children[0]

	res := tools.UpdatePageElement(context.Background(), protocol.UpdatePageElementArgs{
		Path:      "/dashboard",
		ElementID: link.ID,
		Patch: protocol.PageElementOp{
			Op:       "replace_props",
			SetProps: map[string]any{"href": "/p"},
		},
	})
	if !res.OK {
		t.Fatalf("UpdatePageElement: %+v", res)
	}
	props := tools.Live().Session().World.Pages["/dashboard"].
		Tree.Children[1].Children[0].Props
	if props["href"] != "/p" {
		t.Errorf("href = %v, want /p", props["href"])
	}
	if _, has := props["text"]; has {
		t.Errorf("replace_props should drop keys not in set_props; text still present: %v", props)
	}
}

func TestUpdatePageElement_ReplaceSubtree_PreservesID(t *testing.T) {
	tools := newTools(t)
	page := makeDashboard(t, tools)
	originalID := page.Tree.Children[0].ID // heading

	res := tools.UpdatePageElement(context.Background(), protocol.UpdatePageElementArgs{
		Path:      "/dashboard",
		ElementID: originalID,
		Patch: protocol.PageElementOp{
			Op: "replace_subtree",
			Element: &world.Node{
				Kind:  "heading",
				Props: map[string]any{"level": 2, "text": "Welcome back"},
				Children: []world.Node{
					{Kind: "span", Props: map[string]any{"text": "(beta)"}},
				},
			},
		},
	})
	if !res.OK {
		t.Fatalf("UpdatePageElement: %+v", res)
	}
	updated := tools.Live().Session().World.Pages["/dashboard"]
	heading := updated.Tree.Children[0]
	if heading.ID != originalID {
		t.Errorf("ID changed by replace_subtree: was %q, now %q. The id should survive a content swap so cross-call references stay valid.", originalID, heading.ID)
	}
	if heading.Props["level"].(float64) != 2 && heading.Props["level"] != 2 {
		// JSON round-trip can produce float64 or int — accept either
		t.Errorf("level = %v, want 2", heading.Props["level"])
	}
	if len(heading.Children) != 1 || heading.Children[0].Kind != "span" {
		t.Errorf("subtree not replaced; children = %+v", heading.Children)
	}
	// New child should have an _id auto-assigned.
	if heading.Children[0].ID == "" {
		t.Error("inserted span has no _id — AssignNodeIDs should run after every patch")
	}
}

func TestUpdatePageElement_Remove(t *testing.T) {
	tools := newTools(t)
	page := makeDashboard(t, tools)
	usersLink := &page.Tree.Children[1].Children[1] // second link

	res := tools.UpdatePageElement(context.Background(), protocol.UpdatePageElementArgs{
		Path:      "/dashboard",
		ElementID: usersLink.ID,
		Patch:     protocol.PageElementOp{Op: "remove"},
	})
	if !res.OK {
		t.Fatalf("UpdatePageElement: %+v", res)
	}
	links := tools.Live().Session().World.Pages["/dashboard"].
		Tree.Children[1].Children
	if len(links) != 1 {
		t.Errorf("len(links) = %d, want 1 after remove", len(links))
	}
	if len(links) == 1 && links[0].Props["href"] != "/posts" {
		t.Errorf("wrong link removed; remaining = %+v", links[0])
	}
}

func TestUpdatePageElement_RemoveRoot_Rejected(t *testing.T) {
	tools := newTools(t)
	page := makeDashboard(t, tools)
	res := tools.UpdatePageElement(context.Background(), protocol.UpdatePageElementArgs{
		Path:      "/dashboard",
		ElementID: page.Tree.ID,
		Patch:     protocol.PageElementOp{Op: "remove"},
	})
	if res.OK {
		t.Fatal("removing the root should be rejected; use delete_page instead")
	}
	if res.Kind != "validation" {
		t.Errorf("kind = %q, want validation", res.Kind)
	}
}

func TestUpdatePageElement_InsertBefore(t *testing.T) {
	tools := newTools(t)
	page := makeDashboard(t, tools)
	postsLink := &page.Tree.Children[1].Children[0]

	res := tools.UpdatePageElement(context.Background(), protocol.UpdatePageElementArgs{
		Path:      "/dashboard",
		ElementID: postsLink.ID,
		Patch: protocol.PageElementOp{
			Op: "insert_before",
			Element: &world.Node{
				Kind:  "link",
				Props: map[string]any{"href": "/inbox", "text": "Inbox"},
			},
		},
	})
	if !res.OK {
		t.Fatalf("UpdatePageElement: %+v", res)
	}
	links := tools.Live().Session().World.Pages["/dashboard"].
		Tree.Children[1].Children
	if len(links) != 3 {
		t.Fatalf("len(links) = %d, want 3", len(links))
	}
	if links[0].Props["href"] != "/inbox" {
		t.Errorf("Inbox should now be first; got %+v", links)
	}
	if links[0].ID == "" {
		t.Error("inserted element has no _id")
	}
}

func TestUpdatePageElement_InsertAfter(t *testing.T) {
	tools := newTools(t)
	page := makeDashboard(t, tools)
	postsLink := &page.Tree.Children[1].Children[0]

	res := tools.UpdatePageElement(context.Background(), protocol.UpdatePageElementArgs{
		Path:      "/dashboard",
		ElementID: postsLink.ID,
		Patch: protocol.PageElementOp{
			Op: "insert_after",
			Element: &world.Node{
				Kind:  "link",
				Props: map[string]any{"href": "/tags", "text": "Tags"},
			},
		},
	})
	if !res.OK {
		t.Fatalf("UpdatePageElement: %+v", res)
	}
	links := tools.Live().Session().World.Pages["/dashboard"].
		Tree.Children[1].Children
	if len(links) != 3 {
		t.Fatalf("len(links) = %d, want 3", len(links))
	}
	if links[1].Props["href"] != "/tags" {
		t.Errorf("Tags should be second; got %+v", links)
	}
}

func TestUpdatePageElement_AppendChild(t *testing.T) {
	tools := newTools(t)
	page := makeDashboard(t, tools)
	res := tools.UpdatePageElement(context.Background(), protocol.UpdatePageElementArgs{
		Path:      "/dashboard",
		ElementID: page.Tree.ID, // append to root
		Patch: protocol.PageElementOp{
			Op:      "append_child",
			Element: &world.Node{Kind: "footer", Props: map[string]any{"text": "© 2026"}},
		},
	})
	if !res.OK {
		t.Fatalf("UpdatePageElement: %+v", res)
	}
	root := tools.Live().Session().World.Pages["/dashboard"].Tree
	if len(root.Children) != 3 {
		t.Errorf("len(root.children) = %d, want 3 after append", len(root.Children))
	}
	if root.Children[2].Kind != "footer" {
		t.Errorf("footer should be last child; got %+v", root.Children[2])
	}
}

func TestUpdatePageElement_IfMatch_Mismatch(t *testing.T) {
	tools := newTools(t)
	page := makeDashboard(t, tools)
	link := &page.Tree.Children[1].Children[0]

	stale := 7 // Page is at version 1
	res := tools.UpdatePageElement(context.Background(), protocol.UpdatePageElementArgs{
		Path:      "/dashboard",
		ElementID: link.ID,
		IfMatch:   &stale,
		Patch: protocol.PageElementOp{
			Op:       "set_props",
			SetProps: map[string]any{"href": "/x"},
		},
	})
	if res.OK {
		t.Fatal("expected conflict on stale if_match")
	}
	if res.Kind != "conflict" {
		t.Errorf("kind = %q, want conflict", res.Kind)
	}
	// World must not have been mutated.
	current := tools.Live().Session().World.Pages["/dashboard"]
	if current.Version != 1 {
		t.Errorf("Version = %d, want 1 (mutation should have been rejected)", current.Version)
	}
	if current.Tree.Children[1].Children[0].Props["href"] != "/posts" {
		t.Error("href was mutated despite if_match rejection")
	}
}

func TestUpdatePageElement_IfMatch_Match(t *testing.T) {
	tools := newTools(t)
	page := makeDashboard(t, tools)
	link := &page.Tree.Children[1].Children[0]

	v := 1
	res := tools.UpdatePageElement(context.Background(), protocol.UpdatePageElementArgs{
		Path:      "/dashboard",
		ElementID: link.ID,
		IfMatch:   &v,
		Patch: protocol.PageElementOp{
			Op:       "set_props",
			SetProps: map[string]any{"href": "/posts/list"},
		},
	})
	if !res.OK {
		t.Fatalf("expected OK with matching if_match, got %+v", res)
	}
}

func TestUpdatePageElement_UnknownPage(t *testing.T) {
	tools := newTools(t)
	res := tools.UpdatePageElement(context.Background(), protocol.UpdatePageElementArgs{
		Path:      "/nope",
		ElementID: "el_x",
		Patch:     protocol.PageElementOp{Op: "set_props", SetProps: map[string]any{"x": 1}},
	})
	if res.OK || res.Kind != "not_found" {
		t.Errorf("res = %+v, want not_found", res)
	}
}

func TestUpdatePageElement_UnknownElement(t *testing.T) {
	tools := newTools(t)
	makeDashboard(t, tools)
	res := tools.UpdatePageElement(context.Background(), protocol.UpdatePageElementArgs{
		Path:      "/dashboard",
		ElementID: "el_does_not_exist",
		Patch:     protocol.PageElementOp{Op: "set_props", SetProps: map[string]any{"x": 1}},
	})
	if res.OK || res.Kind != "not_found" {
		t.Errorf("res = %+v, want not_found", res)
	}
}

// Pin that update_page_element is wired into the agent dispatcher
// (kiln/agent/loop.go) and the descriptor table — without these, a
// new tool method on Tools is silently invisible to MCP/ACP/the panel.
func TestUpdatePageElement_DescriptorRegistered(t *testing.T) {
	tools := newTools(t)
	if _, ok := tools.Describe("update_page_element"); !ok {
		t.Error("update_page_element missing from descriptor table — MCP and the panel won't see it")
	}
	d, _ := tools.Describe("update_page_element")
	if d.Destructive {
		t.Error("update_page_element marked Destructive — page edits don't lose persisted data and shouldn't trigger the plan flow")
	}
}
