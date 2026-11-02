package a2a

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Property: host-supplied code that runs on one of the server's own
// goroutines (no per-request recover net) cannot take the process down.
// pollGetTask already guarded the subscribe fallback's store reads
// (TestPollStorePanicEndsStreamCleanly). Two siblings were bare:
//
//   - the run goroutine's finalize: the handler returned and finalize
//     read and wrote the task through the Store (and published, and
//     trimmed through the RetentionTrimmer) outside invoke's recover;
//   - the push drain goroutine: each delivery went through the
//     PushOptions.Client the host supplied.
//
// A panic in either crashed the whole process, every other owner's runs
// and streams with it.

// finalizePanicStore panics on UpdateTask once armed: the handler below
// makes no TaskContext call, so the first UpdateTask is finalize's.
type finalizePanicStore struct {
	Store
	armed atomic.Bool
}

func (s *finalizePanicStore) UpdateTask(ctx context.Context, rec *TaskRecord) error {
	if s.armed.Load() {
		panic("store-detail-from-finalize")
	}
	return s.Store.UpdateTask(ctx, rec)
}

func TestFinalizeStorePanicContained(t *testing.T) {
	st := &finalizePanicStore{Store: NewMemoryStore()}
	st.armed.Store(true)
	var logMu sync.Mutex
	var logBuf bytes.Buffer
	h := newHarness(t, func(c *Config) {
		c.Store = st
		c.Logger = slog.New(slog.NewTextHandler(&lockedWriter{mu: &logMu, w: &logBuf}, nil))
	})
	h.setHandler(func(context.Context, TaskContext) error { return nil })

	// The SendMessage waits for the run; finalize panics on the run's
	// goroutine. Unguarded, the process dies here.
	h.send("alice")

	// The run released its registration and the server keeps serving.
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.srv.mu.Lock()
		live := len(h.srv.runs)
		h.srv.mu.Unlock()
		if live == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run still registered after finalize panicked")
		}
		time.Sleep(5 * time.Millisecond)
	}
	logMu.Lock()
	logged := logBuf.String()
	logMu.Unlock()
	if !strings.Contains(logged, "run finalize panicked") {
		t.Errorf("the recovered finalize panic was not attributed in the log:\n%s", logged)
	}
	st.armed.Store(false)
	h.setHandler(echoHandler)
	if task := h.send("alice"); task.Status.State != TaskStateCompleted {
		t.Fatalf("post-panic send = %s, want COMPLETED", task.Status.State)
	}
}

// panicTransport is a host RoundTripper with a bug.
type panicTransport struct{ calls atomic.Int32 }

func (p *panicTransport) RoundTrip(*http.Request) (*http.Response, error) {
	p.calls.Add(1)
	panic("transport-detail-from-push")
}

func TestPushClientPanicContained(t *testing.T) {
	rt := &panicTransport{}
	var logMu sync.Mutex
	var logBuf bytes.Buffer
	h := newHarness(t, func(c *Config) {
		c.Push = PushOptions{AllowPrivate: true, Client: &http.Client{Transport: rt}}
		c.Logger = slog.New(slog.NewTextHandler(&lockedWriter{mu: &logMu, w: &logBuf}, nil))
	})
	h.send("alice", map[string]any{
		"taskPushNotificationConfig": map[string]any{"url": "http://127.0.0.1:9/hook"},
	})

	// The deliveries run on the drain goroutine; wait for one to have
	// panicked, then for its slot to come back.
	deadline := time.Now().Add(3 * time.Second)
	for rt.calls.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no push delivery attempted")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for len(h.srv.push.sem) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the panicked delivery never gave its slot back")
		}
		time.Sleep(5 * time.Millisecond)
	}
	logMu.Lock()
	logged := logBuf.String()
	logMu.Unlock()
	if !strings.Contains(logged, "push delivery panicked") {
		t.Errorf("the recovered push panic was not attributed in the log:\n%s", logged)
	}
	// Still serving.
	if task := h.send("alice"); task.Status.State != TaskStateCompleted {
		t.Fatalf("post-panic send = %s, want COMPLETED", task.Status.State)
	}
}

// lockedWriter serializes writes from the server's goroutines into one
// buffer the test reads.
type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w == nil {
		return 0, errors.New("no buffer")
	}
	return l.w.Write(p)
}
