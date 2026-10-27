package a2a

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// A subscribe on the replica whose run asked for input, after another
// replica resumed the task, sees the resumed run pause even while the
// local handler is still unwinding. It attached to the stale local run,
// whose watcher reacts only to terminal states, and read the store only
// once that run ended: B's INPUT_REQUIRED never reached the stream while
// A's handler held on.
func TestSubscribeSeesRemotePauseUnderStaleRun(t *testing.T) {
	db := openSQLite(t)
	store, err := NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	newReplica := func() *harness {
		var h *harness
		h = newHarness(t, func(c *Config) {
			c.Store = store
			c.Skills = []Skill{{
				ID: "echo", Name: "Echo", Description: "echo",
				Handler: func(ctx context.Context, tc TaskContext) error { return h.current()(ctx, tc) },
			}}
		})
		h.srv.pollEvery = 20 * time.Millisecond
		return h
	}
	a, b := newReplica(), newReplica()
	asked, aReturn, pause := make(chan struct{}), make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(aReturn) })
	a.setHandler(func(_ context.Context, tc TaskContext) error {
		err := tc.RequireInput(TextPart("need input"))
		close(asked)
		<-aReturn // still unwinding for the whole test
		return err
	})
	b.setHandler(func(_ context.Context, tc TaskContext) error {
		<-pause
		return tc.RequireInput(TextPart("need more"))
	})
	task := a.send("alice", map[string]any{"returnImmediately": true})
	<-asked
	if status, e, raw := b.call("alice", MethodSendMessage, map[string]any{
		"message":       map[string]any{"taskId": task.ID, "role": "ROLE_USER", "parts": []any{map[string]any{"text": "answer"}}},
		"configuration": map[string]any{"returnImmediately": true},
	}); status != 200 || e.Error != nil {
		t.Fatalf("resume on B: status=%d body=%s", status, raw)
	}
	r := a.openStream("alice", MethodSubscribeToTask, map[string]any{"id": task.ID})
	if _, sr := r.nextResult(2 * time.Second); sr.Task == nil || sr.Task.Status.State != TaskStateWorking {
		t.Fatalf("snapshot = %+v, want the resumed WORKING task", sr)
	}
	close(pause)
	for deadline := time.Now().Add(3 * time.Second); ; {
		if time.Now().After(deadline) {
			t.Fatal("stream never delivered B's INPUT_REQUIRED while A's handler unwinds")
		}
		ev, ok := r.next(3 * time.Second)
		if !ok {
			t.Fatal("stream closed before B's INPUT_REQUIRED")
		}
		if ev.comment != "" {
			continue
		}
		var e env
		var sr StreamResponse
		if json.Unmarshal([]byte(ev.data), &e) != nil || json.Unmarshal(e.Result, &sr) != nil {
			t.Fatalf("bad event %q", ev.data)
		}
		if (sr.Task != nil && sr.Task.Status.State == TaskStateInputRequired) ||
			(sr.StatusUpdate != nil && sr.StatusUpdate.Status.State == TaskStateInputRequired) {
			return
		}
	}
}
