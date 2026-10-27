package framework

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// The default idempotency store is per process: beside a fanout (several
// replicas) the app warns that a retry on another replica runs again.
func TestIdempotencyMemoryStoreWarnsWithFanout(t *testing.T) {
	for name, tc := range map[string]struct {
		opts []AppOption
		warn bool
	}{
		"memory + fanout": {[]AppOption{WithIdempotency(middleware.IdempotencyConfig{}), WithFanout(fanout.NewInProcess())}, true},
		"memory alone":    {[]AppOption{WithIdempotency(middleware.IdempotencyConfig{})}, false},
		"own store":       {[]AppOption{WithIdempotency(middleware.IdempotencyConfig{Store: middleware.NewMemoryIdempotencyStore(0)}), WithFanout(fanout.NewInProcess())}, false},
	} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			NewApp(append([]AppOption{WithLogger(slog.New(slog.NewTextHandler(&logs, nil)))}, tc.opts...)...)
			if got := strings.Contains(logs.String(), "IdempotencyConfig.Store"); got != tc.warn {
				t.Errorf("warned = %v, want %v; logs:\n%s", got, tc.warn, logs.String())
			}
		})
	}
}
