package moduleproto

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Frame is the JSON-RPC 2.0 envelope: a request (method, id), notification
// (method) or response (id, result or error). A nil ID omits the key.
type Frame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// HasID reports whether the Frame carries an id.
func (f *Frame) HasID() bool { return f != nil && f.ID != nil }

// IDValue returns the id, or 0 when there is none.
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

// NewRequest returns a request Frame; id must be above 0.
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
	if len(result) == 0 {
		result = json.RawMessage("null")
	}
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

// UnmarshalJSON decodes a frame and returns ErrInvalidFrame unless jsonrpc
// is "2.0", an id is absent or above 0, a method frame carries no result
// or error, and a response has an id and exactly one of result or error.
func (f *Frame) UnmarshalJSON(data []byte) error {
	type raw Frame // alias to avoid recursion on json.Unmarshal
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidFrame, err.Error())
	}
	if r.JSONRPC != "2.0" {
		return fmt.Errorf("%w: jsonrpc must be \"2.0\", got %q", ErrInvalidFrame, r.JSONRPC)
	}
	var fields struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidFrame, err)
	}
	if bytes.Equal(bytes.TrimSpace(fields.ID), []byte("null")) {
		return fmt.Errorf("%w: id must be omitted or a positive integer", ErrInvalidFrame)
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
		if !hasID {
			return fmt.Errorf("%w: response missing id", ErrInvalidFrame)
		}
		if hasResult && hasError {
			return fmt.Errorf("%w: response carries both result and error", ErrInvalidFrame)
		}
		if !hasResult && !hasError {
			return fmt.Errorf("%w: response missing result/error", ErrInvalidFrame)
		}
	}
	*f = Frame(r)
	return nil
}

// MarshalJSON encodes the Frame's wire shape.
func (f Frame) MarshalJSON() ([]byte, error) {
	type raw Frame
	return json.Marshal(raw(f))
}
