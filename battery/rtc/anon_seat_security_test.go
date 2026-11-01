package rtc

// Anonymous callers (no Join.User) shared one seat bucket: the 17th evicted
// the first, who rejoined and evicted the next. A userless join is now
// refused unless Config.AnonymousSeats names a policy, and under
// AnonymousPerSocket each such socket stands alone (issue #614, O62).

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestAnonymousJoinRefusedByDefault(t *testing.T) {
	s := New(Config{})
	base := startSignaler(t, s)
	res, err := http.Get(base + "/ws/room1?peer=pA")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("userless join: got %d, want 403", res.StatusCode)
	}
	c, _ := joinAs(t, base, "room1", "pB", "u")
	c.close()
}

func TestAnonymousPerSocketNoEviction(t *testing.T) {
	s := New(Config{AnonymousSeats: AnonymousPerSocket})
	base := startSignaler(t, s)
	const n = defaultSeatsPerUser + 1
	conns := make([]*wsClient, 0, n)
	defer func() {
		for _, c := range conns {
			c.close()
		}
	}()
	for i := range n {
		c, _ := join(t, base, fmt.Sprintf("lobby-%d", i), fmt.Sprintf("p%d", i))
		conns = append(conns, c)
	}
	if !conns[0].alive(300 * time.Millisecond) {
		t.Fatalf("the %dth anonymous socket evicted the first", n)
	}
}
