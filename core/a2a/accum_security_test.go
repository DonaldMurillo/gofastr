package a2a

import (
	"encoding/json"
	"testing"
)

// Property: rows a cheap authenticated write mints (tasks, push
// configs) are bounded — by terminal-row retention per owner and a
// per-task push-config cap — so one low-privilege principal cannot grow
// the store without end.
//
// Surfaces: server.go Config.TerminalTaskRetention (0 = 64; the trim
// runs after every persist that leaves a task terminal — the
// 2026-09-05 round-4 fix) and Config.MaxPushConfigsPerTask (0 = 8;
// trimmed after every create), driven through the optional
// RetentionTrimmer Store interface both built-in stores implement.
// Found red in the same pass: one owner created 200 settled tasks and
// 100 push configs on one task through the public JSON-RPC surface;
// nothing refused, trimmed, or reaped either table, and the default
// store is in-RAM with every row carrying the caller's full message
// body (up to the 1 MiB cap).
//
// TestTaskRowsBoundedForOneOwner drives the cheap writes one
// authenticated principal can make and asserts both bounds hold.
func TestTaskRowsBoundedForOneOwner(t *testing.T) {
	h := newHarness(t, nil) // echo skill: every task settles COMPLETED instantly

	const tasks = 200
	configTaskID := ""
	for i := range tasks {
		task := h.send("mallory")
		if task.Status.State != TaskStateCompleted {
			t.Fatalf("task %d settled %q, want COMPLETED", i, task.Status.State)
		}
		// The push configs below must attach to a task that SURVIVES
		// retention: the bound keeps each owner's NEWEST terminal rows,
		// so the last task is the one still seated after the flood.
		configTaskID = task.ID
	}

	// Push configs: one cheap JSON-RPC call each, all on one surviving
	// task. Past the cap the OLDEST configs are deleted, so every
	// create still succeeds.
	const configs = 100
	for i := range configs {
		status, e, raw := h.call("mallory", MethodCreateTaskPushNotificationConfig, map[string]any{
			"taskId": configTaskID,
			"url":    "https://example.com/hook",
		})
		if status != 200 || e.Error != nil {
			t.Fatalf("CreatePushConfig %d: status=%d err=%+v body=%s", i, status, e.Error, raw)
		}
	}

	// What the store actually retains for this one owner.
	_, e, raw := h.call("mallory", MethodListTasks, map[string]any{"pageSize": 1})
	if e.Error != nil {
		t.Fatalf("ListTasks: %v (body=%s)", e.Error, raw)
	}
	var list ListTasksResponse
	if err := json.Unmarshal(e.Result, &list); err != nil {
		t.Fatalf("ListTasks result %s: %v", e.Result, err)
	}
	_, e, raw = h.call("mallory", MethodListTaskPushNotificationConfigs, map[string]any{"taskId": configTaskID})
	if e.Error != nil {
		t.Fatalf("ListPushConfigs: %v (body=%s)", e.Error, raw)
	}
	var cfgs struct {
		Configs []PushNotificationConfig `json:"configs"`
	}
	if err := json.Unmarshal(e.Result, &cfgs); err != nil {
		t.Fatalf("ListPushConfigs result %s: %v", e.Result, err)
	}

	if list.TotalSize > defaultTerminalTaskRetention {
		t.Errorf("SECURITY: [a2a-rows] one low-privilege owner retains %d terminal task rows after %d cheap writes (bound %d): the default store is in-RAM and every row carries a caller body up to the 1 MiB limit — terminal retention must bound it", list.TotalSize, tasks, defaultTerminalTaskRetention)
	}
	if len(cfgs.Configs) > defaultMaxPushConfigsPerTask {
		t.Errorf("SECURITY: [a2a-rows] one task carries %d push-notification configs after %d cheap writes (bound %d): every event fans a delivery goroutine out per config — the per-task cap must bound it", len(cfgs.Configs), configs, defaultMaxPushConfigsPerTask)
	}
}
