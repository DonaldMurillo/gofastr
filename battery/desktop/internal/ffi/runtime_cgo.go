//go:build (linux || darwin) && (amd64 || arm64) && cgo

package ffi

import _ "runtime/cgo"
