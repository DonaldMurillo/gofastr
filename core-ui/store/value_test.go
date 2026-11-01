package store

import (
	"context"
	"testing"
)

// Value is the per-request resolution a component stamps when it
// renders its own binding: the seed when a producer set one, else the
// declared default — the same value Bind stamps and the seed carries.
func TestSliceValueResolvesTheRequestSeed(t *testing.T) {
	resetForTest()
	qty := New("cart").Int("qty", 0)

	if got := qty.Value(context.Background()); got != 0 {
		t.Fatalf("Value with no value bag = %d, want the default 0", got)
	}
	var nilCtx context.Context
	if got := qty.Value(nilCtx); got != 0 {
		t.Fatalf("Value(nil) = %d, want the default 0", got)
	}
	ctx := WithValues(context.Background())
	if got := qty.Value(ctx); got != 0 {
		t.Fatalf("Value with an unseeded bag = %d, want the default 0", got)
	}
	qty.Seed(ctx, 7)
	if got := qty.Value(ctx); got != 7 {
		t.Fatalf("Value after Seed(ctx, 7) = %d, want 7", got)
	}
	if got := ResolveSeed(ctx, []string{qty.Name()})[qty.Name()]; got != 7 {
		t.Fatalf("the page seed carries %v while Value reads 7: they must agree", got)
	}
	if got := qty.Default(); got != 0 {
		t.Fatalf("Default = %d after a seed, want the declaration 0", got)
	}
}
