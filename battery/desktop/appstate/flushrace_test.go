package appstate

import (
	"path/filepath"
	"testing"
	"time"
)

// The quit-time flush contract. Flush is the last call before the
// process exits (battery/desktop Run), so two orderings must hold:
//
//  1. Flush does not return while a persist is in flight: the quit path
//     must not let the process die inside os.WriteFile, which would
//     truncate the file and empty the whole store on the next launch.
//  2. A write's snapshot cannot outlive a newer flush's persist: if the
//     debounced timer's write snapshots, then a Set plus Flush lands a
//     newer file, the older snapshot must never be persisted after it
//     (the Flush stopped the timer, so nothing would rescue the Set).

// TestFlushWaitsForInFlightPersist: an in-flight persist (writeMu held,
// snapshot already taken, file being written) must block Flush.
func TestFlushWaitsForInFlightPersist(t *testing.T) {
	setDelay(t, time.Hour)
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := s.Set("page.a", 1); err != nil {
		t.Fatal(err)
	}

	// One write in the real shape: the snapshot is taken, then the
	// persist runs under writeMu. persisted is buffered so the send
	// happens while the goroutine still holds writeMu.
	persisted := make(chan struct{}, 1)
	go func() {
		s.writeMu.Lock()
		defer s.writeMu.Unlock()
		s.mu.Lock()
		entries := s.snapshotLocked()
		s.mu.Unlock()
		_ = s.persist(entries)
		persisted <- struct{}{}
	}()
	waitFor(t, "the in-flight write to take its snapshot", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.dirty
	})

	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-persisted:
	default:
		t.Fatal("Flush returned while a persist was in flight; the quit path can exit mid-write")
	}
}

// TestWriteSnapshotsUnderWriteLock: the timer callback's snapshot must
// happen under writeMu, not just mu. Otherwise the fired timer can
// snapshot while an older persist runs, then land its own persist after
// a newer Flush's (the quit flush stopped the timer, so the Set that
// armed it is gone from disk with nothing left to write it).
func TestWriteSnapshotsUnderWriteLock(t *testing.T) {
	setDelay(t, time.Hour)
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := s.Set("page.a", 1); err != nil {
		t.Fatal(err)
	}

	// A persist is in flight: writeMu is held by someone else.
	s.writeMu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.write()
	}()

	// Give the timer callback ample time to reach its snapshot. Under
	// the fixed shape it blocks on writeMu first, so dirty stays set;
	// under the broken shape it snapshots under mu alone and clears
	// dirty while the older persist still holds the write lock.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		dirty := s.dirty
		s.mu.Unlock()
		if !dirty {
			s.writeMu.Unlock()
			<-done
			t.Fatal("write() took its snapshot without writeMu; a stale snapshot can land after a newer flush")
		}
		time.Sleep(2 * time.Millisecond)
	}

	s.writeMu.Unlock()
	<-done
	f := readFile(t, s.Path())
	if f.Entries["page.a"] == nil {
		t.Fatalf("state file entries = %v, want page.a", f.Entries)
	}
}
