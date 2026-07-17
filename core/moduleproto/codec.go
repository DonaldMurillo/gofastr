package moduleproto

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Codec cap constants (design §4.2). The scanner caps are the verbatim lift
// from mcpclient: 64 KiB initial buffer growing to a 4 MiB ceiling. The
// negotiated max_frame_bytes (default 1 MiB) is the protocol-level cap,
// enforced on BOTH read and write — over-cap is a terminal fault, never a
// truncation.
const (
	// DefaultMaxFrameBytes is the default negotiated max_frame_bytes (1 MiB),
	// mirroring core/mcp's maxMCPBodyBytes = 1<<20. A frame whose
	// JSON-encoded length exceeds this is rejected as terminal on both ends.
	DefaultMaxFrameBytes = 1 << 20

	// scannerStartCap is the bufio.Scanner's initial buffer (64 KiB).
	scannerStartCap = 64 * 1024
	// scannerMaxCap is the bufio.Scanner's structural maximum (4 MiB). A
	// line longer than this triggers bufio.ErrTooLong, which is terminal.
	// The negotiated max_frame_bytes MUST be ≤ scannerMaxCap — the codec
	// asserts this at construction.
	scannerMaxCap = 4 * 1024 * 1024

	// DefaultMaxInflight is the default per-peer cap on in-flight originated
	// requests (design §8: 32 leases/child). Over-cap is a clean
	// call-local error ([ErrInflightCap]), not a protocol fault.
	DefaultMaxInflight = 32
)

// ErrScannerOvercap wraps bufio.ErrTooLong so callers can distinguish a
// scanner-structural overflow from a negotiated-cap overflow via [OvercapError].
var ErrScannerOvercap = errors.New("moduleproto: scanner buffer overflow")

// Codec is the full-duplex newline-delimited JSON framing layer (design §4.2).
//
// It is transport-neutral: it speaks to an [io.Reader] and [io.Writer] pair,
// NOT specifically to os.Stdin / os.Stdout. The same Codec works for stdio
// (v1) and a future v2 socket transport — the wire format is identical, only
// the Reader/Writer change (design §1).
//
// Concurrency:
//
//   - Writes are mutex-guarded. Multiple goroutines may call [Codec.WriteFrame]
//     concurrently; each frame is emitted atomically as JSON + '\n'.
//   - Reads are NOT concurrency-safe: exactly one goroutine runs the read loop.
//     This is the [Peer]'s readLoop goroutine.
//
// Over-cap policy (design §4.2): a frame exceeding max_frame_bytes on read or
// write produces an [*OvercapError] and is terminal — the peer tears down. The
// codec NEVER truncates; truncation would silently corrupt the wire.
type Codec struct {
	r   io.Reader
	w   io.Writer
	mu  sync.Mutex // guards Write
	max int        // negotiated max_frame_bytes; enforced on read + write

	scan *bufio.Scanner
}

// NewCodec constructs a Codec over the given reader/writer pair. If
// maxFrameBytes is <= 0, [DefaultMaxFrameBytes] is used. If maxFrameBytes is
// greater than the structural scanner cap (4 MiB), construction fails — the
// scanner itself would reject such frames at the wrong layer, so we surface
// the configuration error early rather than at first frame.
func NewCodec(r io.Reader, w io.Writer, maxFrameBytes int) (*Codec, error) {
	if maxFrameBytes <= 0 {
		maxFrameBytes = DefaultMaxFrameBytes
	}
	if maxFrameBytes > scannerMaxCap {
		return nil, fmt.Errorf(
			"moduleproto: max_frame_bytes %d > scanner structural cap %d",
			maxFrameBytes, scannerMaxCap,
		)
	}
	s := bufio.NewScanner(r)
	// Verbatim lift from mcpclient: 64 KiB start, 4 MiB max.
	s.Buffer(make([]byte, scannerStartCap), scannerMaxCap)
	return &Codec{r: r, w: w, max: maxFrameBytes, scan: s}, nil
}

// MaxFrameBytes returns the negotiated frame cap in bytes.
func (c *Codec) MaxFrameBytes() int { return c.max }

// WriteFrame encodes f as JSON, enforces the negotiated cap, and writes the
// frame followed by a single '\n'. The write is mutex-guarded.
//
// On over-cap the frame is NOT written and [*OvercapError] is returned — the
// caller (the Peer) treats this as a terminal protocol fault.
func (c *Codec) WriteFrame(f *Frame) error {
	if f == nil {
		return fmt.Errorf("%w: nil frame", ErrInvalidFrame)
	}
	body, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("moduleproto: marshal: %w", err)
	}
	if len(body) > c.max {
		return &OvercapError{Size: len(body), Cap: c.max}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Append newline into a single Write so a peer never observes a partial
	// frame. bytes concat allocates, but stdio JSON frames are small and
	// bounded by max_frame_bytes; the allocation is the cost of atomicity.
	if _, err := c.w.Write(append(body, '\n')); err != nil {
		return fmt.Errorf("moduleproto: write: %w", err)
	}
	return nil
}

// ReadFrame reads the next newline-delimited frame. Returns [io.EOF] at a
// clean end-of-stream. Any decode error, scanner overflow, or negotiated-cap
// overflow is returned as a non-nil error that the Peer treats as terminal
// (with the exception of io.EOF, which may be graceful).
//
// ReadFrame must be called from a single goroutine (the read loop).
func (c *Codec) ReadFrame() (*Frame, error) {
	if !c.scan.Scan() {
		if err := c.scan.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				return nil, &OvercapError{Size: -1, Cap: c.max}
			}
			return nil, fmt.Errorf("moduleproto: scanner: %w", err)
		}
		return nil, io.EOF
	}
	data := c.scan.Bytes()
	if len(data) > c.max {
		// Structural scanner cap (4 MiB) allowed the line through, but the
		// negotiated protocol cap rejects it — still terminal.
		return nil, &OvercapError{Size: len(data), Cap: c.max}
	}
	var f Frame
	// Decode into a copy of the bytes: json.Unmarshal will allocate for any
	// json.RawMessage fields, so the Frame is safe across subsequent scans.
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("moduleproto: decode: %w", err)
	}
	return &f, nil
}

// EncodeJSON is a small helper for tests and for peers that need to encode a
// typed value as json.RawMessage for Frame.Params. It enforces nothing about
// size — callers composing raw Frames should check len against [Codec.MaxFrameBytes].
func EncodeJSON(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}
