package main

import (
	"net/http"
	"sync"
	"testing"
)

// applyCommand looked the session up under one lock and wrote to it under
// a second, so a sweep in between dropped the session and the write landed
// on the orphan while the caller got 200, not the 410 the nil branch
// promises (2026-10-27 E1). A 200 must mean the command reached a session
// that was still live: the drop then sees the write it answered for.
func TestCommandOnDroppedSessionIsGone(t *testing.T) {
	a := newAssistApp()
	const commanders = 8
	for i := 0; i < 3000; i++ {
		s := a.createSession()
		var wg sync.WaitGroup
		statuses := make(chan int, commanders)
		for range commanders {
			wg.Go(func() {
				_, st, _ := a.applyCommand(assistCommand{Session: s.id, Kind: cmdInstruction, Instruction: "look left"})
				statuses <- st
			})
		}
		var seqAtDrop uint64
		dropped := false
		for !dropped {
			a.mu.Lock()
			if s.seq >= commanders/2 {
				seqAtDrop = uint64(s.seq)
				a.dropLocked(s)
				dropped = true
			}
			a.mu.Unlock()
		}
		wg.Wait()
		close(statuses)
		ok := 0
		for st := range statuses {
			if st == http.StatusOK {
				ok++
			}
		}
		if uint64(ok) != seqAtDrop {
			t.Fatalf("iteration %d: %d commands answered 200, the session saw %d writes before it was dropped", i, ok, seqAtDrop)
		}
	}
}
