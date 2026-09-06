//go:build darwin && (arm64 || amd64)

package objc

import "unsafe"

// The arm64 block literal: 32 bytes of isa/flags/reserved/invoke/
// descriptor, followed by captured state for non-global blocks. Global
// blocks capture nothing, so the literal is fully described by these
// five words.
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

// blockDescriptorGlobal backs every block NewBlock creates: no copy
// or dispose helpers, so reserved/size are all it carries.
var blockDescriptorGlobal = blockDescriptor{size: blockSize}

// blocks keeps every literal alive (and, because they are separately
// allocated and never moved, at a stable address) for the process
// lifetime. C code holds these pointers indefinitely.
var blocks []*blockLiteral

// NewBlock builds a global block literal whose invoke is a ffi
// trampoline. fn is the trampoline address from ffi.NewCallback; the
// trampoline receives the block pointer as its first argument with the
// block's real arguments behind it. The returned pointer is a valid
// block for the lifetime of the process.
func NewBlock(fn uintptr) uintptr {
	ensureRuntime()
	b := &blockLiteral{
		isa:        symConcreteGlobalBlk,
		flags:      blockIsGlobal,
		invoke:     fn,
		descriptor: uintptr(unsafe.Pointer(&blockDescriptorGlobal)),
	}
	blocks = append(blocks, b)
	return uintptr(unsafe.Pointer(b))
}
