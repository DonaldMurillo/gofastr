package log

import (
	"errors"
	"io"
)

// Sink is a single log destination.
type Sink interface {
	io.Closer
	Write(entry []byte) error
}

// ErrSinkClosed is returned by Sink.Write after Close has run.
var ErrSinkClosed = errors.New("log: sink closed")

// ErrSinkWedged is returned by Sink.Write when the sink has lost its
// underlying file (e.g. rotation succeeded the rename but reopen failed
// because a symlink was planted in the parent dir).
var ErrSinkWedged = errors.New("log: sink wedged (rotation reopen failed)")
