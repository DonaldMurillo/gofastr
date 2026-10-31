package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// An entity whose interface name client.d.ts already declares overwrote
// the runtime's entry in the name check, so `export interface Resource`
// shipped beside the runtime's own: a compile error for the generic
// CursorPage<T>, a silent merge of two shapes for the rest (2026-10-27
// G2). Each entity-derived name is checked like an endpoint's.
func TestSDKRefusesEntityTypeClash(t *testing.T) {
	for _, tc := range []struct{ tables []string }{
		{[]string{"resource"}},
		{[]string{"client"}},
		{[]string{"cursor_page"}},
		{[]string{"order", "order_input"}},
	} {
		decls := statesFixtureDecls()
		for _, table := range tc.tables {
			decls = append(decls, framework.EntityDeclaration{
				Name: table, Table: table,
				Fields: []framework.FieldDeclaration{{Name: "name", Type: "string"}},
			})
		}
		_, err := buildSDKSpec(decls, &SdkOptions{})
		if err == nil || !strings.Contains(err.Error(), "already declared by") {
			t.Errorf("entities %v: %v", tc.tables, err)
		}
	}

	// OrderMoves and OrderEndpoints are declared only for an order with
	// moves or endpoints, so this pair never clashes.
	decls := statesFixtureDecls()
	for _, table := range []string{"order", "order_moves", "order_endpoints"} {
		decls = append(decls, framework.EntityDeclaration{
			Name: table, Table: table,
			Fields: []framework.FieldDeclaration{{Name: "name", Type: "string"}},
		})
	}
	if _, err := buildSDKSpec(decls, &SdkOptions{}); err != nil {
		t.Errorf("an order with no moves beside order_moves: %v", err)
	}
}
