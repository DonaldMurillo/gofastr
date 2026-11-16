package uihost

import (
	"strings"
	"sync"
)

var builderPool = sync.Pool{
	New: func() any {
		return new(strings.Builder)
	},
}

func borrowBuilder() *strings.Builder {
	return GetBuilder()
}

func returnBuilder(b *strings.Builder) {
	PutBuilder(b)
}

// GetBuilder returns an empty strings.Builder from the page-render pool.
func GetBuilder() *strings.Builder {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	return b
}

// PutBuilder returns b to the pool unless it grew past 64 KiB.
func PutBuilder(b *strings.Builder) {
	if b.Cap() > 64*1024 {
		return
	}
	builderPool.Put(b)
}
