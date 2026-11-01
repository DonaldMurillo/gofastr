package datexport

import (
	"context"
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
