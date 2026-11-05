package image

import (
	"fmt"
	"image/color"
	"strings"
	"testing"
)

func TestBlurHashLengthBoundsAllocations(t *testing.T) {
	for _, size := range []int{167, 1024, 65536} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			hash := strings.Repeat("0", size)
			measured := testing.Benchmark(func(bench *testing.B) {
				for bench.Loop() {
					if _, err := DecodeBlurHash(hash, BlurHashRenderConfig{Width: 1, Height: 1}); err == nil {
						bench.Fatal("oversized hash accepted")
					}
				}
			})
			if allocated := measured.AllocedBytesPerOp(); allocated > 1024 {
				t.Fatalf("rejecting %d characters allocates %d bytes, want <= 1024", size, allocated)
			}
		})
	}
	image := FromImage(solidRGBA(9, 9, color.RGBA{R: 32, G: 64, B: 128, A: 255}), FormatPNG)
	hash, err := image.BlurHash(9, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 166 {
		t.Fatalf("maximum hash length = %d, want 166", len(hash))
	}
	if _, err := DecodeBlurHash(hash, BlurHashRenderConfig{Width: 1, Height: 1}); err != nil {
		t.Fatalf("maximum legal hash rejected: %v", err)
	}
}
