package image

import (
	"strings"
	"sync"
	"testing"
)

func TestBlurHashDataURL(t *testing.T) {
	FlushBlurHashCache()
	durl, err := BlurHashDataURL(refHash, BlurHashRenderConfig{})
	if err != nil {
		t.Fatalf("BlurHashDataURL: %v", err)
	}
	if !strings.HasPrefix(durl, "data:image/jpeg;base64,") {
		t.Fatalf("want a JPEG data URL, got %.40q", durl)
	}
	// The whole point of a placeholder is that it is small enough to inline
	// into the HTML document. Guard the property, not a magic number.
	if len(durl) > 2048 {
		t.Errorf("placeholder data URL is %d bytes; too large to inline", len(durl))
	}
}

func TestBlurHashDataURLPNGFormat(t *testing.T) {
	FlushBlurHashCache()
	durl, err := BlurHashDataURL(refHash, BlurHashRenderConfig{Format: FormatPNG})
	if err != nil {
		t.Fatalf("BlurHashDataURL: %v", err)
	}
	if !strings.HasPrefix(durl, "data:image/png;base64,") {
		t.Fatalf("want a PNG data URL, got %.40q", durl)
	}
}

func TestBlurHashDataURLPropagatesDecodeError(t *testing.T) {
	FlushBlurHashCache()
	if _, err := BlurHashDataURL("not-a-hash", BlurHashRenderConfig{}); err == nil {
		t.Fatal("expected an error for a malformed hash")
	}
	if BlurHashCacheLen() != 0 {
		t.Error("a failed decode must not be cached")
	}
}

func TestBlurHashDataURLCachesByKey(t *testing.T) {
	FlushBlurHashCache()
	first, err := BlurHashDataURL(refHash, BlurHashRenderConfig{Width: 16, Height: 16})
	if err != nil {
		t.Fatalf("BlurHashDataURL: %v", err)
	}
	if got := BlurHashCacheLen(); got != 1 {
		t.Fatalf("cache len = %d, want 1", got)
	}
	second, err := BlurHashDataURL(refHash, BlurHashRenderConfig{Width: 16, Height: 16})
	if err != nil {
		t.Fatalf("BlurHashDataURL: %v", err)
	}
	if first != second {
		t.Error("cached call returned a different value")
	}
	if got := BlurHashCacheLen(); got != 1 {
		t.Errorf("cache len = %d after a repeat call, want 1", got)
	}
	// Every config field must participate in the key, or one variant's
	// render would be served for another's.
	for _, cfg := range []BlurHashRenderConfig{
		{Width: 24, Height: 16},
		{Width: 16, Height: 24},
		{Width: 16, Height: 16, Punch: 2},
		{Width: 16, Height: 16, Format: FormatPNG},
		{Width: 16, Height: 16, Quality: 90},
	} {
		if _, err := BlurHashDataURL(refHash, cfg); err != nil {
			t.Fatalf("BlurHashDataURL(%+v): %v", cfg, err)
		}
	}
	if got := BlurHashCacheLen(); got != 6 {
		t.Errorf("cache len = %d, want 6 distinct keys", got)
	}
}

func TestBlurHashCacheEvictsAtCapacity(t *testing.T) {
	t.Cleanup(func() { SetBlurHashCacheSize(defaultBlurHashCacheSize) })
	SetBlurHashCacheSize(4)
	for w := 10; w < 20; w++ {
		if _, err := BlurHashDataURL(refHash, BlurHashRenderConfig{Width: w, Height: 16}); err != nil {
			t.Fatalf("BlurHashDataURL(width=%d): %v", w, err)
		}
	}
	if got := BlurHashCacheLen(); got > 4 {
		t.Errorf("cache len = %d, want <= 4", got)
	}
}

func TestSetBlurHashCacheSizeZeroDisables(t *testing.T) {
	t.Cleanup(func() { SetBlurHashCacheSize(defaultBlurHashCacheSize) })
	SetBlurHashCacheSize(0)
	durl, err := BlurHashDataURL(refHash, BlurHashRenderConfig{})
	if err != nil {
		t.Fatalf("BlurHashDataURL: %v", err)
	}
	if durl == "" {
		t.Fatal("caching disabled must still return a result")
	}
	if got := BlurHashCacheLen(); got != 0 {
		t.Errorf("cache len = %d with caching disabled, want 0", got)
	}
}

func TestBlurHashDataURLConcurrent(t *testing.T) {
	FlushBlurHashCache()
	t.Cleanup(func() { SetBlurHashCacheSize(defaultBlurHashCacheSize) })
	SetBlurHashCacheSize(8)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Mix of repeats and fresh keys so readers, writers, and
			// eviction all race under -race.
			cfg := BlurHashRenderConfig{Width: 16 + i%6, Height: 16}
			if _, err := BlurHashDataURL(refHash, cfg); err != nil {
				t.Errorf("BlurHashDataURL: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
