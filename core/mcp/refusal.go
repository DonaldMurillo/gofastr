package mcp

import "errors"

// shownRefusal carries the ShowRefusal marker: a gate refusal whose
// message is meant to reach the caller.
type shownRefusal struct{ err error }

func (e shownRefusal) Error() string { return e.err.Error() }
func (e shownRefusal) Unwrap() error { return e.err }

// ShowRefusal marks a gate error as one whose message may cross to the
// caller. By default a gate's refusal on a direct call
// (tools/call, resources/read, prompts/get) answers exactly as an
// unknown name does, so the call cannot confirm a gated item exists.
// Wrap the error a gate wants the caller to read — an "upgrade your
// plan" refusal — and its code and message cross as a plain refusal
// always did: a plain error answers invalid-params carrying its text, a
// gate that returns its own *RPCError keeps its code and message.
func ShowRefusal(err error) error {
	if err == nil {
		return nil
	}
	return shownRefusal{err}
}

// refusalShown reports whether err, or anything it wraps, carries the
// ShowRefusal marker.
func refusalShown(err error) bool {
	var marked shownRefusal
	return errors.As(err, &marked)
}

// gateRefusalError carries a gate's refusal out of a read path that runs
// app code under a recover guard (readResourceContents), so the dispatch
// can answer it with maskedRefusalRPC instead of classifying it as an
// internal contents failure.
type gateRefusalError struct{ err error }

func (e *gateRefusalError) Error() string { return e.err.Error() }
func (e *gateRefusalError) Unwrap() error { return e.err }

// errGatedHidden is the sentinel a Gated-wrapped handler returns for an
// unmarked refusal; callTool maps it to the unknown-tool error so a
// Gated tool answers exactly as an unregistered name.
var errGatedHidden = errors.New("mcp: gated item hidden")

// maskedRefusalRPC answers a gate's refusal for an item the caller named.
// A refusal marked with ShowRefusal crosses with its own code and message
// (gateRefusal converts it); any other refusal answers notFound verbatim,
// the exact error an unknown name answers with, so a direct call is not
// an existence oracle: the listings already hide a gated item, and the
// call must not confirm it.
func maskedRefusalRPC(err error, notFound *RPCError) *RPCError {
	if refusalShown(err) {
		return gateRefusal(err)
	}
	return notFound
}

// errGatePanicked is the unmarked refusal a recovered gate panic leaves
// behind: it masks exactly like a plain gate refusal.
var errGatePanicked = errors.New("mcp: gate panicked")
