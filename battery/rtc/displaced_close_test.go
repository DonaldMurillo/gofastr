package rtc

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/stream"
)

// A socket pushed out by a join or a Kick is half-open here: its client
// never answers the Close frame. Nobody else waits out its handshake.
func TestClosingADisplacedSocketBlocksNoOneElse(t *testing.T) {
	const closeTimeout = 5 * time.Second
	const bound = 2 * time.Second
	cases := []struct {
		name string
		cfg  Config
		old  string // the socket that goes
		act  func(t *testing.T, s *Signaler, base string)
	}{
		{"rejoin", Config{}, "/ws/room1?peer=p1&user=u1", func(t *testing.T, _ *Signaler, base string) {
			c, _ := joinAs(t, base, "room1", "p1", "u1")
			t.Cleanup(c.close)
		}},
		{"seat cap", Config{MaxSocketsPerUser: 1}, "/ws/roomA?peer=p1&user=u1", func(t *testing.T, _ *Signaler, base string) {
			c, _ := joinAs(t, base, "roomB", "p2", "u1")
			t.Cleanup(c.close)
		}},
		{"kick", Config{}, "/ws/room1?peer=p1&user=u1", func(t *testing.T, s *Signaler, _ string) {
			if !s.Kick("room1", "p1") {
				t.Fatal("Kick did not find the peer")
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.WS = stream.WSConfig{CloseTimeout: closeTimeout}
			s := newTestSignaler(t, tc.cfg)
			base := startSignaler(t, s)
			old := wsDial(t, base+tc.old)
			t.Cleanup(old.close)
			old.expectEnv(t, "snapshot")

			start := time.Now()
			tc.act(t, s, base)
			if d := time.Since(start); d > bound {
				t.Fatalf("took %v: waited for the old socket's close handshake (CloseTimeout %v)", d.Round(10*time.Millisecond), closeTimeout)
			}
			// The old socket is still told to go.
			if _, err := old.recv(bound); err != errClosed {
				t.Fatalf("old socket: got %v, want a close frame", err)
			}
		})
	}
}
