package appspec

import (
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
)

// ── appspec.go scalar/value helpers ─────────────────────────────────

func sc(v any) *coreyaml.Node { return &coreyaml.Node{Kind: coreyaml.Scalar, Value: v} }

func lst(n ...*coreyaml.Node) *coreyaml.Node {
	return &coreyaml.Node{Kind: coreyaml.List, List: n}
}

func mp(m map[string]*coreyaml.Node) *coreyaml.Node {
	return &coreyaml.Node{Kind: coreyaml.Map, Map: m}
}

func TestAppSpecScalarHelpers(t *testing.T) {
	// boolValue
	if !boolValue(sc(true)) || boolValue(sc("false")) {
		t.Fatal("boolValue bool")
	}
	if !boolValue(sc("TRUE")) {
		t.Fatal("boolValue string true")
	}
	if boolValue(nil) || boolValue(lst()) {
		t.Fatal("boolValue non-scalar")
	}
	// intValue
	if intValue(sc(int64(5))) != 5 || intValue(sc(float64(3.9))) != 3 {
		t.Fatal("intValue numeric")
	}
	if intValue(sc("x")) != 0 || intValue(nil) != 0 {
		t.Fatal("intValue non-numeric")
	}
	// floatValue
	if floatValue(sc(int64(2))) != 2 || floatValue(sc(float64(1.5))) != 1.5 {
		t.Fatal("floatValue numeric")
	}
	if floatValue(sc("x")) != 0 || floatValue(nil) != 0 {
		t.Fatal("floatValue non-numeric")
	}
	// scalarValue
	if scalarValue(sc("hi")) != "hi" || scalarValue(lst()) != nil {
		t.Fatal("scalarValue")
	}
}

func TestExpectMapAndList(t *testing.T) {
	if _, err := expectMap(nil, "x"); err == nil {
		t.Fatal("nil map should error")
	}
	if _, err := expectMap(lst(), "x"); err == nil {
		t.Fatal("list-as-map should error")
	}
	if _, err := expectMap(mp(nil), "x"); err != nil {
		t.Fatalf("valid map: %v", err)
	}
	if l, err := expectList(nil, "x"); err != nil || l != nil {
		t.Fatal("nil list → nil,nil")
	}
	if _, err := expectList(sc("x"), "y"); err == nil {
		t.Fatal("scalar-as-list should error")
	}
	if _, err := expectList(lst(sc("a")), "y"); err != nil {
		t.Fatalf("valid list: %v", err)
	}
}

func TestRelationTypeFromStringAndScreenType(t *testing.T) {
	good := map[string]framework.RelationType{
		"": framework.RelManyToOne, "belongs_to": framework.RelManyToOne,
		"has_one": framework.RelHasOne, "has_many": framework.RelHasMany,
		"many_to_many": framework.RelManyToMany,
	}
	for in, want := range good {
		got, err := relationTypeFromString(in)
		if err != nil || got != want {
			t.Errorf("relationTypeFromString(%q)=%v,%v want %v", in, got, err, want)
		}
	}
	if _, err := relationTypeFromString("bogus"); err == nil {
		t.Fatal("bad relation type")
	}

	st := map[string]string{
		"": "app.ScreenPage", "page": "app.ScreenPage", "drawer": "app.ScreenDrawer",
		"sheet": "app.ScreenSheet", "dialog": "app.ScreenDialog", "modal": "app.ScreenDialog",
	}
	for in, want := range st {
		got, err := ScreenTypeConst(in)
		if err != nil || got != want {
			t.Errorf("screenTypeConst(%q)=%q,%v want %q", in, got, err, want)
		}
	}
	if _, err := ScreenTypeConst("bogus"); err == nil {
		t.Fatal("bad screen type")
	}
}

func TestAppSpecThemeColorPathAll(t *testing.T) {
	keys := []string{"primary", "primary-fg", "secondary", "background", "surface",
		"surface-soft", "text", "text-muted", "text-subtle", "border", "border-strong",
		"accent", "success", "warning", "danger", "danger-fg", "info"}
	for _, k := range keys {
		if _, ok := AppSpecThemeColorPath(k); !ok {
			t.Errorf("appSpecThemeColorPath(%q) not ok", k)
		}
	}
	if _, ok := AppSpecThemeColorPath("nonsense"); ok {
		t.Fatal("unknown key should be !ok")
	}
}
