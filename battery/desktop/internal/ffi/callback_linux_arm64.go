//go:build linux && arm64

package ffi

// entrySize is the byte length of one callbackasm table entry on arm64:
// MOVD $i, R12 (a single MOVZ, 4 bytes) plus B ·callbackasm1 (4 bytes).
// See callback_linux_arm64.s.
const entrySize = 8
