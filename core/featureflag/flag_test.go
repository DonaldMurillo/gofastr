package featureflag

import (
	"context"
	"testing"
)

func TestMemoryStore_GetMissingReturnsNil(t *testing.T) {
	s := NewMemoryStore()
	f, err := s.Get(context.Background(), "nope")
	if err != nil || f != nil {
		t.Fatalf("missing key: got (%v, %v), want (nil, nil)", f, err)
	}
}

func TestMemoryStore_SetEmptyKeyErrors(t *testing.T) {
	s := NewMemoryStore()
	if err := s.Set(Flag{}); err == nil {
		t.Fatalf("expected error on empty key")
	}
}

func TestMemoryStore_RolloutClamped(t *testing.T) {
	s := NewMemoryStore()
	if err := s.Set(Flag{Key: "x", Enabled: true, Rollout: 200}); err != nil {
		t.Fatal(err)
	}
	f, _ := s.Get(context.Background(), "x")
	if f.Rollout != 100 {
		t.Fatalf("rollout: got %d want 100", f.Rollout)
	}
	if err := s.Set(Flag{Key: "y", Enabled: true, Rollout: -5}); err != nil {
		t.Fatal(err)
	}
	f, _ = s.Get(context.Background(), "y")
	if f.Rollout != 0 {
		t.Fatalf("rollout: got %d want 0", f.Rollout)
	}
}

func TestEvaluator_DisabledKillsEverything(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: false, Rollout: 100, Users: []string{"u1"}})
	e := NewEvaluator(s)
	ctx := WithContext(context.Background(), EvalContext{UserID: "u1"})
	if e.Bool(ctx, "x") {
		t.Fatalf("disabled flag should be off even for allow-listed users")
	}
}

func TestEvaluator_UserAllowlist(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: true, Users: []string{"alice"}})
	e := NewEvaluator(s)
	for _, tc := range []struct {
		userID string
		want   bool
	}{
		{"alice", true},
		{"bob", false},
		{"", false}, // empty userID never matches even if Users contains ""
	} {
		ctx := WithContext(context.Background(), EvalContext{UserID: tc.userID})
		if got := e.Bool(ctx, "x"); got != tc.want {
			t.Fatalf("userID=%q: got %v want %v", tc.userID, got, tc.want)
		}
	}
}

func TestEvaluator_TenantAllowlist(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: true, Tenants: []string{"acme"}})
	e := NewEvaluator(s)
	yes := WithContext(context.Background(), EvalContext{TenantID: "acme"})
	no := WithContext(context.Background(), EvalContext{TenantID: "beta"})
	if !e.Bool(yes, "x") {
		t.Fatalf("acme tenant should match")
	}
	if e.Bool(no, "x") {
		t.Fatalf("beta tenant should not match")
	}
}

func TestEvaluator_RolloutZeroOff(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: true, Rollout: 0})
	e := NewEvaluator(s)
	for i := 0; i < 100; i++ {
		ctx := WithContext(context.Background(), EvalContext{UserID: "u" + string(rune('0'+i%10))})
		if e.Bool(ctx, "x") {
			t.Fatalf("rollout 0 should be uniformly off")
		}
	}
}

func TestEvaluator_RolloutHundredOn(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: true, Rollout: 100})
	e := NewEvaluator(s)
	for i := 0; i < 100; i++ {
		ctx := WithContext(context.Background(), EvalContext{UserID: "u" + string(rune('0'+i%10))})
		if !e.Bool(ctx, "x") {
			t.Fatalf("rollout 100 should be uniformly on")
		}
	}
}

func TestEvaluator_RolloutStableAcrossCalls(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: true, Rollout: 50})
	e := NewEvaluator(s)
	ctx := WithContext(context.Background(), EvalContext{UserID: "alice"})
	first := e.Bool(ctx, "x")
	for i := 0; i < 20; i++ {
		if got := e.Bool(ctx, "x"); got != first {
			t.Fatalf("same subject must return stable result, flipped on call %d", i)
		}
	}
}

func TestEvaluator_RolloutPartiallyDistributes(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: true, Rollout: 50})
	e := NewEvaluator(s)
	on := 0
	const N = 1000
	for i := 0; i < N; i++ {
		ctx := WithContext(context.Background(), EvalContext{UserID: testSubject(i)})
		if e.Bool(ctx, "x") {
			on++
		}
	}
	// 50% rollout over 1000 distinct subjects — allow wide tolerance
	// since FNV bucketing is uniform but not perfect.
	if on < 400 || on > 600 {
		t.Fatalf("50%% rollout produced %d/%d on — expected ~500", on, N)
	}
}

func TestEvaluator_EnvAllowlistGatesByEvalContextEnv(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: true, Rollout: 100, Envs: []string{"staging"}})
	e := NewEvaluator(s)

	staging := WithContext(context.Background(), EvalContext{UserID: "u", Env: "staging"})
	prod := WithContext(context.Background(), EvalContext{UserID: "u", Env: "production"})
	if !e.Bool(staging, "x") {
		t.Fatalf("staging request should see flag with Envs=[staging]")
	}
	if e.Bool(prod, "x") {
		t.Fatalf("production request must NOT see flag restricted to staging")
	}
}

func TestEvaluator_EnvEmptyEnvsMeansAnyEnv(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: true, Rollout: 100})
	e := NewEvaluator(s)
	for _, env := range []string{"", "dev", "staging", "production"} {
		ctx := WithContext(context.Background(), EvalContext{UserID: "u", Env: env})
		if !e.Bool(ctx, "x") {
			t.Fatalf("env=%q: flag without Envs restriction must be on", env)
		}
	}
}

func TestEvaluator_EnvAllowlistOverridesUserAllowlist(t *testing.T) {
	// Env restriction is the outermost filter — a force-enable via Users
	// must still not leak the flag into the wrong environment.
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: true, Users: []string{"alice"}, Envs: []string{"dev"}})
	e := NewEvaluator(s)

	devAlice := WithContext(context.Background(), EvalContext{UserID: "alice", Env: "dev"})
	prodAlice := WithContext(context.Background(), EvalContext{UserID: "alice", Env: "production"})
	if !e.Bool(devAlice, "x") {
		t.Fatalf("alice in dev should see flag")
	}
	if e.Bool(prodAlice, "x") {
		t.Fatalf("alice in production must NOT bypass Envs restriction")
	}
}

func TestEvaluator_MissingFlagFalse(t *testing.T) {
	e := NewEvaluator(NewMemoryStore())
	if e.Bool(context.Background(), "absent") {
		t.Fatalf("absent flag should default false")
	}
}

func TestEvaluator_NilStoreFalse(t *testing.T) {
	if NewEvaluator(nil).Bool(context.Background(), "x") {
		t.Fatalf("nil-store evaluator should always return false")
	}
}

func TestDefault_Helpers(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "x", Enabled: true, Rollout: 100})
	prev := Default()
	SetDefault(NewEvaluator(s))
	defer SetDefault(prev)

	if !Bool(context.Background(), "x") {
		t.Fatalf("global Bool should consult Default()")
	}
}

func TestDefault_NilAfterClear(t *testing.T) {
	prev := Default()
	SetDefault(nil)
	defer SetDefault(prev)
	if Bool(context.Background(), "anything") {
		t.Fatalf("Bool with no default should return false, not panic")
	}
}

func TestFromContext_ZeroValue(t *testing.T) {
	got := FromContext(context.Background())
	if got.UserID != "" || got.TenantID != "" || got.Env != "" || got.Attrs != nil {
		t.Fatalf("missing eval context should yield zero value, got %+v", got)
	}
	gotNil := FromContext(nil)
	if gotNil.UserID != "" || gotNil.TenantID != "" || gotNil.Env != "" || gotNil.Attrs != nil {
		t.Fatalf("nil context should yield zero value, got %+v", gotNil)
	}
}

func testSubject(i int) string {
	// Generate stable distinct subject ids without importing strconv.
	b := []byte("subject-")
	for n := i; ; n /= 10 {
		b = append(b, byte('0'+n%10))
		if n < 10 {
			break
		}
	}
	return string(b)
}
