package a2a

import "fmt"

// JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	CodeTaskNotFound                   = -32001
	CodeTaskNotCancelable              = -32002
	CodePushNotificationNotSupported   = -32003
	CodeUnsupportedOperation           = -32004
	CodeContentTypeNotSupported        = -32005
	CodeInvalidAgentResponse           = -32006
	CodeExtendedAgentCardNotConfigured = -32007
	CodeExtensionSupportRequired       = -32008
	CodeVersionNotSupported            = -32009

	CodeUnauthenticated = -31401
	CodeUnauthorized    = -31403
)

// Error is a JSON-RPC error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("a2a: %s (code %d)", e.Message, e.Code)
}

// Errorf builds an Error with a formatted message.
func Errorf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ErrTaskNotFound is the refusal for an unknown task id — or, deliberately,
// for a task another owner holds: the two are indistinguishable to the
// caller, so task ids do not enumerate across owners.
func ErrTaskNotFound(id string) *Error {
	return Errorf(CodeTaskNotFound, "task %q not found", id)
}

// ErrTaskNotCancelable is the refusal for cancelling a task already in a
// terminal state.
func ErrTaskNotCancelable(id string, state TaskState) *Error {
	return Errorf(CodeTaskNotCancelable, "task %q is %s and cannot be canceled", id, state)
}

// ErrUnsupportedOperation is the refusal for an operation the task's state
// does not admit, such as a message to a terminal task.
func ErrUnsupportedOperation(msg string) *Error {
	return Errorf(CodeUnsupportedOperation, "%s", msg)
}

// ErrUnauthenticated is the refusal for a caller with no resolved identity.
func ErrUnauthenticated() *Error {
	return Errorf(CodeUnauthenticated, "authentication required")
}
