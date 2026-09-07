//go:build red && race

package protocol_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// RACE-TAGGED: only compiles under red+race; the -race detector's DATA RACE
// report IS the red proof.
// Property: protocol.Tools is safe for concurrent use — protocol.go:28 says
// so verbatim ("It is safe for concurrent use") and the transport
// (kiln/chat/server.go) serves each request in its own goroutine with no
// serialization: the operator panel and the agent curl the same Tools value
// concurrently.
// Surfaces: kiln/protocol/protocol.go::Tools (every method), specifically
// WorldGet + AddPage as the representative read/write pair.
// Finding: Tools.mu (protocol.go:32) is declared and never locked anywhere
// (no t.mu. occurrence in the file). Every Tools method reads
// t.live.Session() UNLOCKED — Session() RLocks only long enough to return
// the *journal.Session pointer — and then walks sess.World maps with no
// lock, while applyEntry→live.Apply holds the write lock and mutates the
// SAME session in place (live.go:120 journal.Apply(l.sess, e) does
// w.Pages[path]=page etc.). Concurrent map read/write is a fatal
// unrecoverable throw: two concurrent curls (agent + operator panel) kill
// the whole kiln process.
// Fix direction: lock Tools.mu (or route reads through live.ReadSession)
// so a Tools call's session read can't interleave with Apply's in-place
// world mutation.
func TestKilnToolsRedConcurrentUse(t *testing.T) {
	tools := newTools(t)
	ctx := context.Background()

	const n = 300
	var wg sync.WaitGroup
	var addErrs, getErrs int
	var mu sync.Mutex // guards the two counters only

	wg.Add(2)
	go func() { // writer: the agent's add_page hammer
		defer wg.Done()
		for i := range n {
			res := tools.AddPage(ctx, protocol.AddPageArgs{Page: &world.Page{
				Path: fmt.Sprintf("/p%d", i),
				Tree: world.Node{Kind: "div"},
			}})
			if !res.OK {
				mu.Lock()
				addErrs++
				mu.Unlock()
			}
		}
	}()
	go func() { // reader: the operator panel polling /kiln/world
		defer wg.Done()
		for i := range n {
			res := tools.WorldGet(ctx, protocol.WorldGetArgs{
				Path: fmt.Sprintf("pages./p%d", i%50),
			})
			if !res.OK && res.Kind != "not_found" {
				mu.Lock()
				getErrs++
				mu.Unlock()
			}
		}
	}()
	wg.Wait()

	if addErrs > 0 {
		t.Fatalf("setup broken: %d/%d add_page calls failed — fixture noise, not the race under test", addErrs, n)
	}
	if getErrs > 0 {
		t.Fatalf("setup broken: %d/%d world_get calls failed unexpectedly — fixture noise, not the race under test", getErrs, n)
	}
	// Red proof is the -race detector's DATA RACE report on the World.Pages
	// map access (writer: journal.Apply via live.Apply; reader: WorldGet's
	// unlocked Session() walk) — it fails this run today. When Tools.mu is
	// actually locked, both loops complete cleanly and this test goes green.
}
