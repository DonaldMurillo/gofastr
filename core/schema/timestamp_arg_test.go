package schema

import (
	"testing"
	"time"
)

// TestTimestampArgConversions: times and RFC 3339 strings bind in
// TimestampLayout; a nil pointer, an unparseable string and any other
// value pass through unchanged.
func TestTimestampArgConversions(t *testing.T) {
	at := time.Date(2026, 11, 19, 10, 30, 0, 5000, time.FixedZone("x", 2*3600))
	const want = "2026-11-19T08:30:00.000005Z"
	var nilTime *time.Time
	for _, tc := range []struct {
		name string
		in   any
		want any
	}{
		{"time", at, want},
		{"pointer", &at, want},
		{"rfc3339", "2026-11-19T10:30:00.000005+02:00", want},
		{"nil pointer", nilTime, nilTime},
		{"unparseable", "2026-11-19 10:30", "2026-11-19 10:30"},
		{"other", 42, 42},
	} {
		if got := TimestampArg(tc.in); got != tc.want {
			t.Errorf("%s: TimestampArg(%v) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}
