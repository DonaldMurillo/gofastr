package a2a

import (
	"context"
	"testing"
	"time"
)

// TestGetTaskOtherOwnerIs32001 pins owner scoping on GetTask: bob
// asking for alice's task gets the same -32001 as for a missing task,
// so ids do not enumerate across owners.
func TestGetTaskOtherOwnerIs32001(t *testing.T) {
	h := newHarness(t, nil)
	task := h.send("alice")
	_, e, _ := h.call("bob", MethodGetTask, map[string]any{"id": task.ID})
	if e.Error == nil || e.Error.Code != CodeTaskNotFound {
		t.Fatalf("err = %+v, want -32001", e.Error)
	}
}

// TestListTasksOtherOwnerEmpty pins owner scoping on ListTasks.
func TestListTasksOtherOwnerEmpty(t *testing.T) {
	h := newHarness(t, nil)
	h.send("alice")
	status, e, raw := h.call("bob", MethodListTasks, struct{}{})
	if status != 200 || e.Error != nil {
		t.Fatalf("status=%d err=%+v body=%s", status, e.Error, raw)
	}
	assertJSON(t, "bob list", raw, map[string]any{
		"jsonrpc": "2.0", "id": "call-1",
		"result": ListTasksResponse{Tasks: []Task{}, PageSize: 50, TotalSize: 0},
	})
}

// TestCancelTaskOtherOwner32001 pins owner scoping on CancelTask.
func TestCancelTaskOtherOwner32001(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(ctx context.Context, tc TaskContext) error {
		<-ctx.Done()
		return nil
	})
	task := h.send("alice", map[string]any{"returnImmediately": true})
	_, e, _ := h.call("bob", MethodCancelTask, map[string]any{"id": task.ID})
	if e.Error == nil || e.Error.Code != CodeTaskNotFound {
		t.Fatalf("err = %+v, want -32001", e.Error)
	}
	// and the task is untouched for its owner
	got := h.waitTask("alice", task.ID, TaskStateSubmitted, time.Second)
	if got.Status.State != TaskStateSubmitted {
		t.Fatalf("alice task state = %s", got.Status.State)
	}
}

// TestPushConfigOtherOwner32001 pins owner scoping through the push
// config methods: an unknown-or-foreign task is -32001, not a config
// error.
func TestPushConfigOtherOwner32001(t *testing.T) {
	h := newHarness(t, nil)
	task := h.send("alice")
	for _, m := range []string{
		MethodCreateTaskPushNotificationConfig,
		MethodGetTaskPushNotificationConfig,
		MethodListTaskPushNotificationConfigs,
		MethodDeleteTaskPushNotificationConfig,
	} {
		params := map[string]any{"taskId": task.ID, "url": "https://hooks.example.test/cb"}
		_, e, _ := h.call("bob", m, params)
		if e.Error == nil || e.Error.Code != CodeTaskNotFound {
			t.Fatalf("%s: err = %+v, want -32001", m, e.Error)
		}
	}
}
