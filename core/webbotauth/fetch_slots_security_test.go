package webbotauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// One origin that never answers, named through many query-varied
// jwks_uri identifiers on many subdomains, defeats coalescing and the
// negative cache. It must not hold every fetch slot: a different
// agent's directory still resolves.
func TestSlowOriginLeavesSlotsForOthers(t *testing.T) {
	release := make(chan struct{})
	var slowHits atomic.Int32
	srv := startTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "fast.test" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"keys":[]}`)
			return
		}
		slowHits.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })
	res := resolverForServer(t, srv)

	var refused atomic.Int32
	for i := range maxConcurrentFetches {
		ref := &agentRef{
			identifier: fmt.Sprintf("https://a%d.slow.test/jwks?v=%d", i, i),
			fetchURL:   fmt.Sprintf("https://a%d.slow.test/jwks?v=%d", i, i),
			typ:        discoveryJWKS,
		}
		go func() {
			if _, err := res.resolve(context.Background(), ref); errors.Is(err, ErrResolverBusy) {
				refused.Add(1)
			}
		}()
	}
	// Let the slow fetches take whatever slots they can get.
	deadline := time.Now().Add(2 * time.Second)
	for slowHits.Load()+refused.Load() < maxConcurrentFetches && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := slowHits.Load(); got > maxFetchesPerSite {
		t.Errorf("one site holds %d fetches; want at most %d", got, maxFetchesPerSite)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	fast := &agentRef{identifier: "https://fast.test/jwks", fetchURL: "https://fast.test/jwks", typ: discoveryJWKS}
	if _, err := res.resolve(ctx, fast); err != nil {
		t.Fatalf("another agent's directory did not resolve while one origin hung: %v", err)
	}
}

// With every slot taken, a fetch waits a bounded time and reports the
// resolver busy, instead of waiting as long as the request lives.
func TestFetchSlotWaitIsBounded(t *testing.T) {
	res := newDirectoryResolver(nil)
	res.slotWait = 50 * time.Millisecond
	for range cap(res.sem) {
		res.sem <- struct{}{}
	}
	ref := &agentRef{identifier: "https://busy.test/jwks", fetchURL: "https://busy.test/jwks", typ: discoveryJWKS}
	done := make(chan error, 1)
	go func() { _, err := res.resolve(context.Background(), ref); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrResolverBusy) {
			t.Fatalf("err = %v; want ErrResolverBusy", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fetch waited for a slot with no bound")
	}
	// Busy is not evidence about the directory: nothing negative-cached.
	if _, ok := res.neg.get(ref.identifier); ok {
		t.Error("a busy resolver negative-cached the identifier")
	}
}

// A busy fetch budget at key resolution is our own backpressure, like
// a busy DNS budget: unverified and retryable, so require mode answers
// 503, not 403.
func TestBusyKeyFetchIsRetryable(t *testing.T) {
	v := loadVector[wbaVector](t, "testdata/wba-e.2.1-ed25519-dictionary.json")
	res, _ := directoryServer(t, v.DirectoryHost, v.DirectoryBody)
	res.slotWait = 10 * time.Millisecond
	for range cap(res.sem) {
		res.sem <- struct{}{}
	}
	r := v.Request.toHTTPRequest(t)
	r.Header.Set("Signature-Input", v.SignatureInput)
	r.Header.Set("Signature", v.SignatureLabel+"=:"+v.Signature+":")
	got := testVerifier(res, time.Now(), false).VerifyRequest(r)
	if got.Outcome != OutcomeUnverified || !got.Retryable {
		t.Fatalf("outcome %s retryable=%v (%s); want unverified and retryable", got.Outcome, got.Retryable, got.Reason)
	}
}

// During a directory outage a lapsed entry keeps serving, and the
// refetch is retried once per negative TTL, not on every request.
func TestFailedRefreshBacksOff(t *testing.T) {
	md := newMutableDirectory(t, `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs","use":"sig"}]}`)
	res := md.resolver(t)
	now := time.Now()
	res.now = func() time.Time { return now }
	ref := &agentRef{identifier: "https://outage.test/x", fetchURL: md.srv.srv.URL, typ: discoveryJWKS}
	if _, err := res.resolve(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	md.fail.Store(true)
	now = now.Add(2 * time.Hour)
	for range 5 {
		if _, err := res.resolve(context.Background(), ref); err != nil {
			t.Fatalf("stale entry not served: %v", err)
		}
	}
	if got := md.hits.Load(); got != 2 {
		t.Fatalf("outage refetched %d times over 5 requests; want 1 refetch", got-1)
	}
	now = now.Add(res.negativeTTL + time.Second)
	if _, err := res.resolve(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if got := md.hits.Load(); got != 3 {
		t.Fatalf("after the backoff %d fetches; want one more retry", got)
	}
}
