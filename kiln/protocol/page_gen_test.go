package protocol_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Replay tells a re-added page from the one a patch was read from by its
// generation (2026-10-27 K20), so add_page mints a fresh one every time
// and never keeps one the caller sent: a copied generation would let a
// stale patch through again.
func TestAddPageMintsGeneration(t *testing.T) {
	tools := newTools(t)
	for _, path := range []string{"/a", "/b"} {
		page := &world.Page{Path: path, Gen: "copied", Tree: world.Node{Kind: "div"}}
		if res := tools.AddPage(context.Background(), protocol.AddPageArgs{Page: page}); !res.OK {
			t.Fatalf("AddPage %s: %+v", path, res)
		}
	}
	var a, b string
	tools.Live().ReadSession(func(s *journal.Session) { a, b = s.World.Pages["/a"].Gen, s.World.Pages["/b"].Gen })
	if a == "" || a == "copied" || b == "copied" || a == b {
		t.Fatalf("generations = %q, %q, want two fresh distinct ones", a, b)
	}
}
