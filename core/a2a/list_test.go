package a2a

import (
	"context"
	"encoding/json"
	"testing"
)

// TestListTasksPagingAndFilters pins the HTTP surface of ListTasks:
// filters, clamped pageSize, opaque tokens, invalid token refusal, and
// artifact stripping.
func TestListTasksPagingAndFilters(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		return tc.Artifact(Artifact{ArtifactID: "a", Parts: []Part{TextPart("x")}}, false)
	})
	// Three tasks; the handler's artifact rides each.
	ids := []string{}
	for range 3 {
		task := h.send("alice")
		ids = append(ids, task.ID)
	}
	states := map[string]bool{}
	_, e, _ := h.call("alice", MethodListTasks, map[string]any{"pageSize": 2})
	if e.Error != nil {
		t.Fatalf("list: %+v", e.Error)
	}
	var page1 ListTasksResponse
	if err := json.Unmarshal(e.Result, &page1); err != nil {
		t.Fatalf("parse: %v (%s)", err, e.Result)
	}
	if page1.TotalSize != 3 || len(page1.Tasks) != 2 || page1.PageSize != 2 {
		t.Fatalf("page1 = %+v", page1)
	}
	if page1.NextPageToken == "" {
		t.Fatal("page1 must carry a next token")
	}
	for _, tk := range page1.Tasks {
		states[tk.ID] = true
		if len(tk.Artifacts) != 0 {
			t.Fatalf("artifacts must be stripped: %+v", tk.Artifacts)
		}
	}

	_, e, _ = h.call("alice", MethodListTasks, map[string]any{"pageSize": 2, "pageToken": page1.NextPageToken})
	if e.Error != nil {
		t.Fatalf("page2: %+v", e.Error)
	}
	var page2 ListTasksResponse
	_ = json.Unmarshal(e.Result, &page2)
	if len(page2.Tasks) != 1 {
		t.Fatalf("page2 = %+v", page2)
	}
	states[page2.Tasks[0].ID] = true
	if len(states) != 3 {
		t.Fatalf("pages covered %d tasks, want 3", len(states))
	}
	if page2.NextPageToken != "" {
		t.Fatalf("last page must have no token: %q", page2.NextPageToken)
	}

	// includeArtifacts keeps them; invalid token refused; pageSize
	// clamped; unknown status refused.
	include := true
	_, e, _ = h.call("alice", MethodListTasks, map[string]any{"includeArtifacts": include})
	if e.Error != nil {
		t.Fatalf("include: %+v", e.Error)
	}
	var with ListTasksResponse
	_ = json.Unmarshal(e.Result, &with)
	if len(with.Tasks) != 3 || len(with.Tasks[0].Artifacts) != 1 {
		t.Fatalf("includeArtifacts = %+v", with.Tasks[0])
	}

	_, e, _ = h.call("alice", MethodListTasks, map[string]any{"pageToken": "!!!not-base64"})
	if e.Error == nil || e.Error.Code != CodeInvalidParams {
		t.Fatalf("bad token err = %+v, want -32602", e.Error)
	}
	huge := 100000
	_, e, _ = h.call("alice", MethodListTasks, map[string]any{"pageSize": huge})
	if e.Error != nil {
		t.Fatalf("clamped list: %+v", e.Error)
	}
	var clamped ListTasksResponse
	_ = json.Unmarshal(e.Result, &clamped)
	if clamped.PageSize != 200 {
		t.Fatalf("pageSize = %d, want clamp to 200", clamped.PageSize)
	}
	_, e, _ = h.call("alice", MethodListTasks, map[string]any{"status": "TASK_STATE_BOGUS"})
	if e.Error == nil || e.Error.Code != CodeInvalidParams {
		t.Fatalf("bogus status err = %+v, want -32602", e.Error)
	}
}

// TestMaxHistoryTrimsOldest pins the history cap: oldest dropped, the
// newest message always survives.
func TestMaxHistoryTrimsOldest(t *testing.T) {
	task := &Task{}
	keep := 3
	for i := range 6 {
		m := &Message{MessageID: string(rune('a' + i))}
		appendHistory(task, m, keep)
	}
	if len(task.History) != keep {
		t.Fatalf("len = %d, want %d", len(task.History), keep)
	}
	if task.History[keep-1].MessageID != "f" || task.History[0].MessageID != "d" {
		t.Fatalf("kept %+v, want d..f (newest survives)", task.History)
	}
	// nil message is a no-op
	appendHistory(task, nil, keep)
	if len(task.History) != keep {
		t.Fatalf("nil append changed history")
	}
}
