package datexport

import (
	"reflect"
	"testing"
)

func TestSweepDatexportAllErasersKeepsEveryField(t *testing.T) {
	Reset(t)
	t.Cleanup(func() { Reset(t) })
	in := DataEraser{
		Name: "flags", Source: "featureflag", Table: "flags", Column: "users",
		Mode: EraseRemoveMember, ScrubColumns: []string{"a"}, Tombstone: "x",
		Identity: IdentityEmail, ValuePrefix: "p:", Where: []Equals{{Column: "k", Value: "v"}},
		MemberSeparator: ":",
	}
	want := in
	want.Where = []Equals{{Column: "k", Value: "v"}}
	RegisterEraser(in)
	in.Where[0].Value = "changed"
	if got := AllErasers(); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("AllErasers = %+v, want %+v", got, want)
	}
}

func TestSweepDatexportNameRefusesDots(t *testing.T) {
	for _, name := range []string{"..", "a.b", "."} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Register accepted Name %q", name)
				}
			}()
			Register(DataExporter{Name: name, Table: "t"})
		}()
	}
}

func TestSweepDatexportRegisterLeavesCallerColumns(t *testing.T) {
	Reset(t)
	t.Cleanup(func() { Reset(t) })
	cols := make([]string, 1, 4)
	cols[0] = "body"
	Register(DataExporter{Name: "n", Table: "t", PrimaryKey: "id", Columns: cols})
	if spare := cols[:2][1]; spare != "" {
		t.Fatalf("Register wrote %q into the caller's Columns array", spare)
	}
}

func TestSweepDatexportUnregisterRemovesOnlyItsOwn(t *testing.T) {
	Reset(t)
	t.Cleanup(func() { Reset(t) })
	_, first := RegisterStoreEraser(StoreEraser{Name: "s", Erase: nopErase})
	first()
	RegisterStoreEraser(StoreEraser{Name: "s", Erase: nopErase})
	first()
	if n := len(AllStoreErasers()); n != 1 {
		t.Fatalf("a stale unregister removed a later store of the same name: %d left", n)
	}
}

func TestSweepDatexportResetClearsStoreErasers(t *testing.T) {
	RegisterStoreEraser(StoreEraser{Name: "s", Erase: nopErase})
	Reset(t)
	if n := len(AllStoreErasers()); n != 0 {
		t.Fatalf("%d store erasers survived Reset", n)
	}
}
