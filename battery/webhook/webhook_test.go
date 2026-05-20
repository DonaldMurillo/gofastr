package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ----- signature -------------------------------------------------------------

func TestSign_DeterministicAndVerifies(t *testing.T) {
	sig := Sign("topsecret", []byte(`{"k":1}`))
	if !strings.HasPrefix(sig, SignaturePrefix) {
		t.Fatalf("sig missing prefix: %q", sig)
	}
	if !Verify("topsecret", sig, []byte(`{"k":1}`)) {
		t.Fatalf("Verify rejected its own signature")
	}
	if Sign("topsecret", []byte(`{"k":1}`)) != sig {
		t.Fatalf("Sign should be deterministic")
	}
}

func TestVerify_RejectsTampering(t *testing.T) {
	sig := Sign("s", []byte("abc"))
	if Verify("s", sig, []byte("abd")) {
		t.Fatalf("modified body should fail verification")
	}
	if Verify("other", sig, []byte("abc")) {
		t.Fatalf("wrong secret should fail verification")
	}
	if Verify("", sig, []byte("abc")) {
		t.Fatalf("empty secret should never verify")
	}
	if Verify("s", "md5=00", []byte("abc")) {
		t.Fatalf("wrong algorithm prefix should fail verification")
	}
}

// ----- glob matching ---------------------------------------------------------

func TestMatch_Patterns(t *testing.T) {
	cases := []struct {
		event   string
		pattern string
		want    bool
	}{
		{"a.b", "*", true},
		{"a.b", "a.b", true},
		{"a.b", "a.*", true},
		{"a.b", "*.b", true},
		{"a.b", "a.c", false},
		{"a.b.c", "a.*", false}, // segment count mismatch
		{"a.b.c", "a.*.c", true},
		{"a", "a.*", false},
	}
	for _, tc := range cases {
		if got := matchOne(tc.event, tc.pattern); got != tc.want {
			t.Errorf("matchOne(%q, %q) = %v want %v", tc.event, tc.pattern, got, tc.want)
		}
	}
}

// ----- manager: end-to-end delivery ------------------------------------------

func TestManager_SuccessfulDeliverySignsBody(t *testing.T) {
	var (
		gotSig    string
		gotEvent  string
		gotBody   []byte
		gotMu     sync.Mutex
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMu.Lock()
		gotSig = r.Header.Get(SignatureHeader)
		gotEvent = r.Header.Get("X-GoFastr-Event")
		gotBody, _ = io.ReadAll(r.Body)
		gotMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	mgr := newTestManager(t)
	defer mgr.Stop(context.Background())

	ctx := context.Background()
	if _, err := mgr.Subscribe(ctx, Subscriber{
		URL:    srv.URL,
		Secret: "shh",
		Events: []string{"orders.created"},
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	body := []byte(`{"id":42}`)
	queued, err := mgr.Publish(ctx, "orders.created", body)
	if err != nil || queued != 1 {
		t.Fatalf("publish: %d, %v", queued, err)
	}

	waitFor(t, func() bool {
		gotMu.Lock()
		defer gotMu.Unlock()
		return gotSig != ""
	}, time.Second, "delivery did not arrive")

	gotMu.Lock()
	defer gotMu.Unlock()
	if !VerifyTimestamped("shh", gotSig, gotBody, 5*time.Minute) {
		t.Fatalf("delivered body did not verify against subscriber secret: sig=%q", gotSig)
	}
	if gotEvent != "orders.created" {
		t.Fatalf("event header: got %q", gotEvent)
	}
	if string(gotBody) != string(body) {
		t.Fatalf("body: got %q want %q", gotBody, body)
	}
}

func TestManager_NonMatchingEventIsNotDelivered(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	mgr := newTestManager(t)
	defer mgr.Stop(context.Background())

	if _, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    srv.URL,
		Secret: "x",
		Events: []string{"orders.*"},
	}); err != nil {
		t.Fatal(err)
	}
	queued, _ := mgr.Publish(context.Background(), "users.created", []byte("{}"))
	if queued != 0 {
		t.Fatalf("queued non-matching event: %d", queued)
	}

	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("subscriber received non-matching event")
	}
}

func TestManager_RetriesOnFailureThenDead(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	store := NewMemoryStore()
	mgr := New(store, Options{
		MaxAttempts:          3,
		Backoff:              []time.Duration{0, 0, 0},
		PollInterval:         5 * time.Millisecond,
		AllowPrivateNetworks: true,
	})
	mgr.Start()
	defer mgr.Stop(context.Background())

	ctx := context.Background()
	if _, err := mgr.Subscribe(ctx, Subscriber{
		URL:    srv.URL,
		Secret: "x",
		Events: []string{"*"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Publish(ctx, "x", []byte("{}")); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool {
		dl, _ := store.ListDeliveries(ctx, "", 0)
		return len(dl) == 1 && dl[0].Status == StatusDead
	}, 2*time.Second, "delivery never reached dead state")

	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
}

func TestManager_SuccessfulDeliveryStopsRetrying(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewMemoryStore()
	mgr := New(store, Options{
		MaxAttempts:          5,
		Backoff:              []time.Duration{0},
		PollInterval:         5 * time.Millisecond,
		AllowPrivateNetworks: true,
	})
	mgr.Start()
	defer mgr.Stop(context.Background())

	ctx := context.Background()
	if _, err := mgr.Subscribe(ctx, Subscriber{URL: srv.URL, Secret: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Publish(ctx, "x", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		dl, _ := store.ListDeliveries(ctx, "", 0)
		return len(dl) == 1 && dl[0].Status == StatusSuccess
	}, time.Second, "no successful delivery")
	time.Sleep(40 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("should have stopped retrying after success; got %d calls", got)
	}
}

func TestManager_DeadIfSubscriberRemoved(t *testing.T) {
	store := NewMemoryStore()
	mgr := New(store, Options{
		MaxAttempts:          5,
		Backoff:              []time.Duration{0},
		PollInterval:         5 * time.Millisecond,
		AllowPrivateNetworks: true,
	})
	mgr.Start()
	defer mgr.Stop(context.Background())

	ctx := context.Background()
	s, err := mgr.Subscribe(ctx, Subscriber{URL: "http://127.0.0.1:1", Secret: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Publish(ctx, "x", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Unsubscribe(ctx, s.ID); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool {
		dl, _ := store.ListDeliveries(ctx, "", 0)
		return len(dl) == 1 && dl[0].Status == StatusDead && dl[0].LastError != ""
	}, time.Second, "delivery should have been marked dead")
}

func TestSubscribe_RejectsBadInput(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.Stop(context.Background())
	ctx := context.Background()
	if _, err := mgr.Subscribe(ctx, Subscriber{Secret: "x"}); err == nil {
		t.Fatal("expected error on missing URL")
	}
	if _, err := mgr.Subscribe(ctx, Subscriber{URL: "http://x"}); err == nil {
		t.Fatal("expected error on missing secret")
	}
}

// ----- helpers --------------------------------------------------------------

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	mgr := New(NewMemoryStore(), Options{
		MaxAttempts:          1,
		Backoff:              []time.Duration{0},
		PollInterval:         5 * time.Millisecond,
		AllowPrivateNetworks: true,
	})
	mgr.Start()
	return mgr
}

func waitFor(t *testing.T, fn func() bool, max time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", msg)
}
