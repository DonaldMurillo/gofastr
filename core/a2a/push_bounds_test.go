package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingTransport holds every request until release closes.
type blockingTransport struct {
	inFlight atomic.Int64
	release  chan struct{}
}

func (b *blockingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	b.inFlight.Add(1)
	defer b.inFlight.Add(-1)
	select {
	case <-b.release:
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}
	return nil, fmt.Errorf("released")
}

// lockedBuffer is a goroutine-safe log sink.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// TestPushBacklogBounded pins that a slow receiver cannot pile up
// delivery goroutines: past the in-flight cap a delivery is dropped
// with a Warn line instead of parking a goroutine (and its payload) on
// the semaphore. Delivery is one best-effort attempt, so a drop is the
// documented outcome, not a lost guarantee. Each event goes to its own
// owner's task config, since one config's events queue behind each
// other (TestPushQueueBounded) and one owner holds only its share of
// the slots (TestPushSlotsPerOwner).
func TestPushBacklogBounded(t *testing.T) {
	tr := &blockingTransport{release: make(chan struct{})}
	logs := &lockedBuffer{}
	log := slog.New(slog.NewTextHandler(logs, nil))
	p := newPusher(PushOptions{Client: &http.Client{Transport: tr}, AllowPrivate: true}, log)

	const events = 4 * pushConcurrency
	before := runtime.NumGoroutine()
	for i := range events {
		id := fmt.Sprintf("t%d", i)
		recs := []*PushConfigRecord{{Owner: "o" + id, Config: PushNotificationConfig{ID: "c1", TaskID: id, URL: "http://receiver.test/hook"}}}
		p.deliver(recs, StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{TaskID: id}}, 1)
	}
	deadline := time.Now().Add(3 * time.Second)
	for tr.inFlight.Load() < pushConcurrency && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // let any parked goroutine settle
	grown := runtime.NumGoroutine() - before
	close(tr.release)

	if grown > pushConcurrency+4 {
		t.Errorf("%d deliveries to a blocked receiver grew %d goroutines, want at most %d (the in-flight cap)", events, grown, pushConcurrency+4)
	}
	if got, want := strings.Count(logs.String(), "push notification dropped"), events-pushConcurrency; got != want {
		t.Errorf("logged %d drops, want %d", got, want)
	}
}

// TestPushPlainTransportDialGuarded pins that a caller-supplied plain
// *http.Transport (no proxy, no custom dialer) gets the dial-time guard,
// so the address the guard checks is the address dialed: the
// resolve-then-RoundTrip wrapper resolved once to check and let the
// inner transport resolve again to dial, which a rebinding name passes.
func TestPushPlainTransportDialGuarded(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer ts.Close()

	inner := &http.Transport{}
	c := guardedClient(&http.Client{Transport: inner}, false)
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("plain transport wrapped as %T, want a guarded *http.Transport", c.Transport)
	}
	if tr == inner || inner.DialContext != nil {
		t.Fatal("the caller's transport was modified instead of cloned")
	}
	if tr.DialContext == nil {
		t.Fatal("guarded clone has no dialer")
	}
	conn, err := tr.DialContext(context.Background(), "tcp", ts.Listener.Addr().String())
	if err == nil {
		_ = conn.Close()
		t.Fatal("guarded dialer connected to a loopback address")
	}

	// A transport with a proxy or a custom dialer keeps the documented
	// resolve-and-check wrapper: swapping its dialer would break an
	// egress proxy or unix-socket transport.
	proxied := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "proxy.test:3128"})}
	if _, ok := guardedClient(&http.Client{Transport: proxied}, false).Transport.(*guardedRoundTripper); !ok {
		t.Error("proxied transport lost the resolve-and-check wrapper")
	}
	var d net.Dialer
	dialer := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return d.DialContext(ctx, network, addr)
	}}
	if _, ok := guardedClient(&http.Client{Transport: dialer}, false).Transport.(*guardedRoundTripper); !ok {
		t.Error("custom-dialer transport lost the resolve-and-check wrapper")
	}

	// http.DefaultTransport.Clone() carries the environment proxy and a
	// stock net.Dialer; it used to take the weaker wrapper (2026-10-27
	// P54) and is dial-guarded now.
	clone := http.DefaultTransport.(*http.Transport).Clone()
	if _, ok := guardedClient(&http.Client{Transport: clone}, false).Transport.(*http.Transport); !ok {
		t.Error("DefaultTransport clone took the resolve-and-check wrapper, want the dial guard")
	}
}

// orderTransport answers every POST, holding artifact B until C's POST
// arrives (or a short wait passes), and records the artifact ids in the
// order the POSTs finish.
type orderTransport struct {
	mu       sync.Mutex
	cArrived chan struct{}
	done     []string
}

func (o *orderTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var ev StreamResponse
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		return nil, err
	}
	switch ev.ArtifactUpdate.Artifact.ArtifactID {
	case "B":
		select {
		case <-o.cArrived:
		case <-time.After(300 * time.Millisecond):
		}
	case "C":
		close(o.cArrived)
	}
	o.mu.Lock()
	o.done = append(o.done, ev.ArtifactUpdate.Artifact.ArtifactID)
	o.mu.Unlock()
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: r}, nil
}

// A slow first delivery to a config does not let the next event for
// that config overtake it: appended artifact chunks carry no sequence,
// so a receiver that got C before B rebuilds the wrong artifact.
func TestPushOrderPerConfig(t *testing.T) {
	tr := &orderTransport{cArrived: make(chan struct{})}
	p := newPusher(PushOptions{Client: &http.Client{Transport: tr}, AllowPrivate: true}, slog.New(slog.DiscardHandler))
	recs := []*PushConfigRecord{{Owner: "alice", Config: PushNotificationConfig{ID: "c1", TaskID: "t1", URL: "http://receiver.test/hook"}}}
	for v, id := range []string{"B", "C"} {
		p.deliver(recs, StreamResponse{ArtifactUpdate: &TaskArtifactUpdateEvent{TaskID: "t1", Artifact: Artifact{ArtifactID: id}, Append: true}}, int64(v))
	}
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(time.Millisecond) {
		tr.mu.Lock()
		got := strings.Join(tr.done, ",")
		tr.mu.Unlock()
		if len(got) == 3 {
			if got != "B,C" {
				t.Fatalf("receiver got %s, want B,C", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("receiver got %q, want B,C", got)
		}
	}
}

// Events queued behind a config's slow delivery are bounded: past the
// backlog they are dropped with a Warn line, so one hung receiver
// cannot hold every payload a fast task mints. Each of five owners
// floods one config: four fill the process-wide backlog with their
// shares, and the fifth queues nothing behind its in-flight delivery.
func TestPushQueueBounded(t *testing.T) {
	tr := &blockingTransport{release: make(chan struct{})}
	logs := &lockedBuffer{}
	p := newPusher(PushOptions{Client: &http.Client{Transport: tr}, AllowPrivate: true}, slog.New(slog.NewTextHandler(logs, nil)))
	const owners, events = 5, 4 * pushOwnerBacklog
	for o := range owners {
		recs := []*PushConfigRecord{{Owner: fmt.Sprintf("o%d", o), Config: PushNotificationConfig{ID: "c1", TaskID: "t1", URL: "http://receiver.test/hook"}}}
		for v := range events {
			p.deliver(recs, StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{TaskID: "t1"}}, int64(v))
		}
	}
	if got, want := strings.Count(logs.String(), "push notification dropped"), owners*(events-1)-pushBacklog; got != want {
		t.Errorf("logged %d drops, want %d (one in flight per owner, %d queued)", got, want, pushBacklog)
	}
	// Once the receiver answers, the queue drains and frees its share
	// of the backlog for later events.
	close(tr.release)
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(time.Millisecond) {
		p.mu.Lock()
		idle, queued := len(p.queues) == 0, p.queued
		owners := len(p.ownerSlots) + len(p.ownerQueued)
		p.mu.Unlock()
		if idle {
			if queued != 0 || owners != 0 {
				t.Fatalf("drained pusher still counts %d queued events and %d owner counters", queued, owners)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the config's queue never drained")
		}
	}
}

// One owner holds at most its share of the delivery slots and of the
// backlog, so a caller whose receiver hangs cannot take every slot and
// leave other owners' notifications dropped (#614).
func TestPushSlotsPerOwner(t *testing.T) {
	tr := &blockingTransport{release: make(chan struct{})}
	defer close(tr.release)
	logs := &lockedBuffer{}
	p := newPusher(PushOptions{Client: &http.Client{Transport: tr}, AllowPrivate: true}, slog.New(slog.NewTextHandler(logs, nil)))
	send := func(owner, task string, v int64) {
		recs := []*PushConfigRecord{{Owner: owner, Config: PushNotificationConfig{ID: "c1", TaskID: task, URL: "http://receiver.test/hook"}}}
		p.deliver(recs, StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{TaskID: task}}, v)
	}
	// mallory: one event to each of many tasks, then many to one task.
	for i := range pushConcurrency {
		send("mallory", fmt.Sprintf("m%d", i), 1)
	}
	for v := range 2 * pushBacklog {
		send("mallory", "m0", int64(v+2))
	}
	p.mu.Lock()
	slots, queued := len(p.queues), p.queued
	p.mu.Unlock()
	if slots != pushOwnerSlots {
		t.Errorf("one owner holds %d delivery slots, want its share %d", slots, pushOwnerSlots)
	}
	if queued != pushOwnerBacklog {
		t.Errorf("one owner queued %d events, want its share %d", queued, pushOwnerBacklog)
	}
	before := strings.Count(logs.String(), "push notification dropped")
	send("alice", "a1", 1)
	send("alice", "a1", 2)
	if got := strings.Count(logs.String(), "push notification dropped"); got != before {
		t.Errorf("alice's events were dropped behind mallory's: %d new drops", got-before)
	}
}
