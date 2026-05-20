package featureflag

import (
	"context"
	"testing"
)

// ----- BoolDefault distinguishes "missing" from "disabled" ------------------

func TestEvaluator_BoolDefaultReturnsFallbackWhenKeyMissing(t *testing.T) {
	e := NewEvaluator(NewMemoryStore())
	if got := e.BoolDefault(context.Background(), "absent", true); got != true {
		t.Fatalf("missing key must yield supplied default=true; got %v", got)
	}
	if got := e.BoolDefault(context.Background(), "absent", false); got != false {
		t.Fatalf("missing key with default=false must yield false; got %v", got)
	}
}

func TestEvaluator_BoolDefaultRespectsExplicitDisabled(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: false, Rollout: 100})
	e := NewEvaluator(s)
	if e.BoolDefault(context.Background(), "x", true) {
		t.Fatalf("explicitly disabled flag must override default=true; safe path is off")
	}
}

func TestEvaluator_BoolDefaultRespectsExplicitEnabled(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: true, Rollout: 100})
	e := NewEvaluator(s)
	ctx := WithContext(context.Background(), EvalContext{UserID: "u1"})
	if !e.BoolDefault(ctx, "x", false) {
		t.Fatalf("enabled flag should ignore default=false; got off")
	}
}

// ----- Anonymous subject falls back to off below 100% rollout ---------------

func TestEvaluator_EmptySubjectOffBelowFullRollout(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: true, Rollout: 50})
	e := NewEvaluator(s)
	ctx := WithContext(context.Background(), EvalContext{})
	for i := 0; i < 20; i++ {
		if e.Bool(ctx, "x") {
			t.Fatalf("anonymous subject must NOT see a flag below 100%% rollout — that'd be 100%% per key")
		}
	}
}

func TestEvaluator_EmptySubjectOnAtFullRollout(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: true, Rollout: 100})
	e := NewEvaluator(s)
	ctx := WithContext(context.Background(), EvalContext{})
	if !e.Bool(ctx, "x") {
		t.Fatalf("anonymous subject at rollout=100 (everyone) should see flag")
	}
}

// ----- Rollout salt mixes into bucket --------------------------------------

func TestEvaluator_SaltShiftsBuckets(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "k", Enabled: true, Rollout: 50})
	e1 := NewEvaluatorWithSalt(s, "process-a")
	e2 := NewEvaluatorWithSalt(s, "process-b")

	ctx := WithContext(context.Background(), EvalContext{UserID: "alice"})
	// Salt must change SOMETHING — over many users, the two evaluators
	// can't produce identical decisions. Use a representative sample.
	disagreements := 0
	for i := 0; i < 200; i++ {
		uid := "user-" + string(rune('A'+i%26)) + string(rune('0'+i%10))
		c := WithContext(context.Background(), EvalContext{UserID: uid})
		if e1.Bool(c, "k") != e2.Bool(c, "k") {
			disagreements++
		}
	}
	if disagreements < 10 {
		t.Fatalf("salted evaluators should disagree on a meaningful share of subjects; got %d/200", disagreements)
	}
	_ = ctx // silence unused
}
