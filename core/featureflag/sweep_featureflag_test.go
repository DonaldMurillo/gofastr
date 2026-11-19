package featureflag

import (
	"context"
	"fmt"
	"testing"
)

func TestSweepFeatureflagStoreParity(t *testing.T) {
	_, sqlStore := openSQLStore(t)
	stores := map[string]MutableStore{"memory": NewMemoryStore(), "sql": sqlStore}
	sets := []Flag{
		{Key: ""},
		{Key: "bad-user", Enabled: true, Users: []string{"acme:"}},
		{Key: "bad-escape", Enabled: true, Users: []string{"a%zz:1"}},
		{Key: "over", Enabled: true, Rollout: 250},
		{Key: "under", Enabled: true, Rollout: -5},
		{Key: "half", Enabled: true, Rollout: 50},
		{Key: "off", Enabled: false, Rollout: 100, Users: []string{"1"}},
		{Key: "lists", Enabled: true, Users: []string{"acme:7", "9", ":g:1"}, Tenants: []string{"beta"}, Envs: []string{"prod"}},
		{Key: "k'; DROP TABLE feature_flags; --", Enabled: true, Rollout: 100},
	}
	errs := map[string][]string{}
	for name, s := range stores {
		for _, f := range sets {
			err := s.Set(f)
			errs[name] = append(errs[name], fmt.Sprint(err))
		}
	}
	for i := range sets {
		if errs["memory"][i] != errs["sql"][i] {
			t.Errorf("Set(%q): memory %q, sql %q", sets[i].Key, errs["memory"][i], errs["sql"][i])
		}
	}
	callers := []EvalContext{
		{}, {UserID: "1"}, {UserID: "7", TenantID: "acme"}, {UserID: "7", TenantID: "beta", Env: "prod"},
		{UserID: "9", Env: "prod"}, {UserID: "g:1", Env: "prod"}, {TenantID: "beta", Env: "dev"},
	}
	for i := range 40 {
		callers = append(callers, EvalContext{UserID: fmt.Sprint(i), TenantID: "t", Env: "prod"})
	}
	mem, sq := NewEvaluator(stores["memory"]), NewEvaluator(stores["sql"])
	for _, f := range sets {
		for _, ec := range callers {
			ctx := WithContext(context.Background(), ec)
			if a, b := mem.Bool(ctx, f.Key), sq.Bool(ctx, f.Key); a != b {
				t.Errorf("Bool(%q, %+v): memory %v, sql %v", f.Key, ec, a, b)
			}
			if a, b := mem.BoolDefault(ctx, f.Key, true), sq.BoolDefault(ctx, f.Key, true); a != b {
				t.Errorf("BoolDefault(%q, %+v): memory %v, sql %v", f.Key, ec, a, b)
			}
		}
	}
	for _, key := range []string{"over", "under"} {
		a, _ := stores["memory"].Get(context.Background(), key)
		b, _ := stores["sql"].Get(context.Background(), key)
		if a.Rollout != b.Rollout {
			t.Errorf("%s rollout: memory %d, sql %d", key, a.Rollout, b.Rollout)
		}
	}
}

func TestSweepFeatureflagEmptyTenantNeverMatches(t *testing.T) {
	s := NewMemoryStore()
	if err := s.Set(Flag{Key: "f", Enabled: true, Tenants: []string{""}}); err != nil {
		t.Fatal(err)
	}
	if NewEvaluator(s).Bool(context.Background(), "f") {
		t.Fatal("an empty Tenants entry turned the flag on for a caller with no tenant")
	}
}

func TestSweepFeatureflagContextEvaluatorWins(t *testing.T) {
	on := NewMemoryStore()
	if err := on.Set(Flag{Key: "f", Enabled: true, Rollout: 100}); err != nil {
		t.Fatal(err)
	}
	SetDefault(NewEvaluator(on))
	t.Cleanup(func() { SetDefault(nil) })
	if !Bool(context.Background(), "f") {
		t.Fatal("Bool ignored Default on a bare context")
	}
	if Bool(WithEvaluator(context.Background(), nil), "f") {
		t.Fatal("a nil evaluator on the context fell back to Default")
	}
	if Bool(WithEvaluator(context.Background(), NewEvaluator(NewMemoryStore())), "f") {
		t.Fatal("the context's evaluator lost to Default")
	}
}

func TestSweepFeatureflagGetReturnsACopy(t *testing.T) {
	s := NewMemoryStore()
	if err := s.Set(Flag{Key: "f", Enabled: true, Users: []string{"1"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(context.Background(), "f")
	got.Users[0] = "2"
	again, _ := s.Get(context.Background(), "f")
	if again.Users[0] != "1" {
		t.Fatal("changing the flag Get returned changed the store")
	}
	in := Flag{Key: "g", Enabled: true, Users: []string{"1"}}
	if err := s.Set(in); err != nil {
		t.Fatal(err)
	}
	in.Users[0] = "2"
	if g, _ := s.Get(context.Background(), "g"); g.Users[0] != "1" {
		t.Fatal("changing the flag passed to Set changed the store")
	}
}
