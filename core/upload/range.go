package upload

import (
	"context"
	"io"
)

// RangeGetter is the optional Storage capability ServeHandler uses to
// answer Range requests. GetRange validates keys exactly as Get does.
type RangeGetter interface {
	GetRange(ctx context.Context, key string) (io.ReadSeekCloser, error)
}
