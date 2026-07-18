package moduleproto

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

// TestCodecWriteReadRoundTrip: a frame written then read yields back an
// equivalent frame.
func TestCodecWriteReadRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	c, err := NewCodec(&buf, &buf, 0)
	if err != nil {
		t.Fatal(err)
	}
	id := uint64(42)
	in := NewRequest(id, "module.health", json.RawMessage(`{"verbose":true}`))
	if err := c.WriteFrame(in); err != nil {
		t.Fatal(err)
	}
	out, err := c.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsRequest() {
		t.Fatalf("not a request: %+v", out)
	}
	if out.IDValue() != id {
		t.Fatalf("id mismatch: want %d got %d", id, out.IDValue())
	}
	if out.Method != "module.health" {
		t.Fatalf("method mismatch: %s", out.Method)
	}
}

// TestCodecWriteOvercapTerminal: writing a frame larger than max_frame_bytes
// is terminal (design §4.2). It returns *OvercapError and does not write.
func TestCodecWriteOvercapTerminal(t *testing.T) {
	var buf bytes.Buffer
	c, err := NewCodec(&buf, &buf, 32)
	if err != nil {
		t.Fatal(err)
	}
	// Build a frame whose JSON exceeds 32 bytes.
	bigParams := json.RawMessage(`"` + strings.Repeat("x", 64) + `"`)
	in := NewRequest(1, "module.health", bigParams)
	err = c.WriteFrame(in)
	if err == nil {
		t.Fatalf("expected overcap error")
	}
	var oe *OvercapError
	if !errors.As(err, &oe) {
		t.Fatalf("expected *OvercapError, got %T: %v", err, err)
	}
	if oe.Cap != 32 {
		t.Fatalf("cap mismatch: %d", oe.Cap)
	}
	if buf.Len() != 0 {
		t.Fatalf("overcap frame must not be written; buf has %d bytes", buf.Len())
	}
}

// TestCodecReadOvercapTerminal: a frame larger than negotiated cap on the read
// side is terminal too (not just the structural scanner cap).
func TestCodecReadOvercapTerminal(t *testing.T) {
	// Use a default-cap codec to WRITE (so it fits in 1 MiB), then READ on a
	// tighter-cap codec that rejects the same bytes.
	var buf bytes.Buffer
	writeC, _ := NewCodec(&buf, &buf, 0)
	bigParams := json.RawMessage(`"` + strings.Repeat("y", 512) + `"`)
	if err := writeC.WriteFrame(NewRequest(1, "x", bigParams)); err != nil {
		t.Fatal(err)
	}
	readC, err := NewCodec(&buf, nil, 64) // tighter than the frame
	if err != nil {
		t.Fatal(err)
	}
	_, err = readC.ReadFrame()
	if err == nil {
		t.Fatalf("expected read overcap")
	}
	var oe *OvercapError
	if !errors.As(err, &oe) {
		t.Fatalf("expected *OvercapError, got %T: %v", err, err)
	}
}

// TestCodecRejectsOverScannerCap: construction fails when max_frame_bytes
// exceeds the scanner structural cap (4 MiB). This prevents a misconfig where
// the scanner would reject at the wrong layer.
func TestCodecRejectsOverScannerCap(t *testing.T) {
	_, err := NewCodec(&bytes.Buffer{}, &bytes.Buffer{}, scannerMaxCap+1)
	if err == nil {
		t.Fatalf("expected construction failure")
	}
}

// TestCodecEnforcesJSONRPCRequired: a frame missing jsonrpc fails at decode.
func TestCodecEnforcesJSONRPCRequired(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString(`{"id":1,"method":"x"}` + "\n")
	c, _ := NewCodec(&buf, &buf, 0)
	_, err := c.ReadFrame()
	if err == nil {
		t.Fatalf("expected decode error for missing jsonrpc")
	}
	if !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("not ErrInvalidFrame: %v", err)
	}
}

// TestCodecConcurrentWrites: parallel WriteFrame calls produce parseable
// frames — the mutex serializes them, no interleaving.
func TestCodecConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	c, _ := NewCodec(&buf, &buf, 0)
	const N = 64
	var wg sync.WaitGroup
	for i := 1; i <= N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := uint64(i)
			if err := c.WriteFrame(&Frame{
				JSONRPC: "2.0", ID: &id, Method: "ping",
			}); err != nil {
				t.Errorf("write %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	// Every line must parse as a Frame.
	r := bytes.NewReader(buf.Bytes())
	counter, _ := NewCodec(r, io.Discard, 0)
	seen := 0
	for {
		f, err := counter.ReadFrame()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode failed at frame %d: %v", seen, err)
		}
		seen++
		_ = f
	}
	if seen != N {
		t.Fatalf("decoded %d frames, expected %d", seen, N)
	}
}

// TestCodecScannerTooLongTerminal: a single line longer than the 4 MiB
// scanner cap produces a terminal *OvercapError (wrapping bufio.ErrTooLong).
func TestCodecScannerTooLongTerminal(t *testing.T) {
	var buf bytes.Buffer
	// 5 MiB single line: scanner rejects at its own cap before we check
	// the negotiated cap. Default negotiated cap (1 MiB) is also exceeded,
	// but the scanner error fires first.
	buf.WriteString(`{"jsonrpc":"2.0","id":1,"method":"x","params":"`)
	buf.Write(bytes.Repeat([]byte("z"), 5*1024*1024))
	buf.WriteString(`"}` + "\n")
	c, _ := NewCodec(&buf, &bytes.Buffer{}, 0)
	_, err := c.ReadFrame()
	if err == nil {
		t.Fatalf("expected scanner-overflow error")
	}
	var oe *OvercapError
	if !errors.As(err, &oe) {
		t.Fatalf("expected *OvercapError, got %T: %v", err, err)
	}
	if oe.Size != -1 {
		t.Fatalf("scanner overflow should report Size=-1, got %d", oe.Size)
	}
}

// TestCodecNotificationRoundTrip: a notification (no id) survives a round trip.
func TestCodecNotificationRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	c, _ := NewCodec(&buf, &buf, 0)
	in := NewNotification("module.cancel", json.RawMessage(`{"request_id":"7"}`))
	if err := c.WriteFrame(in); err != nil {
		t.Fatal(err)
	}
	out, err := c.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsNotification() {
		t.Fatalf("not a notification: %+v", out)
	}
}
