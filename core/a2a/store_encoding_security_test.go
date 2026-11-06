package a2a

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestMemoryRefusesUnencodableTasks(t *testing.T) {
	for name, value := range map[string]any{"nan": math.NaN(), "infinity": math.Inf(1), "channel": make(chan int), "function": func() {}} {
		t.Run(name, func(t *testing.T) {
			for _, location := range []string{"metadata", "history", "status", "artifact"} {
				t.Run(location, func(t *testing.T) {
					store := NewMemoryStore()
					ctx := context.Background()
					record := rec("alice", "task", string(TaskStateWorking), time.Now())
					if err := store.CreateTask(ctx, record); err != nil {
						t.Fatal(err)
					}
					switch location {
					case "metadata":
						record.Task.Metadata["bad"] = value
					case "history":
						record.Task.History[0].Parts = []Part{DataPart(value)}
					case "status":
						record.Task.Status.Message = &Message{Role: RoleAgent, Parts: []Part{DataPart(value)}}
					case "artifact":
						record.Task.Artifacts = []Artifact{{ArtifactID: "bad", Parts: []Part{DataPart(value)}}}
					}
					if err := store.UpdateTask(ctx, record); err == nil {
						t.Error("UpdateTask accepted an unencodable task")
					}
					record.Task.ID = "new"
					if err := store.CreateTask(ctx, record); err == nil {
						t.Error("CreateTask accepted an unencodable task")
					}
					stored, total, err := store.ListTasks(ctx, "alice", ListQuery{})
					if err != nil || total != 1 || len(stored) != 1 || stored[0].Version != 0 {
						t.Fatalf("failed writes changed store: total=%d records=%v err=%v", total, stored, err)
					}
					if _, bad := stored[0].Task.Metadata["bad"]; bad {
						t.Fatal("failed update mutated stored metadata")
					}
				})
			}
		})
	}
}
