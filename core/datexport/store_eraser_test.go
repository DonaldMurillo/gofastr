package datexport

import (
	"context"
	"slices"
	"testing"
)

func nopErase(context.Context, string, bool) (int, error) { return 0, nil }

func storeNames() []string {
	var out []string
	for _, e := range AllStoreErasers() {
		out = append(out, e.Name+"/"+e.Source)
	}
	return out
}

// Two RedisQueues with one queue name on different servers registered one
// Name, and the second replaced the first, so the first server's jobs
// left every erasure. Both now stay registered, the later one under the
// first free "#n" label, and each unregister removes only its own.
func TestSameNameStoresBothStay(t *testing.T) {
	Reset(t)
	t.Cleanup(func() { Reset(t) })
	_, first := RegisterStoreEraser(StoreEraser{Name: "s", Source: "a", Erase: nopErase})
	name, second := RegisterStoreEraser(StoreEraser{Name: "s", Source: "b", Erase: nopErase})
	if name != "s#2" {
		t.Errorf("second registration named %q, want s#2", name)
	}
	if got := storeNames(); len(got) != 2 || got[0] != "s/a" || got[1] != "s#2/b" {
		t.Fatalf("two stores named s = %v, want [s/a s#2/b]", got)
	}
	first()
	if got := storeNames(); len(got) != 1 || got[0] != "s#2/b" {
		t.Fatalf("after the first unregistered: %v, want [s#2/b]", got)
	}
	_, third := RegisterStoreEraser(StoreEraser{Name: "s", Source: "c", Erase: nopErase})
	if got := storeNames(); len(got) != 2 || got[0] != "s/c" || got[1] != "s#2/b" {
		t.Fatalf("a third store takes the free name: %v, want [s/c s#2/b]", got)
	}
	first() // a second call removes nothing
	second()
	third()
	if got := AllStoreErasers(); len(got) != 0 {
		t.Fatalf("after unregister: %+v, want none", got)
	}
}

func TestStoreEraserNeedsNameAndErase(t *testing.T) {
	for _, e := range []StoreEraser{{Erase: nopErase}, {Name: "s"}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("RegisterStoreEraser(%+v) did not panic", e)
				}
			}()
			RegisterStoreEraser(e)
		}()
	}
}

// A producer (a scheduler that enqueues jobs) is listed, and so erased,
// before every store it could refill, whatever the names.
func TestProducersListedFirst(t *testing.T) {
	Reset(t)
	t.Cleanup(func() { Reset(t) })
	RegisterStoreEraser(StoreEraser{Name: "b", Source: "q", Erase: nopErase})
	RegisterStoreEraser(StoreEraser{Name: "z", Source: "p", Erase: nopErase, Producer: true})
	RegisterStoreEraser(StoreEraser{Name: "a", Source: "q", Erase: nopErase})
	RegisterStoreEraser(StoreEraser{Name: "y", Source: "p", Erase: nopErase, Producer: true})
	got := storeNames()
	want := []string{"y/p", "z/p", "a/q", "b/q"}
	if !slices.Equal(got, want) {
		t.Errorf("store erasers in order %v, want %v", got, want)
	}
}

// A store eraser takes exactly one of Erase and EraseResolved: neither
// would leave the store out of every erasure, and both leave which one
// runs to chance.
func TestStoreEraserNeedsExactlyOneEraseFunc(t *testing.T) {
	Reset(t)
	t.Cleanup(func() { Reset(t) })
	resolved := func(context.Context, ErasedUser, bool) (int, error) { return 0, nil }
	for name, e := range map[string]StoreEraser{
		"neither": {Name: "s", Source: "a"},
		"both":    {Name: "s", Source: "a", Erase: nopErase, EraseResolved: resolved},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: registered", name)
				}
			}()
			RegisterStoreEraser(e)
		}()
	}
	_, un := RegisterStoreEraser(StoreEraser{Name: "s", Source: "a", EraseResolved: resolved})
	un()
}

// Identity reports a kind as resolved only when it carries a value: an
// erasure records nothing for a kind it could not resolve, and an empty
// value would match nothing a store keys on.
func TestErasedUserIdentity(t *testing.T) {
	u := ErasedUser{ID: "u1", Identities: map[IdentityKind]string{IdentityEmail: "a@example.com"}}
	if v, ok := u.Identity(IdentityEmail); !ok || v != "a@example.com" {
		t.Errorf("resolved email = %q, %v", v, ok)
	}
	if _, ok := (ErasedUser{ID: "u1"}).Identity(IdentityEmail); ok {
		t.Error("an unresolved kind reads as resolved")
	}
	if _, ok := (ErasedUser{Identities: map[IdentityKind]string{IdentityEmail: ""}}).Identity(IdentityEmail); ok {
		t.Error("an empty value reads as resolved")
	}
}
