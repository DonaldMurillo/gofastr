package render

import (
	"context"
	"testing"
)

func TestUniqueIDSuffixesRepeats(t *testing.T) {
	ctx := WithIDScope(context.Background())
	ReserveID(ctx, "plans")
	got := []string{
		UniqueID(ctx, "features"),
		UniqueID(ctx, "features"),
		UniqueID(ctx, "features-3"),
		UniqueID(ctx, "features"),
		UniqueID(ctx, "plans"),
	}
	want := []string{"features", "features-2", "features-3", "features-4", "plans-2"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestUniqueIDWithoutScopeKeepsID(t *testing.T) {
	ctx := context.Background()
	if a, b := UniqueID(ctx, "x"), UniqueID(ctx, "x"); a != "x" || b != "x" {
		t.Errorf("no scope: %q, %q", a, b)
	}
	var none context.Context // a component config whose Ctx is unset
	if got := UniqueID(none, "x"); got != "x" {
		t.Errorf("nil ctx: %q", got)
	}
}

func TestForkIDScopeCopiesTakenIDs(t *testing.T) {
	ctx := WithIDScope(context.Background())
	UniqueID(ctx, "a")
	f1, f2 := ForkIDScope(ctx), ForkIDScope(ctx)
	if got := UniqueID(f1, "a"); got != "a-2" {
		t.Errorf("fork sees parent's id: %q, want a-2", got)
	}
	if got := UniqueID(f2, "a"); got != "a-2" {
		t.Errorf("sibling forks are independent: %q, want a-2", got)
	}
	if got := UniqueID(ctx, "a"); got != "a-2" {
		t.Errorf("fork writes back to parent: %q, want a-2", got)
	}
}
