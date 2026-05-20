package log

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebhookBatchesAndPosts(t *testing.T) {
	var (
		mu       sync.Mutex
		captured [][]json.RawMessage
		hits     atomic.Int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		var body struct {
			Entries []json.RawMessage `json:"entries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		mu.Lock()
		captured = append(captured, body.Entries)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := WebhookSink(srv.URL, WebhookOpts{
		BatchSize:     3,
		BatchInterval: time.Hour, // size-triggered only
	})
	for i := 0; i < 3; i++ {
		_ = s.Write([]byte(`{"i":` + string(rune('0'+i)) + `}`))
	}
	// Wait for the batch to flush.
	deadline := time.Now().Add(2 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if got := hits.Load(); got != 1 {
		t.Fatalf("hits = %d, want 1", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 || len(captured[0]) != 3 {
		t.Fatalf("got batches=%v", captured)
	}
}

func TestWebhookFlushesOnInterval(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := WebhookSink(srv.URL, WebhookOpts{
		BatchSize:     1000, // never triggered
		BatchInterval: 100 * time.Millisecond,
	})
	_ = s.Write([]byte(`{"k":1}`))

	deadline := time.Now().Add(2 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	_ = s.Close()
	if hits.Load() == 0 {
		t.Fatal("expected interval flush")
	}
}

func TestWebhookCloseFlushesPending(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := WebhookSink(srv.URL, WebhookOpts{
		BatchSize:     1000,
		BatchInterval: time.Hour,
	})
	for i := 0; i < 5; i++ {
		_ = s.Write([]byte(`{"k":1}`))
	}
	_ = s.Close()
	if hits.Load() != 1 {
		t.Fatalf("close should flush pending; hits=%d", hits.Load())
	}
}

// TestWebhookCloseIsRaceSafe pins that two concurrent callers can both
// invoke Close without panicking on "close of closed channel".
func TestWebhookCloseIsRaceSafe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s := WebhookSink(srv.URL, WebhookOpts{})

	done := make(chan struct{}, 2)
	closer := func() {
		defer func() { _ = recover() }()
		_ = s.Close()
		done <- struct{}{}
	}
	go closer()
	go closer()
	<-done
	<-done

	// Third call should also succeed.
	if err := s.Close(); err != nil {
		t.Fatalf("third Close = %v, want nil", err)
	}
}

// TestWebhookWriteAfterClose pins that the sink is safe to call after
// shutdown — derived loggers held past Stop don't crash, and don't
// silently accumulate entries that never ship.
func TestWebhookWriteAfterClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s := WebhookSink(srv.URL, WebhookOpts{})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Write([]byte(`{"a":1}`)); err != ErrSinkClosed {
		t.Fatalf("Write after Close = %v, want ErrSinkClosed", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
}

// TestWebhookCloseInterruptsRetry pins that a Close mid-retry returns
// quickly even when the server is wedged — without this, App.Stop would
// block for up to (Timeout × MaxRetries).
func TestWebhookCloseInterruptsRetry(t *testing.T) {
	// Server: always 503 (retryable), holds the connection.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	s := WebhookSink(srv.URL, WebhookOpts{
		BatchSize:     1,
		BatchInterval: time.Hour,
		MaxRetries:    20,                     // many retries to make total possible delay huge
		Timeout:       100 * time.Millisecond, // each HTTP attempt small but retry sleep grows
	})
	_ = s.Write([]byte(`{"k":1}`))
	time.Sleep(50 * time.Millisecond) // let the worker pick it up + start retrying

	closed := make(chan struct{})
	start := time.Now()
	go func() {
		_ = s.Close()
		close(closed)
	}()
	select {
	case <-closed:
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("Close took %v — should interrupt retry promptly", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not return; retry sleep is not shutdown-aware")
	}
}

// TestWebhookDroppedCounterAdvances pins the new observability hook:
// drops are no longer silent.
func TestWebhookDroppedCounterAdvances(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(block)

	s := WebhookSink(srv.URL, WebhookOpts{
		BatchSize:     1000,
		BatchInterval: time.Hour,
		QueueSize:     2,
		Timeout:       20 * time.Millisecond,
		MaxRetries:    0,
	}).(*webhookSink)
	defer s.Close()

	for i := 0; i < 50; i++ {
		_ = s.Write([]byte(`{"k":1}`))
	}
	if s.Dropped() == 0 {
		t.Fatal("Dropped() returned 0 after over-filling QueueSize=2 with 50 writes")
	}
}

func TestWebhookDropsOldestWhenQueueFull(t *testing.T) {
	// Server never responds in time; queue fills.
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(block)

	s := WebhookSink(srv.URL, WebhookOpts{
		BatchSize:     1000,
		BatchInterval: time.Hour,
		QueueSize:     2,
		Timeout:       50 * time.Millisecond,
		MaxRetries:    0,
	})
	defer s.Close()

	// Write more than QueueSize; should not panic, should not block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_ = s.Write([]byte(`{"k":1}`))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Write blocked when queue full")
	}
}
