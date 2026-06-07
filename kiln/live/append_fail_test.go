package live_test

import (
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// failAppendJournal wraps a real journal but fails the next Append after
// arming. Read/Len/etc. delegate so Replay still works.
type failAppendJournal struct {
	journal.Journal
	fail bool
}

func (f *failAppendJournal) Append(e journal.Entry) (int, error) {
	if f.fail {
		return 0, errors.New("disk full")
	}
	return f.Journal.Append(e)
}

// TestApplyRollsBackOnAppendFailure asserts that when the durable journal
// Append fails, the in-memory session is NOT left ahead of the log — state
// must never outlive the durable record. (finding k-live-1)
func TestApplyRollsBackOnAppendFailure(t *testing.T) {
	fj := &failAppendJournal{Journal: journal.NewMemory()}
	l, _ := newTestLive(t, fj)

	// Seed one durable entity so the session has a known baseline.
	base := newEntry(t, "1", time.Now(), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}})
	if err := l.Apply(base); err != nil {
		t.Fatalf("baseline Apply: %v", err)
	}

	// Arm the next Append to fail, then attempt a mutation.
	fj.fail = true
	bad := newEntry(t, "2", time.Now().Add(time.Second), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: &world.Entity{Name: "comments", Fields: []world.Field{{Name: "body", Type: "string"}}}})
	if err := l.Apply(bad); err == nil {
		t.Fatal("expected Apply to fail when Append fails")
	}

	// The durable journal holds exactly one entry (the baseline). The
	// in-memory session must agree: "comments" must NOT be present, because
	// it was never journaled.
	sess := l.Session()
	if sess.World.Entities["comments"] != nil {
		t.Error("session is ahead of journal: 'comments' present despite Append failure")
	}
	if sess.World.Entities["posts"] == nil {
		t.Error("baseline 'posts' should survive the rollback")
	}
}
