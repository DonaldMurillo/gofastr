package stream

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

type handshakeDeadlineConn struct {
	net.Conn
	deadlines []time.Time
	failAt    int
}

func (conn *handshakeDeadlineConn) SetWriteDeadline(deadline time.Time) error {
	conn.deadlines = append(conn.deadlines, deadline)
	if len(conn.deadlines) == conn.failAt {
		return errors.New("deadline unavailable")
	}
	return conn.Conn.SetWriteDeadline(deadline)
}

func TestUpgradeAlwaysBoundsHandshake(t *testing.T) {
	for _, timeout := range []time.Duration{0, -1} {
		server, client := net.Pipe()
		tracked := &handshakeDeadlineConn{Conn: server}
		go func() { _, _ = io.Copy(io.Discard, client) }()
		conn, err := Upgrade(handshakeWriter{ResponseRecorder: httptest.NewRecorder(), conn: tracked}, validUpgradeReq(), WSConfig{WriteTimeout: timeout, ReadIdleTimeout: -1})
		if err != nil {
			t.Fatal(err)
		}
		if len(tracked.deadlines) != 2 || tracked.deadlines[0].IsZero() || !tracked.deadlines[1].IsZero() {
			t.Errorf("handshake deadlines = %v", tracked.deadlines)
		}
		_ = conn.Close()
		_ = client.Close()
	}
}

func TestUpgradeRefusesDeadlineFailure(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		server, client := net.Pipe()
		tracked := &handshakeDeadlineConn{Conn: server, failAt: failAt}
		go func() { _, _ = io.Copy(io.Discard, client) }()
		conn, err := Upgrade(handshakeWriter{ResponseRecorder: httptest.NewRecorder(), conn: tracked}, validUpgradeReq(), WSConfig{})
		if err == nil || conn != nil {
			t.Errorf("deadline failure accepted: %v", err)
		}
		if _, err := server.Write([]byte("closed")); !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("hijacked socket not closed: %v", err)
		}
		_ = server.Close()
		_ = client.Close()
	}
}

type handshakeWriter struct {
	*httptest.ResponseRecorder
	conn   net.Conn
	buffer *bufio.ReadWriter
}

func (w handshakeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, w.buffer, nil
}

func TestUpgradeBoundsHandshakeWrite(t *testing.T) {
	for _, buffered := range []bool{false, true} {
		t.Run(map[bool]string{false: "response", true: "flush"}[buffered], func(t *testing.T) {
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()
			writer := handshakeWriter{ResponseRecorder: httptest.NewRecorder(), conn: server}
			if buffered {
				writer.buffer = bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server))
				_, _ = writer.buffer.WriteString("pending")
			}
			done := make(chan error, 1)
			go func() {
				_, err := Upgrade(writer, validUpgradeReq(), WSConfig{WriteTimeout: 30 * time.Millisecond})
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("stalled upgrade succeeded")
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("stalled upgrade write has no deadline")
			}
		})
	}
}

func TestUpgradeClearsHandshakeDeadline(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	go func() { _, _ = io.Copy(io.Discard, client) }()
	conn, err := Upgrade(handshakeWriter{ResponseRecorder: httptest.NewRecorder(), conn: server}, validUpgradeReq(), WSConfig{WriteTimeout: 20 * time.Millisecond, ReadIdleTimeout: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	time.Sleep(40 * time.Millisecond)
	if _, err := server.Write([]byte("after handshake")); err != nil {
		t.Fatalf("handshake deadline survived: %v", err)
	}
}
