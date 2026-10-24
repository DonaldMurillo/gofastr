//go:build (linux || darwin) && (amd64 || arm64) && !cgo

package ffi

// Without runtime/cgo, the assembly shim supplies the runtime hooks and
// enables runtime.cgocall before any Go code runs.
import _ "github.com/DonaldMurillo/gofastr/battery/desktop/internal/fakecgo"
