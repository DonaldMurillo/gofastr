//go:build (linux || darwin) && (amd64 || arm64) && cgo

package ffi

// CGO-enabled consumers use the standard runtime hooks. Linking fakecgo
// alongside runtime/cgo would define _cgo_init and the other hooks twice.
// The desktop bindings themselves still use Go assembly, without C glue.
import _ "runtime/cgo"
