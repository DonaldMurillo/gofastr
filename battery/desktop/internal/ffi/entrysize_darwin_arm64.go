//go:build darwin && arm64

package ffi

// entrySize is the byte length of one callbackasm table entry on arm64:
// MOVD $i, R12 (MOVZ, 4 bytes) + B ·callbackasm1 (4 bytes). Go computes
// entry i's address as callbackasmAddr + i*entrySize.
const entrySize = 8
