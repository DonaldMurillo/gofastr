package protocol

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// A patch journaled from a version another edit already replaced answers
// "conflict" with the refetch hint: the deterministic half of
// TestConcurrentPatchesKeepEdits, which cannot force the interleaving.
func TestStalePatchAnswersConflict(t *testing.T) {
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp() })
	if err != nil {
		t.Fatal(err)
	}
	tools := New(l)
	page := &world.Page{Path: "/p", Tree: world.Node{Kind: "div", Children: []world.Node{
		{ID: "a", Kind: "paragraph", Props: map[string]any{"text": "x"}},
	}}}
	if r := tools.AddPage(context.Background(), AddPageArgs{Page: page}); !r.OK {
		t.Fatalf("AddPage: %+v", r)
	}
	var v1 *world.Page
	l.ReadSession(func(s *journal.Session) { v1 = world.ClonePage(s.World.Pages["/p"]) })
	if r := tools.UpdatePageElement(context.Background(), UpdatePageElementArgs{
		Path: "/p", ElementID: "a",
		Patch: PageElementOp{Op: "set_props", SetProps: map[string]any{"text": "first"}},
	}); !r.OK {
		t.Fatalf("first patch: %+v", r)
	}
	stale := world.ClonePage(v1)
	stale.Version = 2
	r := tools.applyEdit(journal.OpUpdatePageElement, journal.UpdatePageElementPayload{Path: "/p", Prev: v1, New: stale})
	if r.OK || r.Kind != "conflict" || r.Hint != pageRefetchHint {
		t.Fatalf("stale patch = %+v, want conflict with the refetch hint", r)
	}
	var text any
	l.ReadSession(func(s *journal.Session) { text = s.World.Pages["/p"].Tree.Children[0].Props["text"] })
	if text != "first" {
		t.Fatalf("text = %v, want the first patch kept", text)
	}
}
