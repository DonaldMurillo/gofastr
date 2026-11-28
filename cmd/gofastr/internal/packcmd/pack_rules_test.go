package packcmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"
)

const ownedTasks = "app:\n  name: p\nentities:\n  - name: tasks\n    table: tasks\n    scope:\n      owner_field: user_id\n    fields:\n      - name: title\n        type: string\n"

func TestPackDropsTheSynthesizedOwnerColumn(t *testing.T) {
	bp, err := decodeAppSpecString(ownedTasks)
	if err != nil {
		t.Fatal(err)
	}
	owner := bp.Entities[0].Fields[0]
	owner.Name, owner.Hidden = "user_id", true
	bp.Entities[0].Fields = append(bp.Entities[0].Fields, owner)
	out, err := encodeAppSpecYAML(bp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "name: user_id") {
		t.Fatalf("pack wrote back the owner column the generator synthesizes:\n%s", out)
	}
	owner.Hidden = false
	bp.Entities[0].Fields[1] = owner
	if out, _ = encodeAppSpecYAML(bp); !strings.Contains(out, "name: user_id") {
		t.Fatalf("pack dropped an owner field the author declared:\n%s", out)
	}
}

func TestPackFloatStaysAFloat(t *testing.T) {
	bp, err := decodeAppSpecString(ownedTasks)
	if err != nil {
		t.Fatal(err)
	}
	bp.Entities[0].Properties = map[string]any{"price": float64(99), "list": []any{map[string]any{"a": "b"}, float64(2)}}
	out, err := encodeAppSpecYAML(bp)
	if err != nil {
		t.Fatal(err)
	}
	back, err := decodeAppSpecString(out)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := back.Entities[0].Properties["price"].(float64); !ok || v != 99 {
		t.Fatalf("99.0 came back as %#v (%T)\n%s", back.Entities[0].Properties["price"], back.Entities[0].Properties["price"], out)
	}
}

func TestReverseEntityUIStopsOnARecursiveHelper(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "screens.go", "package screens\nfunc a() interface{} { return a() }\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	helpers := packHelperReturns(f)
	done := make(chan bool)
	go func() {
		_, ok := reverseEntityUI(&ast.CallExpr{Fun: &ast.Ident{Name: "a"}}, helpers)
		done <- ok
	}()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("a helper that returns itself reversed to a block")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reverseEntityUI followed a self-referential helper for ever")
	}
}
