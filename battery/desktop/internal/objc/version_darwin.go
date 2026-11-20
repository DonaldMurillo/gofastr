//go:build darwin && (arm64 || amd64)

package objc

import (
	"encoding/binary"
	"math"
)

// GlobalDouble reads a double global C symbol from the process's default image.
func GlobalDouble(name string) (float64, bool) {
	p, err := Dlsym(rtldDefault, name)
	if err != nil || p == 0 {
		return 0, false
	}
	b := CopyBytes(p, 8)
	if len(b) != 8 {
		return 0, false
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b)), true
}
