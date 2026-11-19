package moduleproto

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Frame-size and inflight limits.
const (
	DefaultMaxFrameBytes = 1 << 20

	scannerStartCap = 64 * 1024
	scannerMaxCap   = 4 * 1024 * 1024

	DefaultMaxInflight = 32
)

// ErrScannerOvercap marks a line past the scanner's 4 MiB structural cap.
var ErrScannerOvercap = errors.New("moduleproto: scanner buffer overflow")

// Codec is newline-delimited JSON framing over a reader and a writer.
// WriteFrame is safe for concurrent use; ReadFrame is for one goroutine.
type Codec struct {
	r    io.Reader
	w    io.Writer
	mu   sync.Mutex // guards Write and werr
	werr error      // set once a frame is cut short
	max  int        // negotiated max_frame_bytes; enforced on read + write

	scan *bufio.Scanner
}

// NewCodec builds a Codec capped at maxFrameBytes (0 = 1 MiB). It errors
// when the cap is above the 4 MiB scanner cap.
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
	s.Buffer(make([]byte, scannerStartCap), scannerMaxCap+2)
	return &Codec{r: r, w: w, max: maxFrameBytes, scan: s}, nil
}

// MaxFrameBytes returns the negotiated frame cap in bytes.
func (c *Codec) MaxFrameBytes() int { return c.max }

// WriteFrame writes f and a newline in one Write. A frame past the cap is
// not written and returns *OvercapError; after a partial write every later
// write returns that write's error.
func (c *Codec) WriteFrame(f *Frame) error {
	return c.writeFrame(f, nil)
}

func (c *Codec) writeFrame(f *Frame, locked func()) error {
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
	if locked != nil {
		locked()
	}
	if c.werr != nil {
		return c.werr
	}
	n, err := c.w.Write(append(body, '\n'))
	if err == nil && n != len(body)+1 {
		err = io.ErrShortWrite
	}
	if err != nil {
		err = fmt.Errorf("moduleproto: write: %w", err)
		if n > 0 {
			c.werr = err
		}
		return err
	}
	return nil
}

// ReadFrame reads the next frame: io.EOF at a clean end, *OvercapError past
// the cap, ErrInvalidFrame for a repeated or case-folded key.
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
		return nil, &OvercapError{Size: len(data), Cap: c.max}
	}
	var f Frame
	if err := handler.CheckObjectKeys(data, handler.FoldJSONKey); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidFrame, err.Error())
	}
	//gofastr:allow(GOFASTR1407) the CheckObjectKeys walk above refused duplicate and case-folded keys at every depth; this decode reads vetted bytes
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("moduleproto: decode: %w", err)
	}
	return &f, nil
}

// EncodeJSON marshals v, returning nil for nil.
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
