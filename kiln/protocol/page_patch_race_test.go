package protocol_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Concurrent update_page_element calls each read the page, patch a clone,
// and journal the whole page. Two built from the same version both used to
// apply, the second overwriting the first's edit while both answered OK.
// Every OK must leave its edit in the page, and a loser must answer
// "conflict" (the refetch path), never "internal".
func TestConcurrentPatchesKeepEdits(t *testing.T) {
	tools := newTools(t)
	const n = 12
	kids := make([]world.Node, n)
	for i := range kids {
		kids[i] = world.Node{ID: fmt.Sprintf("p%d", i), Kind: "paragraph", Props: map[string]any{"text": "orig"}}
	}
	page := &world.Page{Path: "/race", Tree: world.Node{Kind: "div", Children: kids}}
	if res := tools.AddPage(context.Background(), protocol.AddPageArgs{Page: page}); !res.OK {
		t.Fatalf("AddPage: %+v", res)
	}
	for round := range 15 {
		want := fmt.Sprintf("r%d", round)
		start := make(chan struct{})
		results := make([]protocol.Result, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() {
				<-start
				results[i] = tools.UpdatePageElement(context.Background(), protocol.UpdatePageElementArgs{
					Path:      "/race",
					ElementID: fmt.Sprintf("p%d", i),
					Patch:     protocol.PageElementOp{Op: "set_props", SetProps: map[string]any{"text": want}},
				})
			})
		}
		close(start)
		wg.Wait()
		var got *world.Page
		tools.Live().ReadSession(func(s *journal.Session) { got = world.ClonePage(s.World.Pages["/race"]) })
		oks := 0
		for i, r := range results {
			if !r.OK {
				if r.Kind != "conflict" {
					t.Fatalf("round %d patch %d: kind %q (%s), want conflict", round, i, r.Kind, r.Error)
				}
				continue
			}
			oks++
			if txt := got.Tree.Children[i].Props["text"]; txt != want {
				t.Fatalf("round %d: patch %d answered OK but its edit is gone (text=%v)", round, i, txt)
			}
		}
		if oks == 0 {
			t.Fatalf("round %d: no patch applied", round)
		}
	}
}
