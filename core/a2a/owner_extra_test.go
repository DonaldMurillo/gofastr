package a2a

import (
	"context"
	"encoding/json"
	"testing"
)

// Resuming another owner's paused task must look exactly like resuming a
// task that does not exist: a message with a foreign taskId is -32001,
// never a resume, and the foreign task stays paused for its owner.
func TestResumeOtherOwnerTaskIs32001(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		return tc.RequireInput(TextPart("which one?"))
	})
	task := h.send("alice")
	if task.Status.State != TaskStateInputRequired {
		t.Fatalf("setup: state = %s", task.Status.State)
	}
	_, e, _ := h.call("bob", MethodSendMessage, SendMessageRequest{Message: &Message{
		Role: RoleUser, TaskID: task.ID, Parts: []Part{TextPart("the second")},
	}})
	if e.Error == nil || e.Error.Code != CodeTaskNotFound {
		t.Fatalf("bob resume err = %+v, want -32001", e.Error)
	}
	_, e, _ = h.call("alice", MethodGetTask, map[string]any{"id": task.ID})
	if e.Error != nil {
		t.Fatalf("alice get: %+v", e.Error)
	}
	var got struct {
		Status TaskStatus `json:"status"`
	}
	if err := json.Unmarshal(e.Result, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.State != TaskStateInputRequired {
		t.Fatalf("alice's task moved to %s after bob's attempt", got.Status.State)
	}
}

// SubscribeToTask on a foreign task is -32001 as a plain JSON-RPC answer,
// never an event stream that would leak the task's updates.
func TestSubscribeOtherOwnerIs32001(t *testing.T) {
	h := newHarness(t, nil)
	task := h.send("alice")
	status, e, _ := h.call("bob", MethodSubscribeToTask, map[string]any{"id": task.ID})
	if status != 200 || e.Error == nil || e.Error.Code != CodeTaskNotFound {
		t.Fatalf("status=%d err=%+v, want 200 with -32001", status, e.Error)
	}
}
