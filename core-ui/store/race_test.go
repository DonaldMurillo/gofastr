package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestRace_PerRequestValueIsolation is the correctness property of the
// two-layer registry: N concurrent renders with distinct per-request
// seed values must show zero cross-request bleed. Run with -race.
func TestRace_PerRequestValueIsolation(t *testing.T) {
	resetForTest()
	slice := New("org").String("companyName", "DefaultCo")

	const n = 64
	var wg sync.WaitGroup
	errs := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			want := "Tenant-" + strconv.Itoa(i)
			ctx := WithValues(context.Background())
			slice.Seed(ctx, want)

			// Consumer binding must stamp THIS request's value.
			bound := string(slice.Bind(ctx, "span", nil))
			if !strings.Contains(bound, ">"+want+"<") {
				errs[i] = fmt.Sprintf("bind bled: %s (want %s)", bound, want)
				return
			}
			// Seed resolution must also be isolated to this request.
			seed := ResolveSeed(ctx, []string{slice.Name()})
			if seed[slice.Name()] != want {
				errs[i] = fmt.Sprintf("seed bled: got %v want %s", seed[slice.Name()], want)
			}
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != "" {
			t.Errorf("goroutine %d: %s", i, e)
		}
	}
}

// TestRace_ConcurrentIdenticalRegistration ensures registering the same
// slice from many goroutines is safe (idempotent), no torn writes.
func TestRace_ConcurrentIdenticalRegistration(t *testing.T) {
	resetForTest()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = New("x").Int("count", 0)
		}()
	}
	wg.Wait()
}
