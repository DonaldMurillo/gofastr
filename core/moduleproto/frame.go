package moduleproto

import (
	"encoding/json"
	"fmt"
)

// Frame is the single bidirectional envelope used by both endpoints of a
// moduleproto connection (design §4.3). One Frame type carries requests,
// notifications, and responses — discrimination happens in the read loop, not
// the type system.
//
// Wire shape (JSON-RPC 2.0):
//
//	{
//	  "jsonrpc": "2.0",            // required; UnmarshalJSON rejects anything else
//	  "id":     <uint64>,          // present ⇒ request or response; absent ⇒ notification
//	  "method": "<name>",          // present ⇒ REQUEST (or notification); absent ⇒ RESPONSE
//	  "params": <raw>,             // requests / notifications only
//	  "result": <raw>,             // success responses only
//	  "error":  {code,message,data?}
//	}
//
// ID semantics are load-bearing:
//
//   - id is a *uint64. nil ⇒ omit the key entirely (notification). A non-nil
//     pointer ⇒ marshal the pointed-to value. This is NOT mcpclient's
//     `uint64 \`json:"id,omitempty"\“ shape: that form re-purposes 0 as the
//     notification sentinel and erases a legitimately-issued id:0. Here id:0
//     is impossible: the per-direction id counter starts at 1, and
//     UnmarshalJSON rejects a decoded id:0 as malformed.
//
//   - IDs are per-direction (design §4.3): each endpoint owns an independent
//     counter. Host-originated id:7 and child-originated id:7 never collide —
//     see the [Peer] read loop and peer_test.go's interleaved test.
type Frame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// HasID reports whether the Frame carries an id (i.e. it is a request or a
// response, not a notification).
func (f *Frame) HasID() bool { return f != nil && f.ID != nil }

// IDValue returns the id, or 0 if absent. The 0 return for "absent" is NOT a
// valid id — it is a convenience for callers that have already checked HasID.
// A decoded Frame never holds id:0 (UnmarshalJSON rejects it), so a 0 return
// unambiguously means "absent" for any Frame obtained via the codec.
func (f *Frame) IDValue() uint64 {
	if f == nil || f.ID == nil {
		return 0
	}
	return *f.ID
}

// IsRequest reports whether the Frame is a request (method present, id present).
func (f *Frame) IsRequest() bool { return f != nil && f.Method != "" && f.ID != nil }

// IsNotification reports whether the Frame is a notification (method present, no id).
func (f *Frame) IsNotification() bool { return f != nil && f.Method != "" && f.ID == nil }

// IsResponse reports whether the Frame is a response (no method, id present).
func (f *Frame) IsResponse() bool { return f != nil && f.Method == "" && f.ID != nil }

// IsSuccess reports whether the Frame is a successful response (no method, no error).
func (f *Frame) IsSuccess() bool { return f != nil && f.Method == "" && f.ID != nil && f.Error == nil }

// IsError reports whether the Frame is an error response.
func (f *Frame) IsError() bool { return f != nil && f.Error != nil }

// NewRequest constructs a request Frame with the given id. The id must be > 0;
// callers normally obtain it from a Peer's monotonic counter.
func NewRequest(id uint64, method string, params json.RawMessage) *Frame {
	idCopy := id
	return &Frame{
		JSONRPC: "2.0",
		ID:      &idCopy,
		Method:  method,
		Params:  params,
	}
}

// NewNotification constructs a notification Frame (method present, no id).
func NewNotification(method string, params json.RawMessage) *Frame {
	return &Frame{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
}

// NewSuccessResponse constructs a success response paired to the given id.
func NewSuccessResponse(id uint64, result json.RawMessage) *Frame {
	idCopy := id
	return &Frame{
		JSONRPC: "2.0",
		ID:      &idCopy,
		Result:  result,
	}
}

// NewErrorResponse constructs an error response paired to the given id.
func NewErrorResponse(id uint64, code int, message string, data json.RawMessage) *Frame {
	idCopy := id
	return &Frame{
		JSONRPC: "2.0",
		ID:      &idCopy,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// UnmarshalJSON validates the wire invariants after default struct decoding.
// It enforces:
//
//   - jsonrpc must be exactly "2.0".
//   - id, if present, must be > 0. id:0 is impossible under this protocol and
//     is rejected to prevent the mcpclient-style zero-sentinel misroute.
//   - A frame with a method must NOT also carry result or error (a request
//     that looks like a response is malformed).
//   - A frame without a method MUST carry an id and either result or error
//     (responses are paired to a specific request).
//   - A frame with neither method nor id is an empty envelope and is rejected.
//
// Discrimination of request-vs-response is intentionally NOT enforced here:
// the read loop does it, because the same wire bytes are valid for both roles
// at the type level. The validation here is purely wire-level sanity.
func (f *Frame) UnmarshalJSON(data []byte) error {
	type raw Frame // alias to avoid recursion on json.Unmarshal
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidFrame, err.Error())
	}
	if r.JSONRPC != "2.0" {
		return fmt.Errorf("%w: jsonrpc must be \"2.0\", got %q", ErrInvalidFrame, r.JSONRPC)
	}
	if r.ID != nil && *r.ID == 0 {
		return fmt.Errorf("%w: id must be > 0 (id:0 is not a valid sentinel)", ErrInvalidFrame)
	}
	hasMethod := r.Method != ""
	hasResult := len(r.Result) > 0
	hasError := r.Error != nil
	hasID := r.ID != nil

	if hasMethod && (hasResult || hasError) {
		return fmt.Errorf("%w: method frame must not carry result/error", ErrInvalidFrame)
	}
	if !hasMethod {
		// Must be a response.
		if !hasID {
			return fmt.Errorf("%w: response missing id", ErrInvalidFrame)
		}
		if !hasResult && !hasError {
			return fmt.Errorf("%w: response missing result/error", ErrInvalidFrame)
		}
	}
	if !hasMethod && !hasID {
		// Empty envelope — neither request nor response nor notification.
		return fmt.Errorf("%w: frame has no method and no id", ErrInvalidFrame)
	}
	*f = Frame(r)
	return nil
}

// MarshalJSON is the default struct marshal. The struct tags already produce
// the correct wire shape (id omitted when nil via omitempty on a pointer). It
// is defined explicitly to lock the shape and so that future tightening (e.g.
// rejecting a Frame built with method+result) can land here without changing
// call sites.
func (f Frame) MarshalJSON() ([]byte, error) {
	type raw Frame
	return json.Marshal(raw(f))
}
