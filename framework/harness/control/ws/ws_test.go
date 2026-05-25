package ws

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }
func (fakeProvider) Chat(_ context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: "ws-hello"}
	ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
	close(ch)
	return ch, nil
}
func (fakeProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (fakeProvider) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

func setupServer(t *testing.T) (string, ids.SessionID, string, func()) {
	t.Helper()
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	reg := tool.NewRegistry()
	d := engine.NewDispatcher(bus, reg)
	eng := engine.NewEngine(session, bus, fakeProvider{}, "fake", d)
	mux := multiplex.New()
	mux.RegisterEngine(eng)

	secret, _ := auth.GenerateSecret()
	enc := auth.NewEncoder(secret)
	rl := auth.NewRevocationList()
	tok, _ := enc.Encode(auth.Claims{
		Sessions:      []ids.SessionID{session},
		IdentityClass: control.IdentityHuman,
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	})
	h := &Handler{Mux: mux, Encoder: enc, Revocations: rl}
	srv := httptest.NewServer(h)
	cleanup := func() {
		srv.Close()
		bus.Close()
	}
	return srv.URL, session, tok, cleanup
}

func TestWSHandshakeAndCommand(t *testing.T) {
	urlStr, session, tok, cleanup := setupServer(t)
	defer cleanup()

	// Rewrite http://… to a tcp dial; build WS handshake by hand.
	host := strings.TrimPrefix(urlStr, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	key := "dGhlIHNhbXBsZSBub25jZQ==" // RFC sample
	req := fmt.Sprintf("GET /?session=%s&token=%s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n\r\n",
		session, tok, host, key,
	)
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	// Read response headers.
	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	resp := string(buf[:n])
	if !strings.HasPrefix(resp, "HTTP/1.1 101") {
		t.Fatalf("handshake failed: %q", resp)
	}

	// Send a command frame.
	cmd := control.SendInput{
		SessionID: session,
		Content:   engine.SimpleInput("hello"),
	}
	body, _ := control.MarshalCommand(cmd)
	f, _ := json.Marshal(struct {
		Frame string          `json:"frame"`
		Body  json.RawMessage `json:"body"`
	}{Frame: "command", Body: body})
	if _, err := conn.Write(maskedFrame(f)); err != nil {
		t.Fatal(err)
	}

	// Read at least one inbound event frame.
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	got := readUntilTextDelta(t, conn)
	if !strings.Contains(got, "ws-hello") {
		t.Errorf("did not see TextDelta in WS stream:\n%s", got)
	}
}

func TestWSRejectsBadToken(t *testing.T) {
	urlStr, session, _, cleanup := setupServer(t)
	defer cleanup()
	host := strings.TrimPrefix(urlStr, "http://")
	conn, _ := net.Dial("tcp", host)
	defer conn.Close()

	req := fmt.Sprintf("GET /?session=%s&token=invalid HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n"+
		"Sec-WebSocket-Version: 13\r\n\r\n",
		session, host,
	)
	_, _ = conn.Write([]byte(req))
	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := conn.Read(buf)
	if !strings.HasPrefix(string(buf[:n]), "HTTP/1.1 401") {
		t.Errorf("expected 401, got: %q", string(buf[:n]))
	}
}

// maskedFrame builds a client-side text frame with a 4-byte mask
// (required by RFC 6455 for client→server frames).
func maskedFrame(payload []byte) []byte {
	mask := []byte{0xAB, 0xCD, 0xEF, 0x12}
	header := []byte{0x80 | 0x1} // FIN + text opcode
	switch {
	case len(payload) < 126:
		header = append(header, byte(len(payload))|0x80) // mask bit
	case len(payload) <= 0xFFFF:
		header = append(header, 126|0x80)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(len(payload)))
		header = append(header, ext[:]...)
	default:
		header = append(header, 127|0x80)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(len(payload)))
		header = append(header, ext[:]...)
	}
	header = append(header, mask...)
	body := make([]byte, len(payload))
	for i := range payload {
		body[i] = payload[i] ^ mask[i%4]
	}
	return append(header, body...)
}

func readUntilTextDelta(t *testing.T, conn net.Conn) string {
	t.Helper()
	var all []byte
	buf := make([]byte, 4096)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := conn.Read(buf)
		if n > 0 {
			all = append(all, buf[:n]...)
			if strings.Contains(string(all), "TextDelta") {
				return string(all)
			}
		}
		if err != nil {
			break
		}
	}
	return string(all)
}

// Compile-time guard against accidental removal of http import.
var _ = http.HandlerFunc(nil)
