package webbotauth

import (
	"context"
	"strings"
	"testing"
	"time"
)

// checkHostPublic runs inside parseAgentRef, which happens before the
// cache lookup and before fetchBounded takes its slot — so the fetch
// semaphore bounded fetches while nothing bounded the resolver calls.
// Concurrent requests naming distinct hostnames could open unbounded
// concurrent lookups, each cheap to ask for and up to dnsLookupTimeout
// long to answer.
func TestCheckHostPublic_LookupsAreBounded(t *testing.T) {
	// Saturate the package-wide lookup budget, then release it whatever
	// the test does next.
	for i := 0; i < maxConcurrentLookups; i++ {
		dnsSem <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < maxConcurrentLookups; i++ {
			<-dnsSem
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := checkHostPublic(ctx, "example.test")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("checkHostPublic resolved a hostname with no lookup slot available")
	}
	if !strings.Contains(err.Error(), "resolver busy") {
		t.Errorf("error = %v, want the refusal that names the exhausted budget", err)
	}
	// It must give up on the caller's deadline, not sit on the resolver.
	if elapsed > time.Second {
		t.Errorf("waited %s for a lookup slot; the caller's deadline should have ended it", elapsed)
	}
}

// A literal IP costs no network, so it must never queue behind the
// lookup budget: that path is the cheap refusal for internal targets and
// has to stay unconditional.
func TestCheckHostPublic_LiteralIPSkipsTheLookupBudget(t *testing.T) {
	for i := 0; i < maxConcurrentLookups; i++ {
		dnsSem <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < maxConcurrentLookups; i++ {
			<-dnsSem
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := checkHostPublic(ctx, "169.254.169.254"); err == nil {
		t.Fatal("link-local literal was allowed")
	} else if strings.Contains(err.Error(), "resolver busy") {
		t.Errorf("literal IP queued behind the lookup budget: %v", err)
	}
	if err := checkHostPublic(ctx, "93.184.216.34"); err != nil {
		t.Errorf("public literal refused: %v", err)
	}
}
