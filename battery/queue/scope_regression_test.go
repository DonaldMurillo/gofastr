package queue

import (
	"container/heap"
	"context"
	"encoding/json"
	"testing"
)

func TestMemoryQueueOwnsPayload(t *testing.T) {
	ctx := context.Background()
	q := NewMemoryQueue(0)
	defer q.Close()
	p := json.RawMessage(`{"ok":true}`)
	q.Enqueue(ctx, Job{ID: "1", Payload: p})
	p[2] = 'X'
	j, _ := q.Dequeue(ctx)
	if string(j.Payload) != `{"ok":true}` {
		t.Fatal("enqueue aliases caller")
	}
	j.Payload[2] = 'Y'
	q.Nack(ctx, j)
	j, _ = q.Dequeue(ctx)
	if string(j.Payload) != `{"ok":true}` {
		t.Fatal("dequeue aliases retry state")
	}
}
func TestHeapPopReleasesPayload(t *testing.T) {
	h := &pendingHeap{}
	heap.Push(h, pendingJob{job: Job{Payload: json.RawMessage(`{}`)}})
	heap.Pop(h)
	if h.items[:cap(h.items)][0].job.Payload != nil {
		t.Fatal("popped payload retained")
	}
}
func TestAllStatusIncludesDeadJobs(t *testing.T) {
	q := NewMemoryQueue(0)
	defer q.Close()
	q.retainDead(Job{ID: "1"})
	got, e := q.ListJobs(context.Background(), "all", 10, 0)
	if e != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, e)
	}
}
