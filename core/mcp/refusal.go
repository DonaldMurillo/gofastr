package mcp

import "errors"

type shownRefusal struct{ err error }

func (e shownRefusal) Error() string { return e.err.Error() }
func (e shownRefusal) Unwrap() error { return e.err }

// ShowRefusal marks a gate error whose code and message may reach the caller instead of the unknown-name answer; it returns nil for nil.
func ShowRefusal(err error) error {
	if err == nil {
		return nil
	}
	return shownRefusal{err}
}

func refusalShown(err error) bool {
	var marked shownRefusal
	return errors.As(err, &marked)
}

type gateRefusalError struct{ err error }

func (e *gateRefusalError) Error() string { return e.err.Error() }
func (e *gateRefusalError) Unwrap() error { return e.err }

var errGatedHidden = errors.New("mcp: gated item hidden")

func maskedRefusalRPC(err error, notFound *RPCError) *RPCError {
	if refusalShown(err) {
		return gateRefusal(err)
	}
	return notFound
}

var errGatePanicked = errors.New("mcp: gate panicked")
