package datexport

import (
	"context"
	"testing"
)

func nopErase(context.Context, string, bool) (int, error) { return 0, nil }

// Two stores that register one Name: the second replaces the first, and
// the first one's Close must not take the second's registration with it.
func TestStaleUnregisterKeepsReplacement(t *testing.T) {
	Reset(t)
	t.Cleanup(func() { Reset(t) })
	first := RegisterStoreEraser(StoreEraser{Name: "s", Source: "a", Erase: nopErase})
	second := RegisterStoreEraser(StoreEraser{Name: "s", Source: "b", Erase: nopErase})
	first()
	got := AllStoreErasers()
	if len(got) != 1 || got[0].Source != "b" {
		t.Fatalf("after the replaced entry unregistered: %+v, want the source b entry", got)
	}
	second()
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
