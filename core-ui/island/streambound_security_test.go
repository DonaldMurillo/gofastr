package island

import (
	"testing"
	"time"
)

// NewManager enforces the SSE heartbeat/stream-bound pair invariant
// (2026-09-05 round-4 finding, family F26 Interactive-layer availability).
//
// Property: a configured SSE stream lifetime must always exceed the
// heartbeat interval, so every stream gets at least one keepalive write
// before the bound reclaims it — the constructor's own documented clamp
// ("The bound must exceed the heartbeat interval; a non-positive or
// sub-heartbeat value restores the default", WithSSEStreamBound doc).
// WithSSEStreamBound accepts any positive value verbatim and options apply
// in caller order, so the cross-check has to live at the END of NewManager;
// a 1s bound against the 15s default heartbeat otherwise installs
// sseStreamBound=1s, ServeSSEWithPresence's bound timer reclaims every
// stream long before the first heartbeat tick — the #159 stranded-stream
// safety net becomes a stream killer, the heartbeat's write-error detection
// never runs, and every EventSource reconnects once per bound per tab.
func TestStreamBoundRestoresDefaultSubHeartbeat(t *testing.T) {
	cases := []struct {
		name  string
		opts  []ManagerOption
		check func(m *Manager) bool
		note  string
	}{
		{
			name:  "bound below heartbeat",
			opts:  []ManagerOption{WithSSEHeartbeat(DefaultSSEHeartbeat), WithSSEStreamBound(1 * time.Second)},
			check: func(m *Manager) bool { return m.sseStreamBound == DefaultSSEStreamBound },
			note:  "a 1s bound against the 15s default heartbeat is documented to restore the 5m default",
		},
		{
			name:  "bound equals heartbeat",
			opts:  []ManagerOption{WithSSEHeartbeat(DefaultSSEHeartbeat), WithSSEStreamBound(DefaultSSEHeartbeat)},
			check: func(m *Manager) bool { return m.sseStreamBound == DefaultSSEStreamBound },
			note:  "bound == heartbeat leaves no window for the first keepalive: documented to restore the default",
		},
		{
			name:  "sub-nanosecond-scale bound",
			opts:  []ManagerOption{WithSSEStreamBound(time.Nanosecond)},
			check: func(m *Manager) bool { return m.sseStreamBound == DefaultSSEStreamBound },
			note:  "1ns is positive, so the d > 0 guard accepts it; every stream is reclaimed before any write",
		},
		{
			name:  "heartbeat raised above an already-set bound",
			opts:  []ManagerOption{WithSSEStreamBound(30 * time.Second), WithSSEHeartbeat(time.Hour)},
			check: func(m *Manager) bool { return m.sseStreamBound > m.sseHeartbeat },
			note:  "\"The bound must exceed the heartbeat interval\" — the pair invariant must hold regardless of option order",
		},
	}
	for _, tc := range cases {
		m := NewManager(tc.opts...)
		if !tc.check(m) {
			t.Errorf("SECURITY: [island] %s: heartbeat=%v streamBound=%v — %s. "+
				"WithSSEStreamBound's doc promises a sub-heartbeat bound restores the default, but a bound below the "+
				"heartbeat reclaims every SSE stream before the first keepalive write: the #159 stranded-stream safety "+
				"net kills the lane and each tab reconnects once per bound",
				tc.name, m.sseHeartbeat, m.sseStreamBound, tc.note)
		}
	}
}
