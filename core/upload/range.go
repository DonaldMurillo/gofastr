package upload

import (
	"context"
	"io"
)

// RangeGetter is an optional capability a [Storage] backend may implement to
// expose seekable reads.
//
// [Storage.Get] returns an io.ReadCloser, which erases seekability, and
// [http.ServeContent] needs an io.ReadSeeker to answer a `Range:` request.
// Without this, a client that loses its connection 1.8 GB into a 2 GB download
// restarts from zero, and browsers/CDNs that probe with a range request get a
// 200 with the whole body instead of a 206.
//
// It is a capability interface rather than a widening of Storage on purpose:
// a network-backed store would have to buffer the whole object to satisfy
// Seek, which is worse than declining. Callers type-assert and fall back —
// [ServeHandler] is the reference consumer.
//
// An implementation MUST apply the same key validation as its Get: a
// capability that skipped the traversal check would be a path-traversal hole
// with a performance justification.
type RangeGetter interface {
	GetRange(ctx context.Context, key string) (io.ReadSeekCloser, error)
}
