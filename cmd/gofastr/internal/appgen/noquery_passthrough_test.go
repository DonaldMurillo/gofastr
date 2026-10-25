package appgen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestNoQueryReachesGeneratedField guards the silent-drop failure mode: every
// schema.Field flag has to be named explicitly in renderFieldLiteral, so a new
// one that nobody adds there is accepted in the app spec, validated, and then
// quietly discarded. For NoQuery that would generate an app whose masked
// column is still filterable: the declaration promises a protection the
// generated code does not have.
func TestNoQueryReachesGeneratedField(t *testing.T) {
	literal, err := renderFieldLiteral(framework.FieldDeclaration{
		Name:    "card",
		Type:    "string",
		NoQuery: true,
	})
	if err != nil {
		t.Fatalf("renderFieldLiteral: %v", err)
	}
	if !strings.Contains(literal, "NoQuery: true") {
		t.Errorf("generated field literal %s drops NoQuery — the app spec would declare a "+
			"protection the generated app does not enforce", literal)
	}
}

// TestNoQueryOmittedWhenUnset keeps the generated source clean for the common
// case, matching how the other flags render.
func TestNoQueryOmittedWhenUnset(t *testing.T) {
	literal, err := renderFieldLiteral(framework.FieldDeclaration{Name: "title", Type: "string"})
	if err != nil {
		t.Fatalf("renderFieldLiteral: %v", err)
	}
	if strings.Contains(literal, "NoQuery") {
		t.Errorf("literal %s should omit NoQuery when unset", literal)
	}
}

// TestNoQueryReachesGeneratedScreens is the end-to-end guard for the
// generator's half: a no_query column stays in the emitted entity (the
// field literal keeps the flag, pinned by TestNoQueryReachesGeneratedField
// above) and the generated screens carry no per-screen column config at
// all — the list renders through appUI.List, and refusing to sort on a
// no_query column is entityui's half (pinned against entityui in the
// framework's own suite).
func TestNoQueryReachesGeneratedScreens(t *testing.T) {
	bp, err := covT_decode(t, clitest.AppSpecWithNoQuery+`
screens:
  - name: cards
    route: /cards
    body:
      - kind: entity_list
        entity: cards
        fields: [label, number]
`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	files := mustRenderAppSpecFiles(t, bp)
	crud := fileContent(files, "screen_cards_crud.go")
	if !strings.Contains(crud, `appUI.List("cards").Columns("label", "number")`) {
		t.Fatalf("the cards screen must render the no_query column through appUI.List:\n%s", crud)
	}
	if strings.Contains(crud, "NoQuery") {
		t.Errorf("screens carry no per-screen column flags any more:\n%s", crud)
	}
	ent := fileContent(files, filepath.Join("entities", "cards.go"))
	if !strings.Contains(ent, "NoQuery: true") {
		t.Fatalf("the entity registration must keep the flag entityui reads:\n%s", ent)
	}
}
