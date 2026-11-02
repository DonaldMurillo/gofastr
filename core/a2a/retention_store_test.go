package a2a

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestTrimDropsTrimmedTaskPushConfigs pins that terminal-task retention
// deletes the trimmed tasks' push configs with them: a config for a
// task that no longer exists can never fire again, so leaving it behind
// grew the push table without bound.
func TestTrimDropsTrimmedTaskPushConfigs(t *testing.T) {
	for _, ctor := range storeConstructors(t) {
		t.Run(ctor.name, func(t *testing.T) {
			st := ctor.new(t)
			rt, ok := st.(RetentionTrimmer)
			if !ok {
				t.Fatalf("%s store does not implement RetentionTrimmer", ctor.name)
			}
			ctx := context.Background()
			base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
			// Unique per run: the Postgres tables are shared across runs.
			pfx := fmt.Sprintf("trim%d-", time.Now().UnixNano())
			owner := pfx + "alice"
			ids := []string{pfx + "t1", pfx + "t2", pfx + "t3"}
			for i, id := range ids {
				if err := st.CreateTask(ctx, rec(owner, id, "TASK_STATE_COMPLETED", base.Add(time.Duration(i)*time.Minute))); err != nil {
					t.Fatalf("create %s: %v", id, err)
				}
				if err := st.CreatePushConfig(ctx, &PushConfigRecord{
					Config: PushNotificationConfig{ID: "c-" + id, TaskID: id, URL: "https://example.com/hook"},
					Owner:  owner,
				}); err != nil {
					t.Fatalf("create push config for %s: %v", id, err)
				}
			}
			// Another owner's config naming a trimmed task id is not
			// this owner's to delete.
			other := pfx + "bob"
			if err := st.CreatePushConfig(ctx, &PushConfigRecord{
				Config: PushNotificationConfig{ID: "c-bob", TaskID: ids[0], URL: "https://example.com/hook"},
				Owner:  other,
			}); err != nil {
				t.Fatalf("create other owner's push config: %v", err)
			}

			if err := rt.TrimTerminalTasks(ctx, owner, RetentionTier{}, 1); err != nil {
				t.Fatalf("trim: %v", err)
			}
			for _, id := range ids[:2] {
				if _, err := st.GetTask(ctx, owner, id); err == nil {
					t.Fatalf("task %s survived the trim", id)
				}
				cfgs, err := st.ListPushConfigs(ctx, owner, id)
				if err != nil {
					t.Fatalf("list push configs for %s: %v", id, err)
				}
				if len(cfgs) != 0 {
					t.Errorf("trimmed task %s kept %d push configs, want 0", id, len(cfgs))
				}
			}
			if cfgs, err := st.ListPushConfigs(ctx, owner, ids[2]); err != nil || len(cfgs) != 1 {
				t.Errorf("kept task's push configs = %d (err %v), want 1", len(cfgs), err)
			}
			if cfgs, err := st.ListPushConfigs(ctx, other, ids[0]); err != nil || len(cfgs) != 1 {
				t.Errorf("other owner's push configs = %d (err %v), want 1", len(cfgs), err)
			}
		})
	}
}

// On a timestamp tie every store keeps the task ListTasks shows first.
// The SQL trim broke ties by id ascending while memory and ListTasks
// broke them by id descending, so SQL deleted the task the list called
// newest.
func TestTrimTieKeepsListedNewest(t *testing.T) {
	for _, ctor := range storeConstructors(t) {
		t.Run(ctor.name, func(t *testing.T) {
			st := ctor.new(t)
			ctx := context.Background()
			ts := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
			pfx := fmt.Sprintf("tie%d-", time.Now().UnixNano())
			owner := pfx + "alice"
			for _, id := range []string{pfx + "a", pfx + "b"} {
				r := rec(owner, id, "TASK_STATE_COMPLETED", ts)
				r.CreatedAt = ts
				if err := st.CreateTask(ctx, r); err != nil {
					t.Fatalf("create %s: %v", id, err)
				}
			}
			if err := st.(RetentionTrimmer).TrimTerminalTasks(ctx, owner, RetentionTier{}, 1); err != nil {
				t.Fatalf("trim: %v", err)
			}
			if _, err := st.GetTask(ctx, owner, pfx+"b"); err != nil {
				t.Errorf("trim deleted %sb, the task ListTasks orders first: %v", pfx, err)
			}
			if _, err := st.GetTask(ctx, owner, pfx+"a"); err == nil {
				t.Errorf("trim kept %sa as well", pfx)
			}
		})
	}
}

// An offset with no limit skips that many tasks on every store: the SQL
// store wrote OFFSET only beside a LIMIT and returned the whole list.
func TestListOffsetWithoutLimit(t *testing.T) {
	for _, ctor := range storeConstructors(t) {
		t.Run(ctor.name, func(t *testing.T) {
			st := ctor.new(t)
			ctx := context.Background()
			pfx := fmt.Sprintf("off%d-", time.Now().UnixNano())
			owner := pfx + "alice"
			ts := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
			for i, id := range []string{"t1", "t2", "t3"} {
				if err := st.CreateTask(ctx, rec(owner, pfx+id, "TASK_STATE_COMPLETED", ts.Add(time.Duration(i)*time.Minute))); err != nil {
					t.Fatalf("create %s: %v", id, err)
				}
			}
			recs, _, err := st.ListTasks(ctx, owner, ListQuery{Offset: 1})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(recs) != 2 || recs[0].Task.ID != pfx+"t2" {
				ids := make([]string, len(recs))
				for i, r := range recs {
					ids[i] = r.Task.ID
				}
				t.Errorf("offset 1 listed %v, want t2, t1", ids)
			}
		})
	}
}

// A push config registered while a trim deleted its task outlives the
// task: the handler checked the task, the trim deleted it with its
// configs, and the insert landed after. The next trim that deletes
// anything sweeps such configs, so a racing client cannot grow the push
// table past the retention bound. A send stores its config just before
// its task row, so a config newer than the grace is a send in flight and
// stays.
func TestTrimSweepsOrphanPushConfigs(t *testing.T) {
	for _, ctor := range storeConstructors(t) {
		t.Run(ctor.name, func(t *testing.T) {
			st := ctor.new(t)
			rt := st.(RetentionTrimmer)
			ctx := context.Background()
			base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
			pfx := fmt.Sprintf("orphan%d-", time.Now().UnixNano())
			owner := pfx + "alice"
			// The race's end state: a config naming a task that is gone.
			if err := st.CreatePushConfig(ctx, &PushConfigRecord{
				Config:    PushNotificationConfig{ID: "c-gone", TaskID: pfx + "gone", URL: "https://example.com/hook"},
				Owner:     owner,
				CreatedAt: time.Now().Add(-2 * orphanPushGrace),
			}); err != nil {
				t.Fatalf("create orphan push config: %v", err)
			}
			// A send in flight: its config is stored, its task not yet.
			if err := st.CreatePushConfig(ctx, &PushConfigRecord{
				Config: PushNotificationConfig{ID: "c-new", TaskID: pfx + "sending", URL: "https://example.com/hook"},
				Owner:  owner,
			}); err != nil {
				t.Fatalf("create pending push config: %v", err)
			}
			live := pfx + "live"
			if err := st.CreateTask(ctx, rec(owner, live, "TASK_STATE_WORKING", base)); err != nil {
				t.Fatal(err)
			}
			if err := st.CreatePushConfig(ctx, &PushConfigRecord{
				Config: PushNotificationConfig{ID: "c-live", TaskID: live, URL: "https://example.com/hook"},
				Owner:  owner,
			}); err != nil {
				t.Fatal(err)
			}
			for i, id := range []string{pfx + "t1", pfx + "t2"} {
				if err := st.CreateTask(ctx, rec(owner, id, "TASK_STATE_COMPLETED", base.Add(time.Duration(i)*time.Minute))); err != nil {
					t.Fatal(err)
				}
			}
			if err := rt.TrimTerminalTasks(ctx, owner, RetentionTier{}, 1); err != nil {
				t.Fatalf("trim: %v", err)
			}
			if cfgs, err := st.ListPushConfigs(ctx, owner, pfx+"gone"); err != nil || len(cfgs) != 0 {
				t.Errorf("orphan push configs = %d (err %v), want 0", len(cfgs), err)
			}
			if cfgs, err := st.ListPushConfigs(ctx, owner, live); err != nil || len(cfgs) != 1 {
				t.Errorf("live task's push configs = %d (err %v), want 1", len(cfgs), err)
			}
			if cfgs, err := st.ListPushConfigs(ctx, owner, pfx+"sending"); err != nil || len(cfgs) != 1 {
				t.Errorf("in-flight send's push configs = %d (err %v), want 1", len(cfgs), err)
			}
		})
	}
}

// TestMemoryUpdateNoAlias pins that UpdateTask hands the caller its own
// copy: the record written back after the version bump shared History,
// Artifacts and Metadata with the stored one, so mutating the caller's
// record changed stored state with no version bump.
func TestMemoryUpdateNoAlias(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()
	if err := st.CreateTask(ctx, rec("alice", "t1", "TASK_STATE_WORKING", t0)); err != nil {
		t.Fatalf("create: %v", err)
	}
	r, err := st.GetTask(ctx, "alice", "t1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	r.Task.Artifacts = []Artifact{{ArtifactID: "a1", Parts: []Part{TextPart("one")}}}
	if err := st.UpdateTask(ctx, r); err != nil {
		t.Fatalf("update: %v", err)
	}
	r.Task.History[0].MessageID = "mutated"
	r.Task.Metadata["gofastr.skill"] = "mutated"
	r.Task.Artifacts[0].ArtifactID = "mutated"

	got, err := st.GetTask(ctx, "alice", "t1")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.Task.History[0].MessageID != "m1" {
		t.Errorf("stored history changed through the caller's record: %q", got.Task.History[0].MessageID)
	}
	if got.Task.Metadata["gofastr.skill"] != "echo" {
		t.Errorf("stored metadata changed through the caller's record: %v", got.Task.Metadata["gofastr.skill"])
	}
	if got.Task.Artifacts[0].ArtifactID != "a1" {
		t.Errorf("stored artifacts changed through the caller's record: %q", got.Task.Artifacts[0].ArtifactID)
	}
	if got.Version != r.Version {
		t.Errorf("caller version = %d, stored = %d", r.Version, got.Version)
	}
}
