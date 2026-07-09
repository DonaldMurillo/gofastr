package crud

// Regression for the ownerId type-fidelity fix (F1): an entity whose owner
// extractor returns a numeric id (a BIGINT user key) must still deliver
// cross-replica CRUD events to that owner's SSE EventStream. Before the fix
// the ownerId was stamped as int64, crossed the fanout bridge's JSON
// round-trip as float64, and EventStream's `!=` filter (interface{} compare)
// found float64(7) != int64(7) → always unequal → every remote event dropped.

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// installInt64OwnerExtractor maps the test user "alice" to int64(7) — the
// shape a host whose users table has a BIGINT primary key produces.
func installInt64OwnerExtractor(t *testing.T) {
	t.Helper()
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		raw, ok := handler.GetUser(ctx)
		if !ok || raw == nil {
			return nil, false
		}
		if u, ok := raw.(*testUser); ok && u.GetID() == "alice" {
			return int64(7), true
		}
		return nil, false
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })
}

// TestEventStream_DeliversRemoteIntOwnerEvent: two replicas share one fanout.
// Replica B holds alice's SSE EventStream; replica A performs the write and
// emits the CRUD event through the real EmitEvent → eventData path. The event
// matches alice's owner (int64 7) on both sides and reaches her stream —
// ownerId is stamped and compared as a string, so the JSON round-trip cannot
// retype it.
func TestEventStream_DeliversRemoteIntOwnerEvent(t *testing.T) {
	chB, _ := covOwnerNotesHandler(t) // replica B: serves the SSE stream
	chA, _ := covOwnerNotesHandler(t) // replica A: performs the write
	installInt64OwnerExtractor(t)     // installed AFTER the helpers' string extractor

	f := fanout.NewInProcess()
	busA := event.NewEventBus()
	busB := event.NewEventBus()
	stopA, err := event.AttachFanout(busA, f)
	if err != nil {
		t.Fatal(err)
	}
	defer stopA()
	stopB, err := event.AttachFanout(busB, f)
	if err != nil {
		t.Fatal(err)
	}
	defer stopB()
	chA.Events = busA
	chB.Events = busB

	ctx, cancel := context.WithCancel(ctxWithUser("alice"))
	req := httptest.NewRequest("GET", "/onotes/_events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { chB.EventStream()(rec, req); close(done) }()
	time.Sleep(100 * time.Millisecond) // let the subscription register

	// Replica A: alice's own write, real CRUD event payload (ownerId int64→string).
	chA.EmitEvent(ctxWithUser("alice"), event.EntityCreated, map[string]any{"id": "remote-n1", "user_id": "alice"})
	time.Sleep(300 * time.Millisecond)

	// Control: the same event emitted locally on replica B IS delivered.
	chB.EmitEvent(ctxWithUser("alice"), event.EntityCreated, map[string]any{"id": "local-n2", "user_id": "alice"})
	time.Sleep(300 * time.Millisecond)

	cancel()
	<-done
	body := rec.Body.String()

	if !strings.Contains(body, "local-n2") {
		t.Fatalf("control failed: local event not delivered — harness problem, not a finding. body=%s", body)
	}
	if !strings.Contains(body, "remote-n1") {
		t.Errorf("alice's own event from replica A never reached her SSE stream on B (ownerId int64 should survive the bridge as a string). body=%s", body)
	}
}
