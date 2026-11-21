package queue

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/settle"
)

// queue keeps its own settle timeout so it stays unmountable (a copy may
// not import gofastr internal/ packages); this pins it to internal/settle.
func TestSettleTimeoutMatchesSettle(t *testing.T) {
	if settleTimeout != settle.Timeout {
		t.Errorf("settleTimeout = %v, settle.Timeout = %v", settleTimeout, settle.Timeout)
	}
}
