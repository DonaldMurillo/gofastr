package stream

import (
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// maskedClientFrame builds a single FIN client-to-server frame: opcode,
// 4-byte mask, then masked payload. Client frames MUST be masked per
// RFC 6455 §5.1, so conns with requireMask:true accept these.
func maskedClientFrame(opcode byte, payload []byte) []byte {
	mask := [4]byte{0xa, 0xb, 0xc, 0xd}
	out := []byte{0x80 | opcode, 0x80 | byte(len(payload))}
	out = append(out, mask[:]...)
	for i, b := range payload {
		out = append(out, b^mask[i%4])
	}
	return out
}

// readServerFrame reads one server-originated frame header from the peer
// side and returns its opcode (server frames are unmasked). Any payload
// bytes are drained and discarded. Used by the fake-peer helpers.
func readServerFrame(r interface{ Read([]byte) (int, error) }) (byte, error) {
	hdr := make([]byte, 2)
	if _, err := readFull(r, hdr); err != nil {
		return 0, err
	}
	opcode := hdr[0] & 0x0F
	length := int(hdr[1] & 0x7F)
	if length > 0 {
		if _, err := readFull(r, make([]byte, length)); err != nil {
			return 0, err
		}
	}
	return opcode, nil
}

// readFull reads exactly len(p) bytes from r.
func readFull(r interface{ Read([]byte) (int, error) }, p []byte) (int, error) {
	total := 0
	for total < len(p) {
		n, err := r.Read(p[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// pongAnsweringPeer reads frames the server conn writes onto the peer
// (cli) end and answers every Ping with a masked Pong. It counts
// answered pings in *answered and exits when stop is closed or the peer
// socket errors out.
func pongAnsweringPeer(cli net.Conn, answered *int32, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		cli.SetReadDeadline(time.Now().Add(15 * time.Millisecond))
		opcode, err := readServerFrame(cli)
		if err != nil {
			continue
		}
		if opcode == wsopcodePing {
			atomic.AddInt32(answered, 1)
			// Masked pong with no payload.
			_, _ = cli.Write([]byte{0x80 | wsopcodePong, 0x80 | 0, 0xa, 0xb, 0xc, 0xd})
		}
	}
}

// newReadPumpConn builds a conn over a REAL TCP socket pair (not net.Pipe)
// and starts every long-lived goroutine a real Upgrade starts: the write
// pump, the keepalive, and the read pump. TCP is used deliberately: its
// kernel send buffer lets a peer's writes (Pong, Close) complete even
// while the server side isn't reading — exactly the push-only scenario
// net.Pipe's synchronous semantics cannot model.
func newReadPumpConn(t *testing.T, cfg WSConfig) (*WebSocketConn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		ln.Close() // stop listening after one accept; must NOT race a blocked Accept.
		if err != nil {
			close(accepted)
			return
		}
		accepted <- c
	}()
	cli, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	srv := <-accepted
	if srv == nil {
		cli.Close()
		t.Fatal("accept failed")
	}
	sendBuf := cfg.SendBuffer
	if sendBuf == 0 {
		sendBuf = 32
	}
	conn := &WebSocketConn{
		conn:       srv,
		sendBuffer: make(chan []byte, sendBuf),
		closed:     make(chan struct{}),
		peerClosed: make(chan struct{}),
		readMsgs:   make(chan []byte, 8),
		readDone:   make(chan struct{}),
		config:     cfg,
	}
	conn.lastReadActivity.Store(time.Now().UnixNano())
	go conn.writePump()
	conn.startKeepalive()
	conn.startReadPump()
	return conn, cli
}

// TestPushOnlyConnSurvivesKeepalive pins the bug this change fixes: a
// server that only PUSHES (never calls Read) must not be force-closed by
// its own keepalive while the peer is healthy and answering Pongs.
//
// Before the internal read pump, readFrame ran only when the app called
// Read(), so the peer's Pongs were never processed and awaitingPong was
// never cleared — a healthy push-only conn died every ReadIdleTimeout +
// PongTimeout (80ms at these settings).
func TestPushOnlyConnSurvivesKeepalive(t *testing.T) {
	conn, cli := newReadPumpConn(t, WSConfig{
		ReadLimit:       1 << 20,
		ReadIdleTimeout: 40 * time.Millisecond,
		PongTimeout:     40 * time.Millisecond,
		WriteTimeout:    time.Second,
		CloseTimeout:    300 * time.Millisecond,
		requireMask:     true,
	})

	var answered int32
	stop := make(chan struct{})
	go pongAnsweringPeer(cli, &answered, stop)

	// The app NEVER calls Read — this is a push-only server.
	time.Sleep(300 * time.Millisecond)
	close(stop)

	select {
	case <-conn.Closed():
		t.Fatalf("push-only conn was closed by its own keepalive despite a healthy peer")
	default:
	}
	if atomic.LoadInt32(&answered) < 1 {
		t.Fatal("peer never answered a ping — keepalive did not fire a Ping as expected")
	}
	conn.Close()
}

// TestDeadPeerStillDetected pins that the read pump did not neuter the
// keepalive: a peer that never responds to Ping must still be detected
// and the connection closed within idle+pong plus slack.
func TestDeadPeerStillDetected(t *testing.T) {
	conn, cli := newReadPumpConn(t, WSConfig{
		ReadLimit:       1 << 20,
		ReadIdleTimeout: 40 * time.Millisecond,
		PongTimeout:     40 * time.Millisecond,
		WriteTimeout:    time.Second,
		CloseTimeout:    300 * time.Millisecond,
		requireMask:     true,
	})
	// Peer answers nothing and just drains so our Ping goes out.
	go func() { io.Copy(io.Discard, cli) }()

	select {
	case <-conn.Closed():
		// good — dead peer detected.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("dead peer was not detected: conn stayed open past idle+pong")
	}
}

// TestReadDrainsBufferedThenError pins the drain-before-error ordering:
// messages the pump decoded before the terminal error must be delivered
// to Read() before the error surfaces.
func TestReadDrainsBufferedThenError(t *testing.T) {
	conn, cli := newReadPumpConn(t, WSConfig{
		ReadLimit:       1 << 20,
		ReadIdleTimeout: -1, // disable keepalive; we drive the frames
		PongTimeout:     -1,
		WriteTimeout:    time.Second,
		CloseTimeout:    time.Second,
		requireMask:     true,
	})

	// Peer sends two data messages, then a Close.
	go func() {
		cli.Write(maskedClientFrame(wsopcodeText, []byte("first")))
		cli.Write(maskedClientFrame(wsopcodeText, []byte("second")))
		cli.Write(maskedClientFrame(wsopcodeClose, []byte{0x03, 0xE8})) // 1000
	}()

	first, err := conn.Read()
	if err != nil || string(first) != "first" {
		t.Fatalf("Read() = %q, %v; want %q, nil", first, err, "first")
	}
	second, err := conn.Read()
	if err != nil || string(second) != "second" {
		t.Fatalf("Read() = %q, %v; want %q, nil", second, err, "second")
	}
	_, err = conn.Read()
	if err == nil {
		t.Fatal("third Read() = nil error; want terminal error from peer Close")
	}
}

// TestCloseHandshakeFastWithoutReader pins that Close()'s closing
// handshake returns promptly when the peer answers with its reciprocal
// Close, even though the app never calls Read(). Before the read pump,
// nobody read the reciprocal Close, so awaitPeerClose always burned the
// full CloseTimeout.
func TestCloseHandshakeFastWithoutReader(t *testing.T) {
	conn, cli := newReadPumpConn(t, WSConfig{
		ReadLimit:       1 << 20,
		ReadIdleTimeout: -1,
		PongTimeout:     -1,
		WriteTimeout:    time.Second,
		CloseTimeout:    2 * time.Second,
		requireMask:     true,
	})

	// Peer reads our Close frame, then answers with its own masked Close.
	go func() {
		cli.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := readServerFrame(cli); err != nil {
			return
		}
		cli.Write(maskedClientFrame(wsopcodeClose, []byte{0x03, 0xE8}))
	}()

	start := time.Now()
	conn.Close()
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Close took %v with a reciprocating peer; want <500ms (CloseTimeout=2s)", elapsed)
	}
}
