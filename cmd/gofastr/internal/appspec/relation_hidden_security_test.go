package appspec

import (
	"fmt"
	"go/ast"
	"go/parser"
	"strings"
	"testing"
)

func TestRelationHiddenRejectsNonBools(t *testing.T) {
	for _, value := range []string{"yes", "on", "1", "null", "[]"} {
		_, err := loadBPWithEntity(t, fmt.Sprintf("  - name: users\n    fields:\n      - name: name\n        type: string\n    relations:\n      - type: has_many\n        name: notes\n        entity: notes\n        foreign_key: user_id\n        hidden: %s\n  - name: notes\n    fields:\n      - name: user_id\n        type: string\n", value))
		if err == nil || !strings.Contains(err.Error(), "hidden") {
			t.Errorf("hidden: %s: want error naming hidden, got %v", value, err)
		}
	}
}

func TestPackKeepsRelationHidden(t *testing.T) {
	call, err := parser.ParseExpr(`app.Entity("users", framework.EntityConfig{Relations: []framework.Relation{{Type: framework.RelHasMany, Name: "notes", Entity: "notes", Hidden: true}}})`)
	if err != nil {
		t.Fatal(err)
	}
	decl, err := PackEntityDeclFromCall(call.(*ast.CallExpr))
	if err != nil {
		t.Fatal(err)
	}
	if len(decl.Relations) != 1 || !decl.Relations[0].Hidden {
		t.Fatalf("packed relation lost Hidden: %+v", decl.Relations)
	}
}
