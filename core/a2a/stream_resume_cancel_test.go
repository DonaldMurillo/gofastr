package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"
)

// gatedUpdateStore holds the first UpdateTask made after arm AFTER its
// write lands, so the caller sits between its persist and whatever it
// does next.
type gatedUpdateStore struct {
	Store
	mu      sync.Mutex
	armed   bool
	blocked chan struct{}
	release chan struct{}
}

func (s *gatedUpdateStore) arm() {
	s.mu.Lock()
	s.armed = true
	s.mu.Unlock()
}

func (s *gatedUpdateStore) UpdateTask(ctx context.Context, rec *TaskRecord) error {
	err := s.Store.UpdateTask(ctx, rec)
	s.mu.Lock()
	hold := s.armed
	s.armed = false
	s.mu.Unlock()
	if hold {
		close(s.blocked)
		<-s.release
	}
	return err
}

// A streaming resume whose task is canceled between the run's claim and
// the stream's bus subscription still ends on CANCELED. CancelTask
// published CANCELED to a bus nobody had joined yet, the halted run
// adopted the stored state without announcing it, and the stream closed
// after the resumed WORKING snapshot with no terminal event.
func TestStreamResumeCanceledBeforeSubscribe(t *testing.T) {
	store := &gatedUpdateStore{Store: NewMemoryStore(), blocked: make(chan struct{}), release: make(chan struct{})}
	h := newHarness(t, func(c *Config) { c.Store = store })
	h.srv.pollEvery = 20 * time.Millisecond
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		return tc.RequireInput(TextPart("need input"))
	})
	task := h.send("alice")
	if task.Status.State != TaskStateInputRequired {
		t.Fatalf("first send state = %s, want INPUT_REQUIRED", task.Status.State)
	}
	for deadline := time.Now().Add(3 * time.Second); h.srv.runFor(task.ID) != nil; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the first run never released its registration")
		}
	}
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		return tc.Complete(TextPart("done"))
	})

	store.arm() // the resume's WORKING write
	req := streamRequest(t, context.Background(), h.ts.URL, MethodSendStreamingMessage, streamSendParams(task.ID))
	type opened struct {
		resp *http.Response
		err  error
	}
	open := make(chan opened, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		open <- opened{resp, err}
	}()
	select {
	case <-store.blocked:
	case <-time.After(3 * time.Second):
		t.Fatal("the resume never wrote WORKING")
	}
	if _, e, raw := h.call("alice", MethodCancelTask, map[string]any{"id": task.ID}); e.Error != nil {
		t.Fatalf("CancelTask: %s", raw)
	}
	close(store.release)

	o := <-open
	if o.err != nil {
		t.Fatalf("open stream: %v", o.err)
	}
	t.Cleanup(func() { _ = o.resp.Body.Close() })
	r := &sseReader{t: t, resp: o.resp, ch: make(chan sseEvent, 64), rawLines: make(chan string, 1024)}
	go r.pump()

	var states []TaskState
	for {
		ev, ok := r.next(3 * time.Second)
		if !ok {
			break
		}
		if ev.comment != "" {
			continue
		}
		var e env
		var sr StreamResponse
		if json.Unmarshal([]byte(ev.data), &e) != nil || json.Unmarshal(e.Result, &sr) != nil {
			t.Fatalf("bad event %q", ev.data)
		}
		switch {
		case sr.Task != nil:
			states = append(states, sr.Task.Status.State)
		case sr.StatusUpdate != nil:
			states = append(states, sr.StatusUpdate.Status.State)
		}
	}
	if len(states) == 0 || states[len(states)-1] != TaskStateCanceled {
		t.Fatalf("stream delivered states %v, want it to end on CANCELED", states)
	}
}
