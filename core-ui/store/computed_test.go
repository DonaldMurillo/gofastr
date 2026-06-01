package store

import (
	"context"
	"strings"
	"testing"
)

func TestComputedEmitsWiringAttrs(t *testing.T) {
	resetForTest()
	_ = New("org").String("companyName", "Acme")
	greeting := Computed[string](New("org"), "greeting", "greet", "org.companyName")
	html := string(greeting.Bind(context.Background(), "h1", nil))

	for _, want := range []string{
		`data-fui-signal="org.greeting"`,
		`data-fui-computed="greet"`,
		`data-fui-computed-deps="org.companyName"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("computed bind missing %q: %s", want, html)
		}
	}
}

func TestComputedExcludedFromSeedButDepsSeeded(t *testing.T) {
	resetForTest()
	_ = New("org").String("companyName", "Acme")
	_ = Computed[string](New("org"), "greeting", "greet", "org.companyName")

	seed := ResolveSeed(context.Background(), []string{"org.companyName", "org.greeting"})
	if _, ok := seed["org.greeting"]; ok {
		t.Errorf("computed slice must not be seeded (it is derived): %v", seed)
	}
	if seed["org.companyName"] != "Acme" {
		t.Errorf("dependency slice should still seed: %v", seed)
	}
}

func TestComputedMultipleDeps(t *testing.T) {
	resetForTest()
	_ = New("cart").Int("a", 1)
	_ = New("cart").Int("b", 2)
	total := Computed[int](New("cart"), "total", "sum", "cart.a", "cart.b")
	html := string(total.Bind(context.Background(), "span", nil))
	if !strings.Contains(html, `data-fui-computed-deps="cart.a,cart.b"`) {
		t.Errorf("multi-dep list wrong: %s", html)
	}
}

func TestComputedNameConflictPanics(t *testing.T) {
	resetForTest()
	_ = New("x").String("v", "1") // value slice
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic redeclaring a value slice as computed")
		}
	}()
	_ = Computed[string](New("x"), "v", "r")
}
