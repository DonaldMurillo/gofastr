package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// gatedPushStore blocks the first ListPushConfigs call made after arm,
// holding that publication between its persist and its push enqueue.
type gatedPushStore struct {
	Store
	mu      sync.Mutex
	armed   bool
	blocked chan struct{}
	release chan struct{}
}

func (s *gatedPushStore) arm() {
	s.mu.Lock()
	s.armed = true
	s.mu.Unlock()
}

func (s *gatedPushStore) ListPushConfigs(ctx context.Context, owner, taskID string) ([]*PushConfigRecord, error) {
	s.mu.Lock()
	hold := s.armed
	s.armed = false
	s.mu.Unlock()
	if hold {
		close(s.blocked)
		<-s.release
	}
	return s.Store.ListPushConfigs(ctx, owner, taskID)
}

// The per-config version marks are not a leak: once a config's queue
// drains, its mark is forgotten after the retention window, the longest
// a stale publication can still be on its way.
func TestPushMarksForgottenWhenIdle(t *testing.T) {
	prev := storeOpTimeout
	storeOpTimeout = 5 * time.Millisecond
	t.Cleanup(func() { storeOpTimeout = prev })
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	p := newPusher(PushOptions{Client: client, AllowPrivate: true}, slog.New(slog.DiscardHandler))
	send := func(task string) {
		recs := []*PushConfigRecord{{Owner: "alice", Config: PushNotificationConfig{ID: "c1", TaskID: task, URL: "http://receiver.test/hook"}}}
		p.deliver(recs, StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{TaskID: task}}, 1)
	}
	for i := range 8 {
		send(fmt.Sprintf("t%d", i))
	}
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(time.Millisecond) {
		p.mu.Lock()
		busy := len(p.queues)
		p.mu.Unlock()
		if busy == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("deliveries never drained")
		}
	}
	time.Sleep(4 * storeOpTimeout)
	send("fresh")
	p.mu.Lock()
	marks := len(p.marks)
	p.mu.Unlock()
	if marks != 1 {
		t.Fatalf("pusher holds %d marks after the idle ones expired, want 1", marks)
	}
}

// The sweep spares a config whose delivery is still in flight: its mark
// is what refuses an older event queued behind that delivery.
func TestPushMarkKeptWhileInFlight(t *testing.T) {
	prev := storeOpTimeout
	storeOpTimeout = 5 * time.Millisecond
	t.Cleanup(func() { storeOpTimeout = prev })
	tr := &blockingTransport{release: make(chan struct{})}
	t.Cleanup(func() { close(tr.release) })
	p := newPusher(PushOptions{Client: &http.Client{Transport: tr}, AllowPrivate: true}, slog.New(slog.DiscardHandler))
	send := func(task string, version int64) int {
		cfg := PushNotificationConfig{ID: "c1", TaskID: task, URL: "http://receiver.test/hook"}
		return p.enqueue(pushQueueKey{"alice", task, "c1"}, pushItem{cfg: cfg, payload: []byte("{}"), version: version})
	}
	send("t1", 2)
	for deadline := time.Now().Add(3 * time.Second); tr.inFlight.Load() == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("first delivery never started")
		}
	}
	time.Sleep(4 * storeOpTimeout)
	send("other", 1) // runs the sweep
	if got := send("t1", 1); got != pushStale {
		t.Fatalf("older event behind an in-flight delivery: enqueue = %d, want pushStale (%d)", got, pushStale)
	}
}

// A config whose only event was dropped because every delivery slot was
// taken has nothing in flight, so its mark expires like an idle one.
func TestPushMarkOfDroppedConfigExpires(t *testing.T) {
	prev := storeOpTimeout
	storeOpTimeout = 5 * time.Millisecond
	t.Cleanup(func() { storeOpTimeout = prev })
	tr := &blockingTransport{release: make(chan struct{})}
	t.Cleanup(func() { close(tr.release) })
	p := newPusher(PushOptions{Client: &http.Client{Transport: tr}, AllowPrivate: true}, slog.New(slog.DiscardHandler))
	send := func(task string) int {
		cfg := PushNotificationConfig{ID: "c1", TaskID: task, URL: "http://receiver.test/hook"}
		return p.enqueue(pushQueueKey{"alice", task, "c1"}, pushItem{cfg: cfg, payload: []byte("{}"), version: 1})
	}
	for i := range pushConcurrency {
		send(fmt.Sprintf("busy%d", i))
	}
	if got := send("dropped"); got != pushFull {
		t.Fatalf("enqueue with every slot taken = %d, want pushFull (%d)", got, pushFull)
	}
	time.Sleep(4 * storeOpTimeout)
	send("later") // runs the sweep
	p.mu.Lock()
	_, kept := p.marks[pushQueueKey{"alice", "dropped", "c1"}]
	p.mu.Unlock()
	if kept {
		t.Fatal("the dropped config's mark outlived the retention window")
	}
}

// Two publications race: the run's WORKING is persisted, then held
// listing its push configs while CancelTask persists CANCELED and
// delivers it. WORKING was enqueued after CANCELED and the per-config
// serialization kept that order, so the receiver's last word on a
// canceled task was WORKING. Delivery now carries the persisted version
// and drops an event older than one already enqueued for the config.
func TestPushNeverGoesBackwards(t *testing.T) {
	rec := newPushRecorder(t, nil)
	store := &gatedPushStore{Store: NewMemoryStore(), blocked: make(chan struct{}), release: make(chan struct{})}
	h := newHarness(t, func(c *Config) {
		c.Push.AllowPrivate = true
		c.Store = store
	})
	taskID := make(chan string, 1)
	h.setHandler(func(ctx context.Context, tc TaskContext) error {
		taskID <- tc.Task().ID
		store.arm()
		if err := tc.Working(TextPart("working")); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	})
	params := pushConfigParams(rec.srv.URL+"/hook", "tok-1")
	params["configuration"].(map[string]any)["returnImmediately"] = true
	if _, e, raw := h.call("alice", MethodSendMessage, params); e.Error != nil {
		t.Fatalf("send: %s", raw)
	}
	id := <-taskID
	select {
	case <-store.blocked:
	case <-time.After(3 * time.Second):
		t.Fatal("WORKING never reached ListPushConfigs")
	}
	before := rec.count()
	if _, e, raw := h.call("alice", MethodCancelTask, map[string]any{"id": id}); e.Error != nil {
		t.Fatalf("CancelTask: %s", raw)
	}
	rec.wait(t, before+1)
	close(store.release)
	time.Sleep(300 * time.Millisecond)

	rec.mu.Lock()
	reqs := append([]pushRecord(nil), rec.reqs...)
	rec.mu.Unlock()
	var states []TaskState
	for _, r := range reqs {
		var sr StreamResponse
		if err := json.Unmarshal(r.body, &sr); err != nil {
			t.Fatalf("push body %s: %v", r.body, err)
		}
		if sr.StatusUpdate != nil {
			states = append(states, sr.StatusUpdate.Status.State)
		}
	}
	if len(states) == 0 || states[len(states)-1] != TaskStateCanceled {
		t.Fatalf("receiver saw states %v, want CANCELED last", states)
	}
}
