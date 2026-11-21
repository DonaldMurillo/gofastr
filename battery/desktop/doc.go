// Package desktop runs a GoFastr app inside the operating system's own
// WebView, with a typed JavaScript bridge to native capabilities that
// plugins extend in plain Go.
//
// EXPERIMENTAL: the capability surface, the Shell and Window contracts and
// the generated bridge may change without a major version bump.
//
//   - battery/desktop/linux: the real shell on linux/amd64 and linux/arm64
//     (GTK 3 + WebKitGTK 4.1). Other Linux architectures are unsupported.
package desktop
