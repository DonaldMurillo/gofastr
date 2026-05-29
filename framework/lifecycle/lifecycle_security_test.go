package lifecycle_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/lifecycle"
)

// TestTimeoutRaceShutdownVsSetter asserts that reading lc.timeout during
// Shutdown is synchronised against a concurrent SetShutdownTimeout. Run
// under `go test -race` to surface the unsynchronised read/write.
func TestTimeoutRaceShutdownVsSetter(t *testing.T) {
	var wg sync.WaitGroup

	// A drainer that blocks briefly so Shutdown's timeout read overlaps
	// with the concurrent setter.
	blocker := lifecycle.DrainFunc(func(ctx context.Context) error {
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
		}
		return nil
	})

	for i := 0; i < 50; i++ {
		lc := lifecycle.New()
		if err := lc.RegisterDrainer(blocker); err != nil {
			t.Fatalf("RegisterDrainer: %v", err)
		}

		wg.Add(2)
		go func() {
			defer wg.Done()
			lc.SetShutdownTimeout(10 * time.Millisecond)
		}()
		go func() {
			defer wg.Done()
			_ = lc.Shutdown(context.Background())
		}()
	}
	wg.Wait()
}
