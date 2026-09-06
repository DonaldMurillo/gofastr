// Package ffi holds the runtime linknames and the callback trampoline
// table that let a CGO_ENABLED=0 binary call C and receive C callbacks
// on darwin/arm64, darwin/amd64, linux/arm64, and linux/amd64.
//
// It is the slice of ebitengine/purego that this repo needs, written
// in-process because the no-third-party-dependency rule forbids the
// import. Every runtime hook it pulls is one the Go team keeps public
// for golang.org/x/sys or tolerates for purego (go.dev/issue/67401);
// none is promised to us, which is why the untagged self-test calls
// each hook once: the day a Go release removes one, the first
// `go test ./battery/desktop/...` run fails, not a user's app.
//
// The Go surface is one API on both architectures. What differs
// underneath, and where it leaks through:
//
//   - IntRegs is 8 (x0-x7) on arm64 and 6 (rdi, rsi, rdx, rcx, r8, r9)
//     on amd64. Call takes the same argument slice on both and spills
//     past IntRegs to the C stack; only a caller laying out a
//     MEMORY-class argument itself has to know the number.
//   - CallRetStruct means "return a large struct into ret" on both, over
//     arm64's x8 indirect-result register and over System V's hidden
//     first argument.
//   - CallStack exists on amd64 only, for the stack argument area a
//     >16-byte aggregate is passed in there. arm64 has no use for it.
//   - The callback table's entrySize is 8 on arm64 and 5 on amd64.
//     Callers never see it: NewCallback returns an address.
//
// On every other GOOS/GOARCH the package is empty; the native layers
// that use it are build-tagged the same way.
package ffi
