package webhook

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ----- response body size limit ----------------------------------------------

func TestAttempt_LimitsResponseBodyRead(t *testing.T) {
	// Subscriber returns way more bytes than the manager should pull —
	// we must cap the read so a malicious receiver can't exhaust RAM.
	const huge = 4 << 20 // 4 MiB > default 64 KiB cap
	junk := make([]byte, huge)
	_, _ = rand.Read(junk)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(junk)
	}))
	defer srv.Close()

	store := NewMemoryStore()
	mgr := New(store, Options{
		MaxAttempts:          1,
		Backoff:              []time.Duration{0},
		PollInterval:         5 * time.Millisecond,
		MaxResponseBodyBytes: 64 << 10, // 64 KiB cap
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
	}, 2*time.Second, "delivery never succeeded with capped body")
}

// ----- HMAC includes timestamp -----------------------------------------------

func TestSignWithTimestamp_RejectsReplay(t *testing.T) {
	secret := "topsecret"
	body := []byte(`{"k":1}`)
	old := time.Now().Add(-10 * time.Minute).Unix()

	header := SignWithTimestamp(secret, old, body)
	// Within tolerance — must verify when the timestamp matches what
	// we present to VerifyTimestamp.
	if !VerifyTimestamped(secret, header, body, time.Hour) {
		t.Fatalf("VerifyTimestamped within tolerance should succeed")
	}
	// Beyond tolerance — must reject even though the HMAC bytes match.
	if VerifyTimestamped(secret, header, body, 1*time.Minute) {
		t.Fatalf("VerifyTimestamped must reject replay outside tolerance")
	}
}

func TestSignWithTimestamp_RejectsTamperedTimestamp(t *testing.T) {
	secret := "s"
	body := []byte("abc")
	ts := time.Now().Unix()
	header := SignWithTimestamp(secret, ts, body)
	// Adversary swaps the timestamp portion: must invalidate the signature.
	parts := strings.Split(header, ",")
	if len(parts) != 2 {
		t.Fatalf("expected t=...,v1=... header, got %q", header)
	}
	tampered := "t=" + "99999," + parts[1]
	if VerifyTimestamped(secret, tampered, body, time.Hour) {
		t.Fatalf("verifier must reject tampered timestamp")
	}
}

func TestAttempt_SignatureHeaderIsTimestampForm(t *testing.T) {
	var gotSig string
	var gotTs string
	var gotBody []byte
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotSig = r.Header.Get(SignatureHeader)
		gotTs = r.Header.Get("X-GoFastr-Timestamp")
		gotBody, _ = io.ReadAll(r.Body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	mgr := newTestManager(t)
	defer mgr.Stop(context.Background())

	ctx := context.Background()
	if _, err := mgr.Subscribe(ctx, Subscriber{URL: srv.URL, Secret: "shh"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Publish(ctx, "evt", []byte(`{"x":1}`)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return gotSig != ""
	}, time.Second, "no delivery")
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(gotSig, "t=") || !strings.Contains(gotSig, "v1=") {
		t.Fatalf("sig header should carry t= and v1=, got %q", gotSig)
	}
	if !VerifyTimestamped("shh", gotSig, gotBody, time.Hour) {
		t.Fatalf("delivered sig didn't verify; ts=%q sig=%q", gotTs, gotSig)
	}
}

// ----- worker respects ctx cancellation on Stop -----------------------------

func TestStop_CancelsInflightHTTPRequest(t *testing.T) {
	// Receiver hangs longer than the manager's HTTP timeout. Stop with
	// a tight ctx must abort the in-flight call instead of waiting for
	// the receiver to respond on its own schedule.
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-blocked:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(blocked)

	store := NewMemoryStore()
	mgr := New(store, Options{
		MaxAttempts:  1,
		Backoff:      []time.Duration{0},
		PollInterval: 5 * time.Millisecond,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second, // longer than the test wants to wait
		},
		AllowPrivateNetworks: true,
	})
	mgr.Start()

	ctx := context.Background()
	if _, err := mgr.Subscribe(ctx, Subscriber{URL: srv.URL, Secret: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Publish(ctx, "x", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	// Give the worker time to pick up the delivery and start dialing.
	time.Sleep(50 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := mgr.Stop(stopCtx); err != nil {
		t.Fatalf("Stop returned err: %v", err)
	}
	if took := time.Since(start); took > 600*time.Millisecond {
		t.Fatalf("Stop should cancel in-flight request and return promptly; took %v", took)
	}
}

// ----- newID panics on entropy failure (defensive) --------------------------

// We can't easily inject a rand failure in unit tests; instead assert the
// invariant that newID never returns the all-zero ID — which would be the
// observable symptom of an unchecked rand.Read error.
func TestNewID_NotAllZero(t *testing.T) {
	for i := 0; i < 200; i++ {
		if newID() == "00000000000000000000000000000000" {
			t.Fatalf("newID returned all-zero id on iteration %d", i)
		}
	}
}

// ----- ** recursive wildcard ------------------------------------------------

func TestMatch_DoubleStarMatchesAnyDepth(t *testing.T) {
	cases := []struct {
		event   string
		pattern string
		want    bool
	}{
		{"orders.created", "orders.**", true},
		{"orders.created.v2", "orders.**", true},
		{"orders.line.added", "orders.**", true},
		{"users.created", "orders.**", false},
		{"a.b.c.d", "a.**.d", true},
		{"a.b.c.d", "a.**.c", false}, // ** is greedy to the right
	}
	for _, tc := range cases {
		if got := matchOne(tc.event, tc.pattern); got != tc.want {
			t.Errorf("matchOne(%q, %q) = %v want %v", tc.event, tc.pattern, got, tc.want)
		}
	}
}

// ----- Subscribe honors caller's Active flag --------------------------------

func TestSubscribe_HonorsPausedFlag(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.Stop(context.Background())

	s, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    "https://example.com/hook",
		Secret: "x",
		Paused: true, // explicit opt-out
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Active {
		t.Fatalf("caller asked for Paused=true; got Active=true")
	}

	s2, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    "https://example.com/hook2",
		Secret: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s2.Active {
		t.Fatalf("zero-value Active+Paused must default to active (backwards compatible)")
	}
}

// ----- Publish to a paused subscriber does not deliver ----------------------

func TestPublish_SkipsPausedSubscribers(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	mgr := newTestManager(t)
	defer mgr.Stop(context.Background())

	if _, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    srv.URL,
		Secret: "x",
		Paused: true,
	}); err != nil {
		t.Fatal(err)
	}
	if queued, _ := mgr.Publish(context.Background(), "x", []byte("{}")); queued != 0 {
		t.Fatalf("expected zero queued for paused subscriber, got %d", queued)
	}
	time.Sleep(50 * time.Millisecond)
	if c := atomic.LoadInt32(&calls); c != 0 {
		t.Fatalf("paused subscriber must not receive deliveries; got %d calls", c)
	}
}
