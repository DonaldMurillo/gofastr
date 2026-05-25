package builtins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// TestTaskListReplaceAndSnapshot covers the core contract: each Run
// REPLACES the stored list, and TaskListSnapshot returns the latest.
func TestTaskListReplaceAndSnapshot(t *testing.T) {
	sess := ids.NewSessionID()
	defer ResetTasks(sess)
	ctx := tool.WithSession(context.Background(), sess)

	tl := TaskList{}
	mkCall := func(items ...TaskItem) tool.ToolCall {
		raw, _ := json.Marshal(map[string]any{"tasks": items})
		return tool.ToolCall{Name: "TaskList", Input: raw}
	}

	// First write: two pending tasks.
	if _, err := tl.Run(ctx, mkCall(
		TaskItem{Content: "read file", Status: "pending"},
		TaskItem{Content: "fix bug", Status: "pending"},
	), nil); err != nil {
		t.Fatal(err)
	}
	got, _ := TaskListSnapshot(sess)
	if len(got) != 2 {
		t.Fatalf("after first write: %d tasks, want 2", len(got))
	}

	// Second write: shorter list with one in_progress — must REPLACE,
	// not append.
	if _, err := tl.Run(ctx, mkCall(
		TaskItem{Content: "read file", Status: "completed"},
		TaskItem{Content: "fix bug", Status: "in_progress", ActiveForm: "Fixing bug"},
	), nil); err != nil {
		t.Fatal(err)
	}
	got, _ = TaskListSnapshot(sess)
	if len(got) != 2 {
		t.Fatalf("after second write: %d tasks, want 2", len(got))
	}
	if got[0].Status != "completed" || got[1].Status != "in_progress" {
		t.Errorf("replace didn't take: %+v", got)
	}

	// Third write: empty list (all done) — must clear.
	if _, err := tl.Run(ctx, mkCall(), nil); err != nil {
		t.Fatal(err)
	}
	got, _ = TaskListSnapshot(sess)
	if len(got) != 0 {
		t.Errorf("after empty write: %d tasks, want 0", len(got))
	}
}

func TestTaskListIsolatesBySession(t *testing.T) {
	a := ids.NewSessionID()
	b := ids.NewSessionID()
	defer ResetTasks(a)
	defer ResetTasks(b)

	tl := TaskList{}
	raw, _ := json.Marshal(map[string]any{"tasks": []TaskItem{
		{Content: "session-a-task", Status: "pending"},
	}})
	if _, err := tl.Run(tool.WithSession(context.Background(), a),
		tool.ToolCall{Input: raw}, nil); err != nil {
		t.Fatal(err)
	}
	gotA, _ := TaskListSnapshot(a)
	gotB, _ := TaskListSnapshot(b)
	if len(gotA) != 1 || gotA[0].Content != "session-a-task" {
		t.Errorf("session a missing its task: %+v", gotA)
	}
	if len(gotB) != 0 {
		t.Errorf("session b should be empty: %+v", gotB)
	}
}

func TestTaskListRejectsBadStatus(t *testing.T) {
	sess := ids.NewSessionID()
	defer ResetTasks(sess)
	ctx := tool.WithSession(context.Background(), sess)
	raw, _ := json.Marshal(map[string]any{"tasks": []TaskItem{
		{Content: "thing", Status: "halfway"},
	}})
	res, err := TaskList{}.Run(ctx, tool.ToolCall{Input: raw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Errorf("invalid status should produce an error result: %+v", res)
	}
}

func TestTaskListNoSessionInCtxReturnsError(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"tasks": []TaskItem{
		{Content: "x", Status: "pending"},
	}})
	res, err := TaskList{}.Run(context.Background(), tool.ToolCall{Input: raw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].Text, "no session") {
		t.Errorf("expected no-session error, got %+v", res)
	}
}
