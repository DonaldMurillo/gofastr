//go:build darwin && (arm64 || amd64)

package objc

// Rect is NSRect: {NSPoint origin; NSSize size;} with CGFloat (float64
// on 64-bit) members.
//
// Where those four doubles travel is the one ABI question the two
// architectures answer differently, so SendRect and SendRectRet live in
// msgsend_darwin_arm64.go and msgsend_darwin_amd64.go rather than here.
// In short: arm64 calls it a homogeneous floating-point aggregate and
// passes and returns it in d0-d3; System V calls 32 bytes MEMORY class
// whatever is in them and passes it in the caller's stack argument area,
// returning it through a hidden pointer and objc_msgSend_stret.
//
// The NSInvocation-based Invoke helper this type used to need is gone:
// every signature the host calls goes through ffi's register loading.
type Rect struct {
	X, Y, W, H float64
}

// Point is NSPoint: two CGFloats. Sixteen bytes of floats fit the
// register rules on both architectures — d0-d1 on arm64, xmm0-xmm1 on
// amd64 — so unlike Rect it needs no per-architecture handling.
type Point struct {
	X, Y float64
}
