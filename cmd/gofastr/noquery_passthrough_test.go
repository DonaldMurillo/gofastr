package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// TestNoQueryReachesGeneratedField guards the silent-drop failure mode: every
// schema.Field flag has to be named explicitly in renderFieldLiteral, so a new
// one that nobody adds there is accepted in the blueprint, validated, and then
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
		t.Errorf("generated field literal %s drops NoQuery — the blueprint would declare a "+
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

// TestNoQuerySearchFieldRejected pins the blueprint-side half of the Define
// panic: ?q= search matches on the stored value, so a NoQuery column listed in
// search_fields would hand back exactly what NoQuery keeps off the query
// surface.
func TestNoQuerySearchFieldRejected(t *testing.T) {
	yaml := `
app:
  name: cards
entities:
  - name: cards
    search_fields: [number]
    fields:
      - name: number
        type: string
        no_query: true
`
	_, err := covT_decode(t, yaml)
	if err == nil {
		t.Fatal("search_fields on a no_query column must be rejected")
	}
	if !strings.Contains(err.Error(), "no_query") {
		t.Errorf("error %q should explain that the column is no_query", err)
	}
}

// TestNoQueryAcceptedInBlueprint pins that the key is in the field allow-list:
// rejectUnknownKeys fails closed, so an unlisted key is a hard error rather
// than a silent ignore.
func TestNoQueryAcceptedInBlueprint(t *testing.T) {
	yaml := `
app:
  name: cards
entities:
  - name: cards
    fields:
      - name: number
        type: string
        no_query: true
`
	bp, err := covT_decode(t, yaml)
	if err != nil {
		t.Fatalf("no_query must be an accepted field key: %v", err)
	}
	// Find the field first. A loop that only asserts "if f.Name == number"
	// passes when the decoder drops the field, which loses the column and its
	// protection together.
	var found *framework.FieldDeclaration
	for i := range bp.Entities {
		for j := range bp.Entities[i].Fields {
			if bp.Entities[i].Fields[j].Name == "number" {
				found = &bp.Entities[i].Fields[j]
			}
		}
	}
	if found == nil {
		t.Fatalf("the no_query field was dropped by the decoder entirely: %#v", bp.Entities)
	}
	if !found.NoQuery {
		t.Error("no_query: true decoded to NoQuery=false")
	}
}

// TestNoQuerySurvivesPackRoundTrip covers the other half of the silent-drop
// class. pack.go's own header states the invariant that both the decoder and
// the serializer must learn every new construct, but the round-trip test
// compares fixtures, and no fixture uses no_query, so an omission here is
// vacuously "covered". Losing the flag on a pack/regenerate cycle turns a
// masked column back into a filterable one.
func TestNoQuerySurvivesPackRoundTrip(t *testing.T) {
	m := fieldToMap(framework.FieldDeclaration{
		Name:    "number",
		Type:    "string",
		NoQuery: true,
	})
	if v, ok := m["no_query"]; !ok || v != true {
		t.Errorf("fieldToMap dropped no_query: %+v — pack(generate(yml)) would strip the "+
			"protection and regenerating makes the column filterable again", m)
	}

	plain := fieldToMap(framework.FieldDeclaration{Name: "label", Type: "string"})
	if _, ok := plain["no_query"]; ok {
		t.Errorf("fieldToMap emits no_query for a normal field: %+v", plain)
	}
}

// TestPackReadsNoQueryFromSource pins the AST reader half: pack parses
// generated Go back into a declaration, and a flag it does not read is a flag
// the packed YAML cannot carry.
func TestPackReadsNoQueryFromSource(t *testing.T) {
	src := `package p
var x = []schema.Field{
	{Name: "number", Type: schema.String, NoQuery: true},
	{Name: "label", Type: schema.String},
}`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var lit ast.Expr
	ast.Inspect(file, func(n ast.Node) bool {
		if cl, ok := n.(*ast.CompositeLit); ok && lit == nil {
			lit = cl
			return false
		}
		return true
	})
	if lit == nil {
		t.Fatal("no composite literal found")
	}

	fields := packReadFields(lit)
	if len(fields) != 2 {
		t.Fatalf("packReadFields returned %d fields, want 2", len(fields))
	}
	if !fields[0].NoQuery {
		t.Error("packReadFields ignored NoQuery: true in the source literal")
	}
	if fields[1].NoQuery {
		t.Error("packReadFields invented NoQuery on a plain field")
	}
}

// blueprintWithNoQuery is a minimal app whose one entity has a masked column,
// used to drive the generator end to end. No fixture in the repo declares
// no_query, so without this the whole emission path was covered only by unit
// tests on individual render functions.
const blueprintWithNoQuery = `
app:
  name: cards
entities:
  - name: cards
    fields:
      - name: label
        type: string
      - name: number
        type: string
        no_query: true
`

// TestNoQueryReachesGeneratedScreens is the end-to-end guard for the
// generator's half: a no_query column stays in the emitted entity (the
// field literal keeps the flag, pinned by TestNoQueryReachesGeneratedField
// above) and the generated screens carry no per-screen column config at
// all — the list renders through appUI.List, and refusing to sort on a
// no_query column is entityui's half (pinned against entityui in the
// framework's own suite).
func TestNoQueryReachesGeneratedScreens(t *testing.T) {
	bp, err := covT_decode(t, blueprintWithNoQuery+`
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
	files := mustRenderBlueprintFiles(t, bp)
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

// TestNoQuerySearchBlockRejected: entity_list search: is gone with the
// key; the refusal names the entity's search_fields:, which itself
// refuses a no_query column (pinned by TestNoQuerySearchFieldRejected
// above), so the oracle the old screen key opened stays closed.
func TestNoQuerySearchBlockRejected(t *testing.T) {
	yaml := blueprintWithNoQuery + `
screens:
  - name: cards
    route: /cards
    body:
      - kind: entity_list
        entity: cards
        fields: [label]
        search: number
`
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, yaml)
	_, err := loadBlueprint(path)
	if err == nil {
		t.Fatal("entity_list search: must be refused at decode")
	}
	if !strings.Contains(err.Error(), "search_fields:") {
		t.Fatalf("error should name the entity's search_fields:, got: %v", err)
	}
}

// TestSearchFieldsAcceptsPlainColumns is the false-positive guard for the
// replacement: an ordinary string column is a valid search_fields entry.
func TestSearchFieldsAcceptsPlainColumns(t *testing.T) {
	bp, err := covT_decode(t, blueprintWithNoQuery)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	bp.Entities[0].SearchFields = []string{"label"}
	if err := validateBlueprint(bp); err != nil {
		t.Errorf("search_fields: label must stay valid, got %v", err)
	}
}

// TestStatCardNoQueryFilterRejected covers the nested-source path. The
// stat_card filter lives in source.filter, not in the block's own props, and
// the first version of this guard read the wrong map, so it validated
// nothing. statValue splits the string into a raw ParsedFilter and hands it
// to CountAll, bypassing the filter parser, so a masked column there makes
// the rendered count a one-bit oracle.
func TestStatCardNoQueryFilterRejected(t *testing.T) {
	yaml := blueprintWithNoQuery + `
screens:
  - name: dash
    route: /dash
    body:
      - kind: stat_card
        props:
          label: Matching
          source:
            entity: cards
            agg: count
            filter: number=4111
`
	bp, err := covT_decode(t, yaml)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	err = validateBlueprint(bp)
	if err == nil {
		t.Fatal("stat_card source.filter on a no_query column must be rejected")
	}
	if !strings.Contains(err.Error(), "no_query") {
		t.Errorf("error %q should explain the column is no_query", err)
	}
}

// TestStatCardPlainFilterAccepted is the false-positive guard.
func TestStatCardPlainFilterAccepted(t *testing.T) {
	yaml := blueprintWithNoQuery + `
screens:
  - name: dash
    route: /dash
    body:
      - kind: stat_card
        props:
          label: Matching
          source:
            entity: cards
            agg: count
            filter: label=gold
`
	bp, err := covT_decode(t, yaml)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := validateBlueprint(bp); err != nil {
		t.Errorf("a plain column filter must stay valid, got %v", err)
	}
}

// The screen-level facet key is refused at decode, naming the entity's
// display: facets: — the entity-level spelling, whose facets run through
// the entityui list's own query-surface bar.
func TestEntityListNoQueryFacetRejected(t *testing.T) {
	yaml := blueprintWithNoQuery + `
screens:
  - name: cards
    route: /cards
    body:
      - kind: entity_list
        entity: cards
        fields: [label, number]
        filters: [number]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, yaml)
	_, err := loadBlueprint(path)
	if err == nil {
		t.Fatal("entity_list filters: must be refused at decode")
	}
	if !strings.Contains(err.Error(), "display:") || !strings.Contains(err.Error(), "facets:") {
		t.Fatalf("error should name display: facets:, got: %v", err)
	}
}
