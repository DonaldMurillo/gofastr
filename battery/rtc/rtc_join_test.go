package rtc

// rtc_join_test.go pins two join-path invariants that the wire tests
// elsewhere take for granted: the room cap holds under concurrent
// joins, and a peer that offers the instant it sees a join reaches
// the newcomer.

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// wsTryDial is wsDial without the fatal paths: it reports ok=false on
// a non-101 answer so concurrent dials can be counted from goroutines.
func wsTryDial(url string) (*wsClient, bool) {
	host, p, found := strings.Cut(url, "/ws/")
	if !found {
		return nil, false
	}
	conn, err := net.Dial("tcp", strings.TrimPrefix(host, "http://"))
	if err != nil {
		return nil, false
	}
	var keyBytes [16]byte
	if _, err := rand.Read(keyBytes[:]); err != nil {
		conn.Close()
		return nil, false
	}
	req := "GET /ws/" + p + " HTTP/1.1\r\n" +
		"Host: " + strings.TrimPrefix(host, "http://") + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + base64.StdEncoding.EncodeToString(keyBytes[:]) + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, false
	}
	c := &wsClient{conn: conn, br: bufio.NewReader(conn)}
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	status, err := c.br.ReadString('\n')
	if err != nil || !strings.Contains(status, "101") {
		conn.Close()
		return nil, false
	}
	for {
		line, err := c.br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, false
		}
		if line == "\r\n" {
			break
		}
	}
	c.conn.SetReadDeadline(time.Time{})
	return c, true
}

// alive reports whether the server still holds the socket open: a
// close frame or a dead connection within d means no; a message or
// silence means yes.
func (c *wsClient) alive(d time.Duration) bool {
	_, err := c.recv(d)
	if err == nil {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// TestConcurrentJoinsNeverExceedCap: the cap is checked before the
// upgrade (so a refusal is an HTTP status) and again under the lock
// that registers the peer, so twelve joins racing for two seats end
// with two members, never three.
func TestConcurrentJoinsNeverExceedCap(t *testing.T) {
	s := newTestSignaler(t, Config{MaxPeers: 2})
	base := startSignaler(t, s)

	const dials = 12
	var wg sync.WaitGroup
	clients := make([]*wsClient, dials)
	for i := range dials {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, ok := wsTryDial(base + "/ws/room1?peer=p" + string(rune('a'+i)))
			if ok {
				clients[i] = c
			}
		}(i)
	}
	wg.Wait()
	for _, c := range clients {
		if c != nil {
			defer c.close()
		}
	}

	// Let every accepted socket register and every over-cap socket
	// be closed, then read the roster at rest.
	waitFor(t, 2*time.Second, func() bool {
		open := 0
		for _, c := range clients {
			if c != nil && c.alive(50*time.Millisecond) {
				open++
			}
		}
		return open <= 2
	}, "over-cap sockets to close")
	if got := len(s.Peers("room1")); got > 2 {
		t.Fatalf("room has %d members, cap is 2: the cap must hold under concurrent joins", got)
	}
	if got := len(s.Peers("room1")); got != 2 {
		t.Fatalf("room has %d members, want the cap filled (2)", got)
	}
}

// TestOfferOnJoinReachesNewcomer: the older peer offers the instant
// it learns of the newcomer (the rtc module's impolite side does
// exactly this), and the offer must reach the newcomer. The join is
// published only after the newcomer's socket is registered on the
// channel, so no signal addressed to it can be enqueued before it can
// receive.
func TestOfferOnJoinReachesNewcomer(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	defer a.close()

	// A reacts to the join before B's own snapshot read returns.
	offered := make(chan error, 1)
	joinSeq := make(chan uint64, 1)
	go func() {
		env, err := a.recvEnv(5 * time.Second)
		if err != nil {
			offered <- err
			return
		}
		if env.Type != "join" {
			offered <- errors.New("first envelope after A's snapshot is " + env.Type + ", want join")
			return
		}
		joinSeq <- env.Sequence
		a.send(map[string]any{"kind": "signal", "to": "pB", "type": "offer", "data": map[string]any{"sdp": "v=0 first-offer"}})
		offered <- nil
	}()

	b, snapB := join(t, base, "room1", "pB")
	defer b.close()
	if err := <-offered; err != nil {
		t.Fatal(err)
	}
	// The deterministic consequence of hydrating before announcing:
	// B's snapshot is read before the join bumps the room version, so
	// its sequence is strictly below the join's. Announcing first
	// makes them equal, and that is the order in which A's offer can
	// be enqueued ahead of B's registration and lost.
	if js := <-joinSeq; snapB.Sequence >= js {
		t.Fatalf("newcomer snapshot sequence %d is not below its join's %d: the join was published before Connect", snapB.Sequence, js)
	}
	env := b.expectEnv(t, "signal")
	var sig signalPayload
	if err := json.Unmarshal(env.Payload, &sig); err != nil || sig.From != "pA" || sig.Type != "offer" {
		t.Fatalf("newcomer got %s (%v), want A's first offer", env.Payload, err)
	}
}
