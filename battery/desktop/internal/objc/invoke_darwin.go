//go:build darwin && (arm64 || amd64)

package objc

// Rect is NSRect: {NSPoint origin; NSSize size;} with CGFloat (float64 on 64-bit) members.
type Rect struct {
	X, Y, W, H float64
}

// Point is NSPoint: two CGFloats.
type Point struct {
	X, Y float64
}
