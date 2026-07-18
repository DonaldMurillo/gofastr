package moduleproto

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Sentinel errors. These describe protocol-fatal or call-local conditions.
// Terminal protocol faults (over-cap, unsolicited response, malformed frame)
// tear down the Peer; the supervisor maps them to a restart-vs-Failed
// classification (design §8).
var (
	// ErrClosed is returned by a Peer method after [Peer.Close] or once the
	// read loop has exited for any reason.
	ErrClosed = errors.New("moduleproto: peer closed")

	// ErrInflightCap is returned by [Peer.Call] when the peer is already
	// servicing the maximum number of in-flight originated requests. It is a
	// call-local error, not a protocol fault.
	ErrInflightCap = errors.New("moduleproto: inflight cap reached")

	// ErrInvalidFrame wraps any frame-level validation failure surfaced from
	// [Frame.UnmarshalJSON]. The underlying error carries the detail.
	ErrInvalidFrame = errors.New("moduleproto: invalid frame")

	// ErrNegotiation is returned by [Negotiate] when the two peers' proto
	// ranges do not intersect above the joint floor.
	ErrNegotiation = errors.New("moduleproto: proto negotiation failed")

	// ErrCriticalFeature is returned when a peer declares a critical feature
	// the other endpoint does not support.
	ErrCriticalFeature = errors.New("moduleproto: unsupported critical feature")
)

// Standard JSON-RPC 2.0 error codes (the -32xxx reserved band), plus
// moduleproto-specific codes in the implementation-defined range.
const (
	// CodeParseError is JSON-RPC's -32700: invalid JSON was received.
	CodeParseError = -32700
	// CodeInvalidRequest is JSON-RPC's -32600: the JSON is not a valid Request.
	CodeInvalidRequest = -32600
	// CodeMethodNotFound is JSON-RPC's -32601.
	CodeMethodNotFound = -32601
	// CodeInvalidParams is JSON-RPC's -32602.
	CodeInvalidParams = -32602
	// CodeInternalError is JSON-RPC's -32603.
	CodeInternalError = -32603

	// CodeOvercap is moduleproto-specific: a frame exceeded the negotiated
	// max_frame_bytes on read or write. It is a terminal protocol fault; the
	// peer is torn down. The implementation-defined error band is -32099 to
	// -32000 per JSON-RPC 2.0.
	CodeOvercap = -32099
	// CodeInflightCap indicates the originating peer's in-flight cap is
	// reached; the caller should retry or shed load. Not terminal.
	CodeInflightCap = -32098
	// CodeHandshakeMismatch indicates a digest or identity mismatch during
	// module.handshake. Terminal — a restart cannot fix a bad artifact.
	CodeHandshakeMismatch = -32097
	// CodeCapabilityDenied indicates the host's capability broker refused a
	// reverse host.* call: the module-grant pre-filter (access.ScopeMatch),
	// the CrossOwnerRead carve-out, the delegation-handle lookup, or the
	// re-dispatch caller-authority gate (401/403 from the CRUD chokepoint)
	// failed closed. NOT a protocol fault — the connection stays up; the
	// child receives a per-call denial it must surface in its own response.
	// (design #37 §5; the trust boundary.)
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

// Is allows errors.Is(err, ErrInvalidFrame) etc. to match a wire *Error whose
// Code corresponds to a known sentinel category. Callers usually want the
// concrete *Error, so this only bridges the module-specific protocol codes.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrInflightCap:
		return e.Code == CodeInflightCap
	}
	return false
}

// AsError unwraps a generic error to a *Error if the underlying value is a
// wire error (already *Error) or nil otherwise. Convenience for callers that
// want the structured code/message pair.
func AsError(err error) *Error {
	var we *Error
	if errors.As(err, &we) {
		return we
	}
	return nil
}

// OvercapError is returned by the codec when a frame exceeds the negotiated
// max_frame_bytes on read or write. It is terminal: by design §4.2 the peer
// never truncates, so the connection is unusable.
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

// Is makes OvercapError comparable for sentinel checks.
func (e *OvercapError) Is(target error) bool {
	_, ok := target.(*OvercapError)
	return ok
}

// HandshakeMismatchError is returned by [Handshake] when the child's echoed
// identity or digests do not round-trip the caller-supplied expected values,
// or when proto negotiation fails. It is terminal per design §4.5/§4.7.
type HandshakeMismatchError struct {
	Field string // human-readable name of the mismatched field
	Want  string // caller-supplied expected value
	Got   string // child-echoed value
}

func (e *HandshakeMismatchError) Error() string {
	return fmt.Sprintf("moduleproto: handshake mismatch: %s want=%q got=%q", e.Field, e.Want, e.Got)
}
