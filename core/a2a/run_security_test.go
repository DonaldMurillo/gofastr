package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// racingStore simulates a second replica winning the version race: the
// first UpdateTask against a paused task is preceded by a competing
// write, so the caller's version check must fail with ErrConflict.
type racingStore struct{ Store }

func (r *racingStore) UpdateTask(ctx context.Context, rec *TaskRecord) error {
	cur, err := r.Store.GetTask(ctx, rec.Owner, rec.Task.ID)
	if err == nil && cur.Task.Status.State == TaskStateInputRequired && cur.Version == rec.Version {
		cand := cur.Clone()
		cand.Task.Metadata["gofastr.race"] = "won-by-other-replica"
		if err := r.Store.UpdateTask(ctx, cand); err != nil {
			return err
		}
	}
	return r.Store.UpdateTask(ctx, rec)
}

// TestWrappedHandlerErrorKeepsMessage pins that a handler failure keeps
// its chosen text only through the *Error contract, including a wrapped
// *Error: errors.As must see through the wrapping, and nothing but an
// *Error ever carries detail to the client.
func TestWrappedHandlerErrorKeepsMessage(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(_ context.Context, _ TaskContext) error {
		return fmt.Errorf("outer context: %w", Errorf(CodeUnsupportedOperation, "plan quota exceeded"))
	})
	task := h.send("alice")
	if msg := statusText(task); msg != "plan quota exceeded" {
		t.Fatalf("message = %q, want the handler's chosen text through the wrap", msg)
	}
	if task.Status.State != TaskStateFailed {
		t.Fatalf("state = %s, want FAILED", task.Status.State)
	}
}

// TestBusSubscriberLifecycleNoLeak pins the bus's subscriber lifecycle:
// a dropped slow subscriber leaves the subscriber set (no leak), an
// unsubscribed channel is never sent on and never closed, and both
// unsubscribe twice and publish-after-unsubscribe are safe.
func TestBusSubscriberLifecycleNoLeak(t *testing.T) {
	h := newHarness(t, nil)
	bus := newTaskBus(h.srv.log)
	slow := bus.subscribe()
	fast := bus.subscribe()
	ev := StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{TaskID: "t", Status: TaskStatus{State: TaskStateWorking}}}

	// Publish with a synchronous drain of fast, so only slow fills.
	got := 0
	for range subscriberBuffer + 16 {
		bus.publish(ev)
	drain:
		for {
			select {
			case <-fast:
				got++
			default:
				break drain
			}
		}
	}
	bus.mu.Lock()
	subs := len(bus.subs)
	bus.mu.Unlock()
	if subs != 1 {
		t.Fatalf("subscriber set = %d after the slow drop, want 1 (no leak)", subs)
	}
	if got != subscriberBuffer+16 {
		t.Fatalf("fast subscriber got %d events, want all %d", got, subscriberBuffer+16)
	}

	// The dropped subscriber holds exactly a full buffer and is closed.
	drainedSlow := 0
slowDrain:
	for {
		select {
		case _, ok := <-slow:
			if !ok {
				break slowDrain
			}
			drainedSlow++
		default:
			break slowDrain
		}
	}
	if drainedSlow != subscriberBuffer {
		t.Fatalf("dropped subscriber left %d buffered events, want the full %d", drainedSlow, subscriberBuffer)
	}

	// Unsubscribe: the channel leaves the set and is then untouchable.
	bus.unsubscribe(fast)
	bus.mu.Lock()
	subs = len(bus.subs)
	bus.mu.Unlock()
	if subs != 0 {
		t.Fatalf("subscriber set = %d after unsubscribe, want 0", subs)
	}
	bus.publish(ev) // must neither send on nor close the gone channel
	select {
	case _, ok := <-fast:
		if ok {
			t.Fatal("unsubscribed channel received an event")
		} else {
			t.Fatal("unsubscribed channel was closed")
		}
	default:
	}
	bus.unsubscribe(fast) // idempotent; must not panic
}

// TestConcurrentResumeExactlyOneWinner pins the resume race: two
// concurrent messages to one interrupted task produce exactly one run
// (the task completes with the winner's message appended) and one
// -32004 refusal; the loser's message never enters the task.
func TestConcurrentResumeExactlyOneWinner(t *testing.T) {
	h := newHarness(t, nil)
	var calls atomic.Int32
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if calls.Add(1) == 1 {
			return tc.RequireInput(TextPart("need input"))
		}
		return tc.Complete(TextPart("resumed"))
	})
	task := h.send("alice")
	if task.Status.State != TaskStateInputRequired {
		t.Fatalf("setup state = %s, want INPUT_REQUIRED", task.Status.State)
	}

	start := make(chan struct{})
	type outcome struct {
		code  int
		state TaskState
	}
	res := make(chan outcome, 2)
	for range 2 {
		go func() {
			<-start
			params := map[string]any{
				"message": map[string]any{
					"taskId": task.ID, "role": "ROLE_USER",
					"parts": []any{map[string]any{"text": "go"}},
				},
			}
			_, e, _ := h.callID("alice", MethodSendMessage, params, "race")
			if e.Error != nil {
				res <- outcome{code: e.Error.Code}
				return
			}
			var out SendMessageResponse
			_ = json.Unmarshal(e.Result, &out)
			res <- outcome{code: 0, state: out.Task.Status.State}
		}()
	}
	close(start)
	wins, refusals := 0, 0
	for range 2 {
		o := <-res
		switch {
		case o.code == 0 && o.state == TaskStateCompleted:
			wins++
		case o.code == CodeUnsupportedOperation:
			refusals++
		default:
			t.Fatalf("unexpected outcome: code=%d state=%s", o.code, o.state)
		}
	}
	if wins != 1 || refusals != 1 {
		t.Fatalf("wins=%d refusals=%d, want exactly one of each", wins, refusals)
	}

	final := h.waitTask("alice", task.ID, TaskStateCompleted, 3*time.Second)
	goMsgs := 0
	for _, m := range final.History {
		if len(m.Parts) == 1 && m.Parts[0].Text != nil && *m.Parts[0].Text == "go" {
			goMsgs++
		}
	}
	if goMsgs != 1 {
		t.Fatalf("resume message appears %d times in history, want exactly the winner's 1", goMsgs)
	}
}

// TestResumeMismatchedPushTaskID32602 pins that a resume carrying a
// push config addressed to a different task is refused before anything
// is written, and the paused task is untouched.
func TestResumeMismatchedPushTaskID32602(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Push.AllowPrivate = true })
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		return tc.RequireInput(TextPart("need input"))
	})
	task := h.send("alice")
	_, e, _ := h.call("alice", MethodSendMessage, map[string]any{
		"message": map[string]any{
			"taskId": task.ID, "role": "ROLE_USER",
			"parts": []any{map[string]any{"text": "hi"}},
		},
		"configuration": map[string]any{
			"taskPushNotificationConfig": map[string]any{
				"taskId": "some-other-task", "url": "https://127.0.0.1:9/hook",
			},
		},
	})
	if e.Error == nil || e.Error.Code != CodeInvalidParams || !strings.Contains(e.Error.Message, "does not match") {
		t.Fatalf("err = %+v, want -32602 mismatch refusal", e.Error)
	}
	stored, err := h.srv.store.GetTask(context.Background(), "alice", task.ID)
	if err != nil || stored.Task.Status.State != TaskStateInputRequired || len(stored.Task.History) != 2 {
		t.Fatalf("paused task disturbed: %s history=%d (%v)", stored.Task.Status.State, len(stored.Task.History), err)
	}
}

// TestResumeLostVersionRace32004 pins the resume-vs-writer edge: when a
// competing write bumps the stored version between the resume's read
// and its WORKING transition, the resume is refused with -32004, the
// client's message never lands, and the task stays paused.
func TestResumeLostVersionRace32004(t *testing.T) {
	inner := NewMemoryStore()
	h := newHarness(t, func(c *Config) { c.Store = &racingStore{Store: inner} })
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		return tc.RequireInput(TextPart("need input"))
	})
	task := h.send("alice")

	_, e, _ := h.call("alice", MethodSendMessage, map[string]any{
		"message": map[string]any{
			"taskId": task.ID, "role": "ROLE_USER",
			"parts": []any{map[string]any{"text": "too late"}},
		},
	})
	if e.Error == nil || e.Error.Code != CodeUnsupportedOperation || !strings.Contains(e.Error.Message, "changed concurrently") {
		t.Fatalf("err = %+v, want -32004 changed-concurrently refusal", e.Error)
	}
	stored, err := inner.GetTask(context.Background(), "alice", task.ID)
	if err != nil || stored.Task.Status.State != TaskStateInputRequired {
		t.Fatalf("task state after lost race = %s (%v), want still INPUT_REQUIRED", stored.Task.Status.State, err)
	}
	for _, m := range stored.Task.History {
		for _, p := range m.Parts {
			if p.Text != nil && *p.Text == "too late" {
				t.Fatal("refused resume message was appended to the task")
			}
		}
	}
}

// TestNewSendPushConfigRebindsTaskID pins the id-confusion guard on the
// new-task path: a push config sent with a new message is always
// attached to the freshly minted task, never to whatever taskId the
// client typed — the documented rebind in prepareNewTask.
func TestNewSendPushConfigRebindsTaskID(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Push.AllowPrivate = true })
	foreign := h.send("alice") // an existing task to try to hijack
	_, e, _ := h.call("alice", MethodSendMessage, map[string]any{
		"message": map[string]any{
			"role":     "ROLE_USER",
			"parts":    []any{map[string]any{"text": "hi"}},
			"metadata": map[string]any{"skill": "echo"},
		},
		"configuration": map[string]any{
			"taskPushNotificationConfig": map[string]any{
				"taskId": foreign.ID, "url": "https://127.0.0.1:9/hook",
			},
		},
	})
	if e.Error != nil {
		t.Fatalf("send with push config: %+v", e.Error)
	}
	var out SendMessageResponse
	if err := json.Unmarshal(e.Result, &out); err != nil || out.Task == nil {
		t.Fatalf("parse result %s: %v", e.Result, err)
	}
	if out.Task.ID == foreign.ID {
		t.Fatal("push-config send reused the foreign task id instead of minting a task")
	}
	cfgs, err := h.srv.store.ListPushConfigs(context.Background(), "alice", out.Task.ID)
	if err != nil || len(cfgs) != 1 || cfgs[0].Config.TaskID != out.Task.ID {
		t.Fatalf("push config not bound to the minted task: %+v (%v)", cfgs, err)
	}
	// The foreign task carries no config from this send.
	foreignCfgs, _ := h.srv.store.ListPushConfigs(context.Background(), "alice", foreign.ID)
	if len(foreignCfgs) != 0 {
		t.Fatalf("foreign task gained configs: %+v", foreignCfgs)
	}
}

// TestFailedPushConfigLeavesNoOrphan pins the no-half-built-task rule
// beyond validation: when the push-config insert fails after the task
// row was created, the refused send must not leave an unreachable
// SUBMITTED task behind. (validateInbound documents the no-half-built
// posture; this exercises the post-creation failure path.)
func TestFailedPushConfigLeavesNoOrphan(t *testing.T) {
	inner := NewMemoryStore()
	h := newHarness(t, func(c *Config) {
		c.Store = &bombStore{Store: inner, op: "CreatePushConfig"}
		c.Push.AllowPrivate = true
	})
	_, e, _ := h.call("alice", MethodSendMessage, map[string]any{
		"message": map[string]any{
			"role":     "ROLE_USER",
			"parts":    []any{map[string]any{"text": "hi"}},
			"metadata": map[string]any{"skill": "echo"},
		},
		"configuration": map[string]any{
			"taskPushNotificationConfig": map[string]any{
				"url": "https://127.0.0.1:9/hook", "id": "p1",
			},
		},
	})
	if e.Error == nil || e.Error.Code != CodeInternalError {
		t.Fatalf("err = %+v, want -32603 from the failed push insert", e.Error)
	}
	recs, _, err := inner.ListTasks(context.Background(), "alice", ListQuery{})
	if err != nil {
		t.Fatalf("list after refused send: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("refused send left %d orphaned task(s): %s in state %s has no run and will never settle",
			len(recs), recs[0].Task.ID, recs[0].Task.Status.State)
	}
}

// armingStore arms a GetTask panic only after the caller says so, so a
// subscribe that read the task cleanly still panics on the next poll
// read — inside pollEvents' ticker loop, the goroutine no per-request
// recover net reaches.
type armingStore struct {
	Store
	armed atomic.Bool
}

func (a *armingStore) GetTask(ctx context.Context, owner, id string) (*TaskRecord, error) {
	if a.armed.Load() {
		panic("super-secret-store-detail")
	}
	return a.Store.GetTask(ctx, owner, id)
}

// Property: a panicking Store read in pollEvents — the multi-replica
// subscribe fallback's ticker loop, host-supplied code on a goroutine
// with no per-request net — ends that one stream with an attributed
// log instead of unwinding the process. The snapshot read on the
// request path must still succeed first, so the panic lands exactly in
// the loop pollGetTask guards.
func TestPollStorePanicEndsStreamCleanly(t *testing.T) {
	inner := NewMemoryStore()
	st := &armingStore{Store: inner}
	if err := st.CreateTask(context.Background(), rec("alice", "t1", "TASK_STATE_WORKING", t0)); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	var logBuf bytes.Buffer
	h := newHarness(t, func(c *Config) {
		c.Store = st
		c.Logger = slog.New(slog.NewTextHandler(&logBuf, nil))
	})

	r := h.openStream("alice", MethodSubscribeToTask, map[string]any{"id": "t1"})
	_, sr := r.nextResult(2 * time.Second) // snapshot: request-path GetTask done
	if sr.Task == nil {
		t.Fatalf("snapshot event carried no task")
	}

	// The next poll read panics; the stream must close (recovered,
	// logged, ended) rather than hang or take the process down.
	st.armed.Store(true)
	r.eof(3 * time.Second)
	if logged := logBuf.String(); !strings.Contains(logged, "poll read panicked") {
		t.Errorf("SECURITY: [panic-isolation] the recovered poll panic was not attributed in the server log " +
			"(want a \"poll read panicked\" line): a swallowed panic is indistinguishable from a healthy stream end")
	}

	// The server survives and keeps serving: the same subscribe on a
	// disarmed store works again.
	st.armed.Store(false)
	r2 := h.openStream("alice", MethodSubscribeToTask, map[string]any{"id": "t1"})
	_, sr2 := r2.nextResult(2 * time.Second)
	if sr2.Task == nil || sr2.Task.ID != "t1" {
		t.Fatalf("post-panic subscribe = %+v, want task t1 back", sr2.Task)
	}
}
