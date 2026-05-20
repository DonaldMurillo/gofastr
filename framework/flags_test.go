package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/featureflag"
)

func TestApp_FlagsLazyDefaultsToMemoryStore(t *testing.T) {
	a := NewApp(WithoutDefaultMiddleware())
	e := a.Flags()
	if e == nil {
		t.Fatal("Flags() returned nil evaluator")
	}
	// Same instance on second call.
	if a.Flags() != e {
		t.Fatalf("Flags() should be idempotent")
	}
}

func TestApp_SetFlagStoreReplacesEvaluator(t *testing.T) {
	a := NewApp(WithoutDefaultMiddleware())
	s := featureflag.NewMemoryStore()
	_ = s.Set(featureflag.Flag{Key: "f1", Enabled: true, Rollout: 100})
	a.SetFlagStore(s)

	if !a.IsEnabled(context.Background(), "f1") {
		t.Fatalf("expected f1 on after wiring store")
	}
	if a.IsEnabled(context.Background(), "missing") {
		t.Fatalf("missing flag should be off")
	}
}

func TestApp_IsEnabledUsesEvalContext(t *testing.T) {
	a := NewApp(WithoutDefaultMiddleware())
	s := featureflag.NewMemoryStore()
	_ = s.Set(featureflag.Flag{Key: "beta", Enabled: true, Users: []string{"alice"}})
	a.SetFlagStore(s)

	on := featureflag.WithContext(context.Background(), featureflag.EvalContext{UserID: "alice"})
	off := featureflag.WithContext(context.Background(), featureflag.EvalContext{UserID: "bob"})
	if !a.IsEnabled(on, "beta") {
		t.Fatalf("alice should see beta")
	}
	if a.IsEnabled(off, "beta") {
		t.Fatalf("bob should not see beta")
	}
}
