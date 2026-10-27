package a2a

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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

// A subscribe on the replica whose run asked for input, after another
// replica resumed the task, follows the task to its end: it attached to
// the stale local run, and when that run's handler returned the stream
// closed on WORKING with the resumed run's completion unseen.
func TestSubscribeOutlivesStaleLocalRun(t *testing.T) {
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
	asked, aReturn, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	a.setHandler(func(_ context.Context, tc TaskContext) error {
		err := tc.RequireInput(TextPart("need input"))
		close(asked)
		<-aReturn // still unwinding when the task moves on
		return err
	})
	b.setHandler(func(_ context.Context, tc TaskContext) error {
		<-finish
		return tc.Complete(TextPart("done"))
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
	close(aReturn)
	for deadline := time.Now().Add(2 * time.Second); a.srv.runFor(task.ID) != nil; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("A's run never ended")
		}
	}
	close(finish)
	for {
		e, sr := r.nextResult(5 * time.Second)
		if e.Error != nil {
			t.Fatalf("event error: %+v", e.Error)
		}
		if sr.Task != nil && sr.Task.Status.State == TaskStateCompleted {
			return
		}
		if sr.StatusUpdate != nil && sr.StatusUpdate.Status.State == TaskStateCompleted {
			return
		}
	}
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

// A CancelTask served by replica B reaches a task running on replica A:
// A's handler context is cut and A's own subscribers see CANCELED. B
// only wrote the store, so A's handler ran on until its next write or
// TaskTimeout, and A's streams ended without the terminal state.
func TestCancelCrossReplica(t *testing.T) {
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

	cut := make(chan struct{})
	a.setHandler(func(ctx context.Context, tc TaskContext) error {
		if err := tc.Working(TextPart("working")); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			close(cut)
		case <-time.After(10 * time.Second):
		}
		return nil
	})
	task := a.send("alice", map[string]any{"returnImmediately": true})
	a.waitTask("alice", task.ID, TaskStateWorking, 2*time.Second)
	r := a.openStream("alice", MethodSubscribeToTask, map[string]any{"id": task.ID})
	if _, sr := r.nextResult(2 * time.Second); sr.Task == nil {
		t.Fatalf("no snapshot on A: %+v", sr)
	}

	if _, e, _ := b.call("alice", MethodCancelTask, map[string]any{"id": task.ID}); e.Error != nil {
		t.Fatalf("cancel on B: %+v", e.Error)
	}
	select {
	case <-cut:
	case <-time.After(2 * time.Second):
		t.Fatal("A's handler context was not cut by a cancel served on B")
	}
	for {
		e, sr := r.nextResult(2 * time.Second)
		if e.Error != nil {
			t.Fatalf("event error: %+v", e.Error)
		}
		if sr.StatusUpdate != nil && sr.StatusUpdate.Status.State == TaskStateCanceled {
			break
		}
		if sr.StatusUpdate != nil && sr.StatusUpdate.Status.State.Terminal() {
			t.Fatalf("A's subscriber saw %s, want CANCELED", sr.StatusUpdate.Status.State)
		}
	}
	r.eof(2 * time.Second)
}

// blindStore fails every GetTask while blind is set, standing in for a
// replica whose watch reads keep missing a short-lived terminal row.
type blindStore struct {
	Store
	blind atomic.Bool
}

func (s *blindStore) GetTask(ctx context.Context, owner, id string) (*TaskRecord, error) {
	if s.blind.Load() {
		return nil, errors.New("store unavailable")
	}
	return s.Store.GetTask(ctx, owner, id)
}

// A task canceled on replica B and trimmed by retention before A's watch
// saw the CANCELED row still halts A's run: retention deletes only
// terminal rows, so a missing row means another replica settled it. A's
// handler ran on to TaskTimeout, and its final write must not bring the
// trimmed row back.
func TestCancelTrimmedCrossReplica(t *testing.T) {
	db := openSQLite(t)
	store, err := NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	aStore := &blindStore{Store: store}
	newReplica := func(st Store) *harness {
		var h *harness
		h = newHarness(t, func(c *Config) {
			c.Store = st
			c.TerminalTaskRetention = 1
			c.Skills = []Skill{{
				ID: "echo", Name: "Echo", Description: "echo",
				Handler: func(ctx context.Context, tc TaskContext) error { return h.current()(ctx, tc) },
			}}
		})
		h.srv.pollEvery = 20 * time.Millisecond
		return h
	}
	a := newReplica(aStore)
	b := newReplica(store)
	var bIDs idGen
	b.srv.newID = func() string { return "b-" + bIDs.next() }

	cut := make(chan struct{})
	a.setHandler(func(ctx context.Context, tc TaskContext) error {
		if err := tc.Working(TextPart("working")); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			close(cut)
		case <-time.After(10 * time.Second):
		}
		return tc.Complete(TextPart("late"))
	})
	task := a.send("alice", map[string]any{"returnImmediately": true})
	b.waitTask("alice", task.ID, TaskStateWorking, 2*time.Second)

	aStore.blind.Store(true)
	if _, e, _ := b.call("alice", MethodCancelTask, map[string]any{"id": task.ID}); e.Error != nil {
		t.Fatalf("cancel on B: %+v", e.Error)
	}
	b.send("alice") // completes, and retention keeps only this one
	if _, e, _ := b.call("alice", MethodGetTask, map[string]any{"id": task.ID}); e.Error == nil || e.Error.Code != CodeTaskNotFound {
		t.Fatalf("canceled task not trimmed: %+v", e.Error)
	}
	aStore.blind.Store(false)

	select {
	case <-cut:
	case <-time.After(2 * time.Second):
		t.Fatal("A's handler context was not cut after its task was trimmed")
	}
	for deadline := time.Now().Add(2 * time.Second); a.srv.runFor(task.ID) != nil; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("A's run never ended")
		}
	}
	if _, e, _ := b.call("alice", MethodGetTask, map[string]any{"id": task.ID}); e.Error == nil || e.Error.Code != CodeTaskNotFound {
		t.Fatalf("trimmed task came back after A's run ended: %+v", e.Error)
	}
}

// A handler that completed its own task and is still unwinding when
// retention trims that task keeps its context: the missing row is this
// run's own settle, not another replica's.
func TestOwnSettleTrimKeepsContext(t *testing.T) {
	db := openSQLite(t)
	store, err := NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	var h *harness
	h = newHarness(t, func(c *Config) {
		c.Store = store
		c.TerminalTaskRetention = 1
		c.Skills = []Skill{{
			ID: "echo", Name: "Echo", Description: "echo",
			Handler: func(ctx context.Context, tc TaskContext) error { return h.current()(ctx, tc) },
		}}
	})
	h.srv.pollEvery = 5 * time.Millisecond
	completed, trimmed, verdict := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	h.setHandler(func(ctx context.Context, tc TaskContext) error {
		if err := tc.Complete(TextPart("done")); err != nil {
			return err
		}
		close(completed)
		<-trimmed
		select {
		case <-ctx.Done():
			verdict <- errors.New("handler context cut after its own settle was trimmed")
		case <-time.After(200 * time.Millisecond):
			verdict <- nil
		}
		return nil
	})
	task := h.send("alice", map[string]any{"returnImmediately": true})
	<-completed
	h.setHandler(echoHandler)
	h.send("alice")
	if _, e, _ := h.call("alice", MethodGetTask, map[string]any{"id": task.ID}); e.Error == nil || e.Error.Code != CodeTaskNotFound {
		t.Fatalf("completed task not trimmed: %+v", e.Error)
	}
	close(trimmed)
	if err := <-verdict; err != nil {
		t.Fatal(err)
	}
}

// A cancel served on replica B after A's handler returned, before A's
// finalize read the store, still reaches A's subscribers: finalize adopts
// the stored CANCELED and announces it, since no halt on A did.
func TestCancelCrossReplicaAtFinalize(t *testing.T) {
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
		return h
	}
	a := newReplica()
	b := newReplica()
	a.srv.pollEvery = time.Hour // only finalize can notice the remote cancel

	subscribed := make(chan struct{})
	a.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Working(TextPart("working")); err != nil {
			return err
		}
		<-subscribed
		if _, e, _ := b.call("alice", MethodCancelTask, map[string]any{"id": tc.Task().ID}); e.Error != nil {
			return fmt.Errorf("cancel on B: %+v", e.Error)
		}
		return nil
	})
	task := a.send("alice", map[string]any{"returnImmediately": true})
	a.waitTask("alice", task.ID, TaskStateWorking, 2*time.Second)
	r := a.openStream("alice", MethodSubscribeToTask, map[string]any{"id": task.ID})
	if _, sr := r.nextResult(2 * time.Second); sr.Task == nil {
		t.Fatalf("no snapshot on A: %+v", sr)
	}
	close(subscribed)
	for {
		e, sr := r.nextResult(2 * time.Second)
		if e.Error != nil {
			t.Fatalf("event error: %+v", e.Error)
		}
		if sr.StatusUpdate != nil && sr.StatusUpdate.Status.State == TaskStateCanceled {
			break
		}
		if sr.StatusUpdate != nil && sr.StatusUpdate.Status.State.Terminal() {
			t.Fatalf("A's subscriber saw %s, want CANCELED", sr.StatusUpdate.Status.State)
		}
	}
	r.eof(2 * time.Second)
}

// cancelRaceStore runs before once, ahead of the first CANCELED write it
// forwards, so another replica's write lands between that write's read
// and its update.
type cancelRaceStore struct {
	Store
	once   sync.Once
	before func()
}

func (s *cancelRaceStore) UpdateTask(ctx context.Context, rec *TaskRecord) error {
	if rec.Task.Status.State == TaskStateCanceled {
		s.once.Do(s.before)
	}
	return s.Store.UpdateTask(ctx, rec)
}

// Two replicas cancel the same task at once and B's write wins. A had
// already halted its run, so neither its watch nor its finalize
// announces; A's cancel must, or A's subscribers see the stream end
// with no CANCELED.
func TestCancelBothReplicasRace(t *testing.T) {
	db := openSQLite(t)
	store, err := NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	var b *harness
	raced := &cancelRaceStore{Store: store}
	newReplica := func(st Store) *harness {
		var h *harness
		h = newHarness(t, func(c *Config) {
			c.Store = st
			c.Skills = []Skill{{
				ID: "echo", Name: "Echo", Description: "echo",
				Handler: func(ctx context.Context, tc TaskContext) error { return h.current()(ctx, tc) },
			}}
		})
		h.srv.pollEvery = time.Hour // only the cancels can announce
		return h
	}
	a := newReplica(raced)
	b = newReplica(store)

	a.setHandler(func(ctx context.Context, tc TaskContext) error {
		if err := tc.Working(TextPart("working")); err != nil {
			return err
		}
		<-ctx.Done()
		return nil
	})
	task := a.send("alice", map[string]any{"returnImmediately": true})
	a.waitTask("alice", task.ID, TaskStateWorking, 2*time.Second)
	r := a.openStream("alice", MethodSubscribeToTask, map[string]any{"id": task.ID})
	if _, sr := r.nextResult(2 * time.Second); sr.Task == nil {
		t.Fatalf("no snapshot on A: %+v", sr)
	}

	raced.before = func() {
		if _, e, _ := b.call("alice", MethodCancelTask, map[string]any{"id": task.ID}); e.Error != nil {
			t.Errorf("cancel on B: %+v", e.Error)
		}
	}
	if _, e, _ := a.call("alice", MethodCancelTask, map[string]any{"id": task.ID}); e.Error != nil {
		t.Fatalf("cancel on A: %+v", e.Error)
	}
	for {
		e, sr := r.nextResult(2 * time.Second)
		if e.Error != nil {
			t.Fatalf("event error: %+v", e.Error)
		}
		if sr.StatusUpdate != nil && sr.StatusUpdate.Status.State == TaskStateCanceled {
			break
		}
		if sr.StatusUpdate != nil && sr.StatusUpdate.Status.State.Terminal() {
			t.Fatalf("A's subscriber saw %s, want CANCELED", sr.StatusUpdate.Status.State)
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
		bus.publish(busEvent{ev: StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{TaskID: "t", Status: TaskStatus{State: TaskStateWorking}}}})
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
	bus.publish(busEvent{ev: StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{TaskID: "t"}}}) // no-op on dropped
	if got != subscriberBuffer {
		t.Fatalf("fast received %d events, want the full buffer", got)
	}
}
