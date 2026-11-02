package lifecycle_test

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/lifecycle"
)

// SetShutdownTimeout(0) and WithTimeout(0) stored the zero, and Shutdown
// then built its drain context with context.WithTimeout(ctx, 0): every
// drainer was handed a context that had already expired, so nothing got
// to drain. A negative timeout did the same. A non-positive timeout now
// keeps the current one (30s by default).
func TestNonPositiveShutdownTimeoutKeepsDefault(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		for name, set := range map[string]func(*lifecycle.Lifecycle){
			"SetShutdownTimeout": func(lc *lifecycle.Lifecycle) { lc.SetShutdownTimeout(d) },
			"WithTimeout":        func(lc *lifecycle.Lifecycle) { lifecycle.WithTimeout(d)(lc) },
		} {
			lc := lifecycle.New()
			set(lc)
			var drainErr error
			lc.RegisterDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
				drainErr = ctx.Err()
				return nil
			}))
			if err := lc.Shutdown(context.Background()); err != nil {
				t.Errorf("%s(%s): Shutdown: %v", name, d, err)
			}
			if drainErr != nil {
				t.Errorf("%s(%s): drainer got an expired context: %v", name, d, drainErr)
			}
		}
	}
}
