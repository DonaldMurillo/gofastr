package a2a

import (
	"context"
	"strings"
	"testing"
	"time"
)

// streamSendParams builds a SendStreamingMessage params object.
func streamSendParams(taskID string) map[string]any {
	msg := map[string]any{
		"role":     "ROLE_USER",
		"parts":    []any{map[string]any{"text": "hi"}},
		"metadata": map[string]any{"skill": "echo"},
	}
	if taskID != "" {
		msg["taskId"] = taskID
	}
	return map[string]any{"message": msg}
}

// TestStreamEventSequence pins the streaming shape: task → WORKING →
// artifact → COMPLETED → close, each event a JSON-RPC response echoing
// the request id, each data payload one line (no raw newline).
func TestStreamEventSequence(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Working(TextPart("working")); err != nil {
			return err
		}
		if err := tc.Artifact(Artifact{ArtifactID: "a1", Parts: []Part{TextPart("chunk")}}, false); err != nil {
			return err
		}
		return tc.Complete(TextPart("done"))
	})
	r := h.openStream("alice", MethodSendStreamingMessage, streamSendParams(""))

	e, sr := r.nextResult(2 * time.Second)
	if e.Error != nil || string(e.ID) != `"call-1"` {
		t.Fatalf("first event id/error: %s %+v", e.ID, e.Error)
	}
	if sr.Task == nil || sr.Task.Status.State != TaskStateSubmitted {
		t.Fatalf("first event = %+v, want SUBMITTED task", sr)
	}

	e, sr = r.nextResult(2 * time.Second)
	if e.Error != nil {
		t.Fatalf("second event error: %+v", e.Error)
	}
	if sr.StatusUpdate == nil || sr.StatusUpdate.Status.State != TaskStateWorking {
		t.Fatalf("second event = %+v, want WORKING statusUpdate", sr)
	}

	e, sr = r.nextResult(2 * time.Second)
	if e.Error != nil {
		t.Fatalf("third event error: %+v", e.Error)
	}
	if sr.ArtifactUpdate == nil || sr.ArtifactUpdate.Artifact.ArtifactID != "a1" {
		t.Fatalf("third event = %+v, want artifactUpdate", sr)
	}

	e, sr = r.nextResult(2 * time.Second)
	if e.Error != nil {
		t.Fatalf("fourth event error: %+v", e.Error)
	}
	if sr.StatusUpdate == nil || sr.StatusUpdate.Status.State != TaskStateCompleted {
		t.Fatalf("fourth event = %+v, want COMPLETED statusUpdate", sr)
	}
	r.eof(2 * time.Second)

	// Every data event must be a single line: the payload cannot
	// contain a raw newline, or an event could be split by injection.
	r2 := h.openStream("alice", MethodSubscribeToTask, map[string]any{"id": sr.StatusUpdate.TaskID})
	for {
		ev, ok := r2.next(2 * time.Second)
		if !ok {
			break
		}
		if ev.dataLines != 1 && ev.comment == "" {
			t.Fatalf("event data spans %d lines: %q", ev.dataLines, ev.data)
		}
		if ev.data != "" && strings.ContainsAny(ev.data, "\n\r") {
			t.Fatalf("data carries a raw newline: %q", ev.data)
		}
	}
}

// TestStreamClosesOnInputRequired pins that an interrupted task closes
// the stream (the client resumes with a new message).
func TestStreamClosesOnInputRequired(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		return tc.RequireInput(TextPart("need input"))
	})
	r := h.openStream("alice", MethodSendStreamingMessage, streamSendParams(""))
	for {
		e, sr := r.nextResult(2 * time.Second)
		if e.Error != nil {
			t.Fatalf("event error: %+v", e.Error)
		}
		if sr.StatusUpdate != nil && sr.StatusUpdate.Status.State == TaskStateInputRequired {
			break
		}
	}
	r.eof(2 * time.Second)
}

// TestSubscribeMidRun pins attaching to a run already in flight: the
// snapshot arrives first, then the remaining events.
func TestSubscribeMidRun(t *testing.T) {
	h := newHarness(t, nil)
	working := make(chan struct{})
	release := make(chan struct{})
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Working(TextPart("working")); err != nil {
			return err
		}
		close(working)
		<-release
		if err := tc.Artifact(Artifact{ArtifactID: "a1", Parts: []Part{TextPart("late")}}, false); err != nil {
			return err
		}
		return tc.Complete(TextPart("done"))
	})
	task := h.send("alice", map[string]any{"returnImmediately": true})
	select {
	case <-working:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}

	r := h.openStream("alice", MethodSubscribeToTask, map[string]any{"id": task.ID})
	_, sr := r.nextResult(2 * time.Second)
	if sr.Task == nil || sr.Task.Status.State != TaskStateWorking {
		t.Fatalf("snapshot = %+v, want WORKING task", sr)
	}
	close(release)

	sawArtifact, sawCompleted := false, false
	for {
		e, sr := r.nextResult(2 * time.Second)
		if e.Error != nil {
			t.Fatalf("event error: %+v", e.Error)
		}
		if sr.ArtifactUpdate != nil {
			sawArtifact = true
		}
		if sr.StatusUpdate != nil && sr.StatusUpdate.Status.State == TaskStateCompleted {
			sawCompleted = true
		}
		if sawArtifact && sawCompleted {
			break
		}
	}
	r.eof(2 * time.Second)
}

// TestSubscribeCrossReplicaPoll pins the multi-replica fallback: two
// servers over ONE SQL store; the task runs on A while B subscribes and
// sees snapshot events through the store until COMPLETED.
func TestSubscribeCrossReplicaPoll(t *testing.T) {
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
	a := newReplica()
	b := newReplica()

	release := make(chan struct{})
	a.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Working(TextPart("working")); err != nil {
			return err
		}
		<-release
		return nil
	})

	// Create on A, deferred.
	task := a.send("alice", map[string]any{"returnImmediately": true})
	a.waitTask("alice", task.ID, TaskStateWorking, 2*time.Second)

	// Subscribe on B: no run in B's registry, so B polls the store.
	r := b.openStream("alice", MethodSubscribeToTask, map[string]any{"id": task.ID})
	_, sr := r.nextResult(2 * time.Second)
	if sr.Task == nil {
		t.Fatalf("no snapshot on B: %+v", sr)
	}

	close(release)
	for {
		e, sr := r.nextResult(5 * time.Second)
		if e.Error != nil {
			t.Fatalf("event error: %+v", e.Error)
		}
		if sr.Task != nil && sr.Task.Status.State == TaskStateCompleted {
			break
		}
		if sr.Task == nil {
			t.Fatalf("B emitted a non-snapshot event: %+v", sr)
		}
	}
	r.eof(2 * time.Second)
}

// TestSubscribeMissing32001 pins the not-found refusal.
func TestSubscribeMissing32001(t *testing.T) {
	h := newHarness(t, nil)
	_, e, _ := h.call("alice", MethodSubscribeToTask, map[string]any{"id": "nope"})
	if e.Error == nil || e.Error.Code != CodeTaskNotFound {
		t.Fatalf("err = %+v, want -32001", e.Error)
	}
}

// TestBusDropsSlowSubscriber pins the bus contract directly: a
// subscriber that never reads is closed and dropped, and publishing
// after the drop is a no-op.
func TestBusDropsSlowSubscriber(t *testing.T) {
	h := newHarness(t, nil)
	bus := newTaskBus(h.srv.log)
	slow := bus.subscribe()
	fast := bus.subscribe()
	for i := range subscriberBuffer + 8 {
		bus.publish(StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{TaskID: "t", Status: TaskStatus{State: TaskStateWorking}}})
		_ = i
	}
	// slow was dropped: its buffered events drain, then it closes.
	drained := 0
	for range slow {
		drained++
	}
	if drained != subscriberBuffer {
		t.Fatalf("slow drained %d events, want the full buffer before close", drained)
	}
	got := 0
	for range fast {
		got++
	}
	bus.publish(StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{TaskID: "t"}}) // no-op on dropped
	if got != subscriberBuffer {
		t.Fatalf("fast received %d events, want the full buffer", got)
	}
}
