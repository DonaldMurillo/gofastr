package a2a

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStoreUpdateKeepsScope(t *testing.T) {
	for _, ctor := range storeConstructors(t) {
		t.Run(ctor.name, func(t *testing.T) {
			for _, scopes := range [][2]string{{"", "read"}, {"read", ""}, {"read", "write"}} {
				t.Run(scopes[0]+"_to_"+scopes[1], func(t *testing.T) {
					store := ctor.new(t)
					ctx := context.Background()
					record := rec("alice", "task", string(TaskStateCompleted), time.Now())
					record.Scope = scopes[0]
					if err := store.CreateTask(ctx, record); err != nil {
						t.Fatal(err)
					}
					record.Scope = scopes[1]
					record.Task.Metadata["updated"] = true
					if err := store.UpdateTask(ctx, record); err != nil {
						t.Fatal(err)
					}
					stored, err := store.GetTask(ctx, "alice", "task")
					if err != nil {
						t.Fatal(err)
					}
					if stored.Scope != scopes[0] || stored.Task.Metadata["updated"] != true || stored.Version != 1 {
						t.Errorf("updated record = %+v, want immutable scope %q and updated task", stored, scopes[0])
					}
					for index, scope := range scopes {
						listed, total, err := store.ListTasks(ctx, "alice", ListQuery{Scoped: true, Scope: scope})
						want := 1 - index
						if err != nil || len(listed) != want || total != want {
							t.Errorf("scope %q: list=%d total=%d err=%v, want %d", scope, len(listed), total, err, want)
						}
					}
					if err := store.(RetentionTrimmer).TrimTerminalTasks(ctx, "alice", RetentionTier{Scope: scopes[0]}, 0); err != nil {
						t.Fatal(err)
					}
					if _, err := store.GetTask(ctx, "alice", "task"); !errors.Is(err, ErrNotFound) {
						t.Errorf("original tier did not trim its task: %v", err)
					}
				})
			}
		})
	}
}
