package render

import (
	"context"
	"strings"
	"testing"
)

func TestSweepRenderAttrsSortedNoDanglingSpace(t *testing.T) {
	for i := 0; i < 20; i++ {
		got := Tag("i", map[string]string{"f": "6", "onclick": "x", "e": "5", "d": "4", "c": "3", "b": "2", "a": "1"}).String()
		if want := `<i a="1" b="2" c="3" d="4" e="5" f="6"></i>`; got != want {
			t.Fatalf("Tag = %s, want %s", got, want)
		}
	}
	if got := Tag("i", map[string]string{"onclick": "x"}).String(); got != "<i></i>" {
		t.Errorf("single dropped attr = %s, want <i></i>", got)
	}
	if got := VoidTag("br", map[string]string{"x y": "1"}).String(); got != "<br>" {
		t.Errorf("void dropped attr = %s, want <br>", got)
	}
}

func TestSweepRenderRegistriesRefuseNil(t *testing.T) {
	for name, f := range map[string]func(){
		"component":      func() { RegisterComponent("sweep-nil", nil) },
		"layout":         func() { RegisterLayout("sweep-nil", nil) },
		"func":           func() { RegisterFunc("sweep-nil", nil) },
		"unknown layout": func() { RenderWithLayout("sweep-missing", "t", "") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			f()
		}()
	}
}

func TestSweepRenderNamespaceReservesInOuterScope(t *testing.T) {
	page := WithIDScope(context.Background())
	ns := WithIDNamespace(page, "cart")
	if got := UniqueID(ns, "x"); got != "cart--x" {
		t.Fatalf("namespaced id = %q", got)
	}
	ReserveID(ns, "plans")
	if got := UniqueID(page, "cart--x"); got != "cart--x-2" {
		t.Errorf("page repeat of a namespaced derived id = %q, want cart--x-2", got)
	}
	if got := UniqueID(page, "plans"); got != "plans-2" {
		t.Errorf("page repeat of a namespaced explicit id = %q, want plans-2", got)
	}
}

func TestSweepRenderRegionIDKeepsItsEscaperBytes(t *testing.T) {
	cssIdent := func(s string) string { return strings.ReplaceAll(s, ".", `\.`) }
	ctx := WithIDScope(context.Background())
	b := NewRegionBatch(ctx, 1)
	h := HTML("#" + cssIdent(UniqueID(b.Context(0), "cart.view")))
	if got := b.Settle(0)(h); got != `#cart\.view` {
		t.Errorf("settled = %q, want #cart\\.view", got)
	}
}
