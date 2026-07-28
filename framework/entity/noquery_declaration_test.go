package entity

import "testing"

func TestFieldDeclarationPreservesNoQuery(t *testing.T) {
	field, err := (FieldDeclaration{Name: "number", Type: "string", NoQuery: true}).Field()
	if err != nil {
		t.Fatal(err)
	}
	if !field.NoQuery {
		t.Fatal("FieldDeclaration.Field dropped NoQuery")
	}
}
