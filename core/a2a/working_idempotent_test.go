package a2a

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
)

type countingTaskStore struct {
	Store
	updates atomic.Int64
}

func (store *countingTaskStore) UpdateTask(ctx context.Context, record *TaskRecord) error {
	store.updates.Add(1)
	return store.Store.UpdateTask(ctx, record)
}

func TestWorkingSkipsRepeatedWrites(t *testing.T) {
	for _, ctor := range storeConstructors(t) {
		t.Run(ctor.name, func(t *testing.T) {
			store := &countingTaskStore{Store: ctor.new(t)}
			h := newHarness(t, func(cfg *Config) { cfg.Store = store })
			h.setHandler(func(ctx context.Context, task TaskContext) error {
				if err := task.Working(DataPart(map[string]any{"count": int(7), "fraction": 2.5})); err != nil {
					return err
				}
				before, err := store.GetTask(ctx, task.Owner(), task.Task().ID)
				if err != nil {
					return err
				}
				writes := store.updates.Load()
				for range 4 {
					if err := task.Working(TextPart("heartbeat")); err != nil {
						return err
					}
				}
				after, err := store.GetTask(ctx, task.Owner(), task.Task().ID)
				if err != nil {
					return err
				}
				if store.updates.Load() != writes || !reflect.DeepEqual(before, after) {
					t.Errorf("repeated Working wrote %d times; versions %d -> %d", store.updates.Load()-writes, before.Version, after.Version)
				}
				return task.Complete(TextPart("done"))
			})
			if task := h.send("alice"); task.Status.State != TaskStateCompleted {
				t.Fatalf("task state = %s, want completed", task.Status.State)
			}
		})
	}
}

func TestSilentTaskChangesPersist(t *testing.T) {
	h := newHarness(t, nil)
	record := rec("alice", "task", string(TaskStateWorking), t0)
	if err := h.srv.store.CreateTask(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	runner := newTaskRun(h.srv, record, nil, nil, "alice")
	if err := runner.mutate(func(task *Task) (*StreamResponse, error) {
		task.Metadata["progress"] = "changed"
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := h.srv.store.GetTask(context.Background(), "alice", "task")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version != 1 || stored.Task.Metadata["progress"] != "changed" {
		t.Fatalf("silent mutation was lost: version=%d metadata=%v", stored.Version, stored.Task.Metadata)
	}
}
