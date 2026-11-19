package moduleproto

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Sentinel errors. A terminal fault (over-cap, unsolicited response,
// malformed frame, stalled transport) ends the Peer.
var (
	// ErrClosed is returned after Close or once the read loop has exited.
	ErrClosed = errors.New("moduleproto: peer closed")
	// ErrInflightCap is returned by Call or Notify at the inflight cap; not terminal.
	ErrInflightCap = errors.New("moduleproto: inflight cap reached")
	// ErrInvalidFrame wraps a frame-level validation failure.
	ErrInvalidFrame = errors.New("moduleproto: invalid frame")
	// ErrNegotiation is returned when two proto ranges do not intersect.
	ErrNegotiation = errors.New("moduleproto: proto negotiation failed")
	// ErrCriticalFeature is returned for a critical feature the other side lacks.
	ErrCriticalFeature = errors.New("moduleproto: unsupported critical feature")
	// ErrHandshakeUnanswered wraps a module.handshake that got no reply at all.
	ErrHandshakeUnanswered = errors.New("moduleproto: handshake unanswered")
	// ErrStalled is the terminal fault for a counterparty that stopped reading.
	ErrStalled = errors.New("moduleproto: transport stalled: the counterparty stopped reading")
)

// JSON-RPC 2.0 error codes, then moduleproto's own in -32099..-32000.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	// CodeOvercap: a frame passed max_frame_bytes; terminal.
	CodeOvercap = -32099
	// CodeInflightCap: the serving peer is at its cap; retry or shed load.
	CodeInflightCap = -32098
	// CodeHandshakeMismatch: a digest or identity did not round-trip; terminal.
	CodeHandshakeMismatch = -32097
	// CodeCapabilityDenied: the host broker refused a host.* call; not terminal.
	CodeCapabilityDenied = -32096
)

// Error is the JSON-RPC 2.0 error object carried in [Frame.Error].
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("moduleproto: rpc error %d: %s", e.Code, e.Message)
}

// Is reports whether target is ErrInflightCap and e carries CodeInflightCap.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrInflightCap:
		return e.Code == CodeInflightCap
	}
	return false
}

// AsError returns the *Error in err's chain, or nil.
func AsError(err error) *Error {
	if we, ok := errors.AsType[*Error](err); ok {
		return we
	}
	return nil
}

// OvercapError reports a frame past max_frame_bytes on read or write; it
// is terminal, since the codec never truncates.
type OvercapError struct {
	Size int // bytes observed; -1 if unknown (e.g. bufio.Scanner ErrTooLong)
	Cap  int // negotiated cap in bytes
}

func (e *OvercapError) Error() string {
	if e.Size < 0 {
		return fmt.Sprintf("moduleproto: frame exceeded scanner cap (%d bytes)", e.Cap)
	}
	return fmt.Sprintf("moduleproto: frame %d bytes > cap %d bytes", e.Size, e.Cap)
}

// Is matches any *OvercapError.
func (e *OvercapError) Is(target error) bool {
	_, ok := target.(*OvercapError)
	return ok
}

// HandshakeMismatchError reports a handshake field the child did not echo
// back exactly; it is terminal.
type HandshakeMismatchError struct {
	Field string // human-readable name of the mismatched field
	Want  string // caller-supplied expected value
	Got   string // child-echoed value
}

func (e *HandshakeMismatchError) Error() string {
	return fmt.Sprintf("moduleproto: handshake mismatch: %s want=%q got=%q", e.Field, e.Want, e.Got)
}
