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
		`data-cui-signal="org.greeting"`,
		`data-cui-computed="greet"`,
		`data-cui-computed-deps="org.companyName"`,
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
	if !strings.Contains(html, `data-cui-computed-deps="cart.a,cart.b"`) {
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

// A cycle between computed slices recursed in the runtime until the
// stack overflowed, freezing the page (2026-10-27 R7). Declaring one
// panics, naming the path.
func TestComputedRefusesCycles(t *testing.T) {
	refused := map[string]string{"self": "c.a", "pair": "c.b", "three": "c.c"}
	paths := map[string]string{"self": "c.a -> c.a", "pair": "c.b -> c.a -> c.b", "three": "c.c -> c.a -> c.b -> c.c"}
	for name, declare := range map[string]func(){
		"self": func() { _ = Computed[int](New("c"), "a", "r", "c.a") },
		"pair": func() {
			_ = Computed[int](New("c"), "a", "r", "c.b")
			_ = Computed[int](New("c"), "b", "r", "c.a")
		},
		"three": func() {
			_ = Computed[int](New("c"), "a", "r", "c.x", "c.b")
			_ = Computed[int](New("c"), "b", "r", "c.c")
			_ = Computed[int](New("c"), "c", "r", "c.a")
		},
	} {
		t.Run(name, func(t *testing.T) {
			resetForTest()
			defer func() {
				msg, _ := recover().(string)
				if !strings.Contains(msg, "cycle: "+paths[name]) {
					t.Fatalf("panic %q, want the cycle %s", msg, paths[name])
				}
				// The refused slice is not left registered.
				if _, kept := declRegistry[refused[name]]; kept {
					t.Errorf("%s stayed registered after its cycle was refused", refused[name])
				}
			}()
			declare()
		})
	}
	// A diamond is not a cycle: a and b both read x, c reads both.
	resetForTest()
	_ = New("d").Int("x", 0)
	_ = Computed[int](New("d"), "a", "r", "d.x")
	_ = Computed[int](New("d"), "b", "r", "d.x")
	_ = Computed[int](New("d"), "c", "r", "d.a", "d.b")
	// Re-declaring an acyclic computed with its own deps stays allowed.
	_ = Computed[int](New("d"), "c", "r", "d.a", "d.b")
}
