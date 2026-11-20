//go:build darwin && (arm64 || amd64)

package objc

import (
	"sync"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
)

type blockLiteral struct {
	isa        uintptr
	flags      int32
	reserved   int32
	invoke     uintptr
	descriptor uintptr
}

type blockDescriptor struct {
	reserved uint64
	size     uint64
}

const (
	blockIsGlobal = 1 << 28
	blockSize     = 32
)

var blockDescriptorGlobal = blockDescriptor{size: blockSize}

var blocks []*blockLiteral
var blocksMu sync.Mutex

// CallBlock invokes a block that takes one word-sized argument, such as WebKit's decisionHandler.
func CallBlock(block, arg uintptr) {
	if block == 0 {
		return
	}
	lit := (*blockLiteral)(cptr(block))
	ffi.Call(lit.invoke, []uintptr{block, arg}, nil)
}

// NewBlock builds a global block literal whose invoke is a ffi trampoline.
func NewBlock(fn uintptr) uintptr {
	ensureRuntime()
	b := &blockLiteral{
		isa:        symConcreteGlobalBlk,
		flags:      blockIsGlobal,
		invoke:     fn,
		descriptor: uintptr(unsafe.Pointer(&blockDescriptorGlobal)),
	}
	blocksMu.Lock()
	blocks = append(blocks, b)
	blocksMu.Unlock()
	return uintptr(unsafe.Pointer(b))
}
