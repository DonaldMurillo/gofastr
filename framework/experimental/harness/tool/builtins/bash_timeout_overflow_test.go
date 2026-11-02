package builtins

import (
	"context"
	"testing"
)

// timeout_ms was multiplied into a Duration before the 10-minute clamp,
// so a huge value wrapped first: 18446744073710 ms wraps to about 448µs,
// which passed both the <= 0 default and the 10-minute clamp, and the
// command was killed before it started. The milliseconds are now bounded
// before the multiply, so a huge timeout_ms takes the 10-minute ceiling.
func TestBashHugeTimeoutTakesTheCeiling(t *testing.T) {
	skipNonUnix(t)
	res, err := (Bash{}).Run(context.Background(), mustCall(t, map[string]any{
		"cmd":        "sleep 0.2; echo done",
		"timeout_ms": int64(18446744073710),
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("a huge timeout_ms killed the command: %+v", res)
	}
}
