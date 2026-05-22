package uihost

import (
	"strings"
	"sync"
)

// builderPool caches strings.Builder instances to reduce allocations on
// the page-render hot path. Each request borrows a builder, uses it for
// HTML assembly, then returns it.
var builderPool = sync.Pool{
	New: func() any {
		return new(strings.Builder)
	},
}

// borrowBuilder gets a strings.Builder from the pool, reset to zero length.
func borrowBuilder() *strings.Builder {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	return b
}

// returnBuilder returns a builder to the pool.
func returnBuilder(b *strings.Builder) {
	// Prevent retaining huge buffers — if the builder grew beyond 64KB,
	// let it GC and return a fresh one next time.
	if b.Cap() > 64*1024 {
		return
	}
	builderPool.Put(b)
}
