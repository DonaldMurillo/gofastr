//go:build !linux && !darwin && !windows

package framework

// This file is the fallback arm of the §6 sandbox backend for GOOS values
// that are neither linux, darwin, nor windows (e.g. freebsd, netbsd,
// openbsd, js/wasm, plan9). The repo does not implement a wrapper-command
// backend for those platforms in v1, so defaultSandboxBackend returns nil
// and HostSandboxBackend propagates that nil to the supervisor — every
// TrustUntrusted module fails closed on these platforms. Fail-closed is
// the only honest answer when no backend is compiled in.
//
// A future port that ships a FreeBSD `sandbox`/`capsicum` wrapper or a
// NetBSD `pledge`/`unveil` wrapper slots in by adding a build-tagged file
// for that GOOS; this file is the safety net that keeps the package
// compiling on every other target.

// defaultSandboxBackend returns nil on platforms without a v1 backend.
func defaultSandboxBackend() SandboxBackend { return nil }
