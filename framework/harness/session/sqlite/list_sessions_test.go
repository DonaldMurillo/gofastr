package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// TestListPastSessions: append events for two distinct sessions,
// verify ListPastSessions returns both with correct counts and the
// first TurnStarted's user text as FirstMessage.
func TestListPastSessions(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	sessA := ids.NewSessionID()
	sessB := ids.NewSessionID()

	// Session A: 3 events including a TurnStarted with content.
	contentA, _ := json.Marshal([]control.ContentBlock{{Type: "text", Text: "first session prompt"}})
	tsA, _ := control.EncodeEvent(1, control.TurnStarted{
		Turn: 1, Originator: ids.NewClientID(),
		Content: []control.ContentBlock{{Type: "text", Text: "first session prompt"}},
	}, sessA, ids.NewClientID(), time.Now().Add(-2*time.Hour))
	_ = contentA
	if err := store.AppendEvent(ctx, tsA); err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 3; i++ {
		env, _ := control.EncodeEvent(uint64(i), control.TextDelta{Text: "ok"},
			sessA, ids.NewClientID(), time.Now().Add(-1*time.Hour))
		if err := store.AppendEvent(ctx, env); err != nil {
			t.Fatal(err)
		}
	}

	// Session B: 1 event, newer.
	tsB, _ := control.EncodeEvent(1, control.TurnStarted{
		Turn: 1, Originator: ids.NewClientID(),
		Content: []control.ContentBlock{{Type: "text", Text: "second session"}},
	}, sessB, ids.NewClientID(), time.Now())
	if err := store.AppendEvent(ctx, tsB); err != nil {
		t.Fatal(err)
	}

	got, err := store.ListPastSessions(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d sessions, want 2: %+v", len(got), got)
	}
	// Newest-first ordering.
	if got[0].SessionID != sessB {
		t.Errorf("first row should be newest (sessB), got %s", got[0].SessionID)
	}
	if got[0].EventCount != 1 {
		t.Errorf("sessB event count = %d, want 1", got[0].EventCount)
	}
	if got[1].EventCount != 3 {
		t.Errorf("sessA event count = %d, want 3", got[1].EventCount)
	}
	if got[1].FirstMessage != "first session prompt" {
		t.Errorf("sessA first message = %q, want extracted from TurnStarted", got[1].FirstMessage)
	}
}

func TestListPastSessions_LimitRespected(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	// Insert 5 distinct sessions.
	for i := 0; i < 5; i++ {
		sess := ids.NewSessionID()
		env, _ := control.EncodeEvent(1, control.TextDelta{Text: "x"},
			sess, ids.NewClientID(), time.Now().Add(time.Duration(i)*time.Second))
		if err := store.AppendEvent(ctx, env); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := store.ListPastSessions(ctx, 3)
	if len(got) != 3 {
		t.Errorf("limit=3 returned %d rows", len(got))
	}
}
