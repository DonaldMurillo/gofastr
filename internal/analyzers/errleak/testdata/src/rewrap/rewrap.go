// Package rewrap reconstructs d01462e8a: a gate's own *RPCError rebuilt
// as a second invalid-params error from its text, so its code was lost.
package rewrap

import (
	"context"
	"errors"
)

const ErrInvalidParams = -32602

type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string { return e.Message }

type Response struct {
	ID    int
	Error *RPCError
}

func newErrorResponse(id int, code int, message string) Response {
	return Response{ID: id, Error: &RPCError{Code: code, Message: message}}
}

type Tool struct {
	Gate func(context.Context) error
}

type Server struct {
	serverGate func(context.Context) error
}

// checkToolGatePreFix is server.go before d01462e8a.
func checkToolGatePreFix(ctx context.Context, t Tool) error {
	if gErr := t.Gate(ctx); gErr != nil {
		return &RPCError{Code: ErrInvalidParams, Message: gErr.Error()} // want `rebuilds a \*RPCError from an error's text`
	}
	return nil
}

// checkServerGate returns the host gate's error as is.
func (s *Server) checkServerGate(ctx context.Context) error { // want checkServerGate:`foreignErr`
	gate := s.serverGate
	if gate == nil {
		return nil
	}
	return gate(ctx)
}

// handleRequestPreFix is protocol.go before d01462e8a: the helper
// builds the *RPCError.
func (s *Server) handleRequestPreFix(ctx context.Context, id int) Response {
	if err := s.checkServerGate(ctx); err != nil {
		return newErrorResponse(id, ErrInvalidParams, err.Error()) // want `rebuilds a \*RPCError from an error's text`
	}
	return Response{ID: id}
}

// refusalPreFix takes the error as a parameter: it may be anything.
func refusalPreFix(err error) *RPCError {
	return &RPCError{Code: ErrInvalidParams, Message: err.Error()} // want `rebuilds a \*RPCError from an error's text`
}

// ---- the fix and its spellings -------------------------------------------

func gateRefusal(err error) *RPCError {
	if rpcErr, ok := errors.AsType[*RPCError](err); ok {
		return rpcErr
	}
	return &RPCError{Code: ErrInvalidParams, Message: err.Error()}
}

func checkToolGate(ctx context.Context, t Tool) error {
	if gErr := t.Gate(ctx); gErr != nil {
		return gateRefusal(gErr)
	}
	return nil
}

func withAs(ctx context.Context, t Tool) error {
	if gErr := t.Gate(ctx); gErr != nil {
		var rpcErr *RPCError
		if errors.As(gErr, &rpcErr) {
			return rpcErr
		}
		return &RPCError{Code: ErrInvalidParams, Message: gErr.Error()}
	}
	return nil
}

func withAssertion(ctx context.Context, t Tool) error {
	if gErr := t.Gate(ctx); gErr != nil {
		if rpcErr, ok := gErr.(*RPCError); ok {
			return rpcErr
		}
		return &RPCError{Code: ErrInvalidParams, Message: gErr.Error()}
	}
	return nil
}

// ---- quiet ---------------------------------------------------------------

// A declared function that never returns a callback's error cannot hand
// back an *RPCError of someone else's.
func validate(name string) error {
	if name == "" {
		return errors.New("name required")
	}
	return nil
}

func validated(name string) error {
	if err := validate(name); err != nil {
		return &RPCError{Code: ErrInvalidParams, Message: err.Error()}
	}
	return nil
}

type preferenceError struct{ key string }

func (e *preferenceError) Error() string { return "bad preference " + e.key }

// A concrete error is what it is.
func concrete(pe *preferenceError) error {
	return &RPCError{Code: ErrInvalidParams, Message: pe.Error()}
}

// A local func literal is the function's own code, not a callback.
func ownLiteral(name string) error {
	check := func() error { return validate(name) }
	if err := check(); err != nil {
		return &RPCError{Code: ErrInvalidParams, Message: err.Error()}
	}
	return nil
}
