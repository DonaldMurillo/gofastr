package log

import (
	"errors"
	"io"
)

// Sink is a single log destination. Implementations must be safe for
// concurrent use from the fan-out handler; the handler serializes JSON
// per entry and then writes the full line to every sink.
//
// Write receives a single JSON-encoded entry without a trailing newline.
// Sinks that produce line-oriented output (files) should append '\n'.
// Sinks that batch (webhook) should treat each Write as one logical entry.
//
// Close is called once during App.Stop. It must flush in-flight data and
// return any final error. After Close, Write must not panic — it should
// return ErrSinkClosed and drop the entry.
type Sink interface {
	io.Closer
	Write(entry []byte) error
}

// ErrSinkClosed is returned by Sink.Write after Close has run. Callers
// can use errors.Is to distinguish post-shutdown drops from genuine IO
// failures.
var ErrSinkClosed = errors.New("log: sink closed")

// ErrSinkWedged is returned by Sink.Write when the sink has lost its
// underlying file (e.g. rotation succeeded the rename but reopen
// failed because a symlink was planted in the parent dir). Distinct
// from ErrSinkClosed so the fanout's stderr fallback emits a useful
// "wedged" message instead of confusing post-shutdown messaging.
var ErrSinkWedged = errors.New("log: sink wedged (rotation reopen failed)")
