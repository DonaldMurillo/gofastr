package provider

import (
	"math"
	"testing"
)

func TestSweepUSDPricesCacheWrites(t *testing.T) {
	if usd := USDForUsage(Pricing{CacheWritePerMTok: 3}, Usage{CacheWriteTokens: 500_000}); math.Abs(usd-1.5) > 1e-9 {
		t.Fatalf("cache-write cost = %v, want 1.5", usd)
	}
}
