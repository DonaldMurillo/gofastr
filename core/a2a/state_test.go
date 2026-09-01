package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestSendToTerminalTask32004 pins that a message to a terminal task is
// refused with -32004 naming the state.
func TestSendToTerminalTask32004(t *testing.T) {
	h := newHarness(t, nil)
	task := h.send("alice")
	if task.Status.State != TaskStateCompleted {
		t.Fatalf("setup state = %s", task.Status.State)
	}
	_, e, _ := h.call("alice", MethodSendMessage, map[string]any{
		"message": map[string]any{
			"taskId": task.ID,
			"role":   "ROLE_USER",
			"parts":  []any{map[string]any{"text": "again"}},
			"metadata": map[string]any{
				"skill": "echo",
			},
		},
	})
	if e.Error == nil || e.Error.Code != CodeUnsupportedOperation {
		t.Fatalf("err = %+v, want -32004", e.Error)
	}
	if e.Error != nil && e.Error.Message != "task "+task.ID+" is TASK_STATE_COMPLETED" {
		t.Fatalf("message = %q", e.Error.Message)
	}
}

// TestSendToRunningTask32004 pins the refusal while a run is live.
func TestSendToRunningTask32004(t *testing.T) {
	h := newHarness(t, nil)
	gate := make(chan struct{})
	h.setHandler(func(ctx context.Context, tc TaskContext) error {
		<-gate
		return nil
	})
	task := h.send("alice", map[string]any{"returnImmediately": true})
	_, e, _ := h.call("alice", MethodSendMessage, map[string]any{
		"message": map[string]any{
			"taskId": task.ID,
			"role":   "ROLE_USER",
			"parts":  []any{map[string]any{"text": "more"}},
			"metadata": map[string]any{
				"skill": "echo",
			},
		},
	})
	if e.Error == nil || e.Error.Code != CodeUnsupportedOperation {
		t.Fatalf("err = %+v, want -32004", e.Error)
	}
	close(gate)
	h.waitTask("alice", task.ID, TaskStateCompleted, 2*time.Second)
}

// TestCancelTerminalTask32002 pins the not-cancelable refusal.
func TestCancelTerminalTask32002(t *testing.T) {
	h := newHarness(t, nil)
	task := h.send("alice")
	_, e, _ := h.call("alice", MethodCancelTask, map[string]any{"id": task.ID})
	if e.Error == nil || e.Error.Code != CodeTaskNotCancelable {
		t.Fatalf("err = %+v, want -32002", e.Error)
	}
}

// TestCancelRunningTaskCancels pins cancel of a live run: the caller
// gets CANCELED, and the handler's context is done.
func TestCancelRunningTaskCancels(t *testing.T) {
	h := newHarness(t, nil)
	ctxDone := make(chan struct{})
	h.setHandler(func(ctx context.Context, tc TaskContext) error {
		<-ctx.Done()
		close(ctxDone)
		return nil
	})
	task := h.send("alice", map[string]any{"returnImmediately": true})
	h.waitTask("alice", task.ID, TaskStateSubmitted, time.Second)

	_, e, raw := h.call("alice", MethodCancelTask, map[string]any{"id": task.ID})
	if e.Error != nil {
		t.Fatalf("cancel: %+v body=%s", e.Error, raw)
	}
	var resp struct {
		Result SendMessageResponse `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.Result.Task == nil || resp.Result.Task.Status.State != TaskStateCanceled {
		t.Fatalf("cancel result = %s", raw)
	}
	select {
	case <-ctxDone:
	case <-time.After(2 * time.Second):
		t.Fatal("handler context was not canceled")
	}
	h.waitTask("alice", task.ID, TaskStateCanceled, 2*time.Second)
}

// TestHandlerWorkingAfterCancelFails is the cancel guard: after
// CancelTask, the run's writes are refused and the STORED task stays
// CANCELED no matter how long the handler keeps calling.
func TestHandlerWorkingAfterCancelFails(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(ctx context.Context, tc TaskContext) error {
		if err := tc.Working(TextPart("start")); err != nil {
			return err
		}
		<-ctx.Done()
		// Every mutator must now be refused: a handler that ignores
		// cancellation cannot resurrect the task or smuggle writes in.
		var refusals []string
		if err := tc.Working(TextPart("late")); err != nil {
			refusals = append(refusals, "working")
		}
		if err := tc.Artifact(Artifact{ArtifactID: "late", Parts: []Part{TextPart("x")}}, false); err != nil {
			refusals = append(refusals, "artifact")
		}
		if err := tc.Complete(TextPart("late")); err != nil {
			refusals = append(refusals, "complete")
		}
		if len(refusals) != 3 {
			return fmt.Errorf("post-cancel mutators not all refused: %v", refusals)
		}
		return nil
	})
	task := h.send("alice", map[string]any{"returnImmediately": true})
	h.waitTask("alice", task.ID, TaskStateWorking, 2*time.Second)

	if _, e, raw := h.call("alice", MethodCancelTask, map[string]any{"id": task.ID}); e.Error != nil {
		t.Fatalf("cancel: %+v body=%s", e.Error, raw)
	}
	// The run ends without error only if every late call was refused.
	got := h.waitTask("alice", task.ID, TaskStateCanceled, 3*time.Second)
	if got.Status.State != TaskStateCanceled {
		t.Fatalf("stored state = %s, want CANCELED", got.Status.State)
	}
	if len(got.History) != 2 { // user message + "start" agent message, no "late"
		t.Fatalf("history = %d messages, want 2 (late writes must not land)", len(got.History))
	}
}

// TestDoubleCompleteSecondFails pins the terminal guard inside the
// TaskContext: a second terminal transition is an error, and the first
// stands.
func TestDoubleCompleteSecondFails(t *testing.T) {
	h := newHarness(t, nil)
	second := make(chan error, 1)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Complete(TextPart("first")); err != nil {
			return err
		}
		second <- tc.Complete(TextPart("second"))
		return nil
	})
	task := h.send("alice")
	select {
	case err := <-second:
		if err == nil {
			t.Fatal("second Complete must fail")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish")
	}
	got := h.waitTask("alice", task.ID, TaskStateCompleted, time.Second)
	if len(got.History) != 2 { // user + first
		t.Fatalf("history = %d, want 2", len(got.History))
	}
}

// TestResumeFromInputRequired pins the resume path: the handler runs a
// second time with the new message, sees the prior history, and the
// final state is COMPLETED.
func TestResumeFromInputRequired(t *testing.T) {
	h := newHarness(t, nil)
	runs := 0
	historySeen := make(chan int, 2)
	msgSeen := make(chan string, 2)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		runs++
		historySeen <- len(tc.Task().History)
		msgSeen <- *tc.Message().Parts[0].Text
		if runs == 1 {
			return tc.RequireInput(TextPart("need input"))
		}
		return tc.Complete(TextPart("resumed"))
	})
	task := h.send("alice")
	if task.Status.State != TaskStateInputRequired {
		t.Fatalf("first run state = %s, want INPUT_REQUIRED", task.Status.State)
	}
	task2 := h.sendWithTask("alice", task.ID, "the answer")
	if task2.Status.State != TaskStateCompleted {
		t.Fatalf("resume state = %s, want COMPLETED", task2.Status.State)
	}
	if runs != 2 {
		t.Fatalf("handler ran %d times, want 2", runs)
	}
	// FIFO: first value is run 1's, second is run 2's. Run 2 sees the
	// prior history (user + "need input") plus its own message.
	<-historySeen
	if n := <-historySeen; n != 3 {
		t.Fatalf("second run saw %d history messages, want 3", n)
	}
	<-msgSeen // run 1's message ("hi") comes out first
	if m := <-msgSeen; m != "the answer" {
		t.Fatalf("second run message = %q", m)
	}
}

// TestReturnImmediatelySubmits pins the deferred run: the response is
// the SUBMITTED task while the handler is still blocked.
func TestReturnImmediatelySubmits(t *testing.T) {
	h := newHarness(t, nil)
	gate := make(chan struct{})
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		<-gate
		return nil
	})
	task := h.send("alice", map[string]any{"returnImmediately": true})
	if task.Status.State != TaskStateSubmitted {
		t.Fatalf("state = %s, want SUBMITTED before the handler finishes", task.Status.State)
	}
	close(gate)
	h.waitTask("alice", task.ID, TaskStateCompleted, 2*time.Second)
}

// TestTimeoutFails pins the TaskTimeout ceiling.
func TestTimeoutFails(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.TaskTimeout = 30 * time.Millisecond })
	h.setHandler(func(ctx context.Context, tc TaskContext) error {
		<-ctx.Done() // let the deadline hit
		return ctx.Err()
	})
	task := h.send("alice")
	if task.Status.State != TaskStateFailed {
		t.Fatalf("state = %s, want FAILED", task.Status.State)
	}
	if task.Status.Message == nil || task.Status.Message.Parts[0].Text == nil || *task.Status.Message.Parts[0].Text != "task timed out" {
		t.Fatalf("failure message = %+v, want 'task timed out'", task.Status.Message)
	}
}

// TestHandlerErrorIsGeneric pins the no-errleak posture: a non-*Error
// handler failure is FAILED with a generic message, while an *Error
// keeps its own text.
func TestHandlerErrorIsGeneric(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		return errors.New("dsn postgres://u:sekrit@db/internal leaked?")
	})
	task := h.send("alice")
	if msg := statusText(task); msg != "skill handler failed" {
		t.Fatalf("message = %q, want generic", msg)
	}

	h2 := newHarness(t, nil)
	h2.setHandler(func(_ context.Context, tc TaskContext) error {
		return Errorf(CodeInvalidAgentResponse, "chosen message")
	})
	task2 := h2.send("alice")
	if msg := statusText(task2); msg != "chosen message" {
		t.Fatalf("*Error message = %q, want the handler's own", msg)
	}
}

func statusText(task *Task) string {
	if task.Status.Message == nil || len(task.Status.Message.Parts) == 0 || task.Status.Message.Parts[0].Text == nil {
		return ""
	}
	return *task.Status.Message.Parts[0].Text
}

// TestHandlerPanicFails pins panic recovery: FAILED with a generic
// message, run cleaned up.
func TestHandlerPanicFails(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		panic("boom: internal /etc/passrd path")
	})
	task := h.send("alice")
	if task.Status.State != TaskStateFailed {
		t.Fatalf("state = %s, want FAILED", task.Status.State)
	}
	if msg := statusText(task); msg != "skill handler failed" {
		t.Fatalf("message = %q, want generic", msg)
	}
}

// TestRoutingMissRejected pins the router-miss shape: a REJECTED task
// (not a JSON-RPC error) whose status message names the skills.
func TestRoutingMissRejected(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.Skills = append(c.Skills, Skill{ID: "other", Name: "Other", Handler: func(context.Context, TaskContext) error { return nil }})
	})
	status, e, raw := h.call("alice", MethodSendMessage, map[string]any{
		"message": map[string]any{
			"role":  "ROLE_USER",
			"parts": []any{map[string]any{"text": "hi"}},
		},
	})
	if status != 200 || e.Error != nil {
		t.Fatalf("status=%d err=%+v body=%s", status, e.Error, raw)
	}
	var resp struct {
		Result SendMessageResponse `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	task := resp.Result.Task
	if task == nil || task.Status.State != TaskStateRejected {
		t.Fatalf("task = %+v, want REJECTED", task)
	}
	if msg := statusText(task); msg != "no skill named; available: echo, other" {
		t.Fatalf("message = %q", msg)
	}
}

// TestInvalidMessages32002 pins message validation.
func TestInvalidMessages32002(t *testing.T) {
	h := newHarness(t, nil)
	cases := []struct {
		name string
		msg  map[string]any
	}{
		{"agent role", map[string]any{"role": "ROLE_AGENT", "parts": []any{map[string]any{"text": "x"}}}},
		{"no parts", map[string]any{"role": "ROLE_USER", "parts": []any{}}},
		{"empty part", map[string]any{"role": "ROLE_USER", "parts": []any{map[string]any{"filename": "x"}}}},
		{"two-contents part", map[string]any{"role": "ROLE_USER", "parts": []any{map[string]any{"text": "x", "url": "https://x"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, e, _ := h.call("alice", MethodSendMessage, map[string]any{"message": tc.msg})
			if e.Error == nil || e.Error.Code != CodeInvalidParams {
				t.Fatalf("err = %+v, want -32602", e.Error)
			}
		})
	}
	_, e, _ := h.call("alice", MethodSendMessage, map[string]any{})
	if e.Error == nil || e.Error.Code != CodeInvalidParams {
		t.Fatalf("missing message: err = %+v, want -32602", e.Error)
	}
}
