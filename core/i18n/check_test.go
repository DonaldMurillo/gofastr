package i18n

import (
	"slices"
	"testing"
)

var (
	checkGreet = Define("checktest.greet", "Hello, {{name}}")
	checkCount = Define("checktest.count", "{{n}} results for {{q}}")
	checkWord  = Define("checktest.word", "Save")
	checkGlyph = Define("checktest.glyph", "…")
)

func TestCheckCatalogFindsEachProblem(t *testing.T) {
	c := NewMapCatalog()
	c.Set("fr", "checktest.greet", Message{Text: "Bonjour, {{nom}}"})
	c.Set("fr", "checktest.count", Message{Plural: map[string]string{
		"one":   "Un résultat pour {{q}}",
		"other": "{{n}} résultats pour {{q}}",
	}})
	c.Set("fr", "checktest.gone", Message{Text: "Parti"})
	c.Set("fr", "entity.invoices.singular", Message{Text: "Facture"})

	got := CheckCatalog(c, "fr", Skip("entity."), Require("checktest."))
	var summary []string
	for _, is := range got {
		summary = append(summary, is.Key+" "+string(is.Problem))
	}
	want := []string{
		"checktest.gone unknown key",
		"checktest.greet placeholders differ",
		"checktest.word missing",
	}
	if !slices.Equal(summary, want) {
		t.Fatalf("issues = %q\nwant     %q", summary, want)
	}
}

func TestCheckCatalogPluralMustCoverEnglish(t *testing.T) {
	c := NewMapCatalog()
	c.Set("de", "checktest.count", Message{Plural: map[string]string{
		"one":   "Ein Ergebnis",
		"other": "{{n}} Ergebnisse",
	}})
	got := CheckCatalog(c, "de")
	if len(got) != 1 || got[0].Problem != ProblemPlaceholders {
		t.Fatalf("plural forms dropping {{q}} everywhere = %+v, want one placeholder issue", got)
	}
}

func TestCheckCatalogCleanAndNil(t *testing.T) {
	c := NewMapCatalog()
	c.Set("es", "checktest.greet", Message{Text: "Hola, {{name}}"})
	if got := CheckCatalog(c, "es"); len(got) != 0 {
		t.Fatalf("clean catalog reported %+v", got)
	}
	if got := CheckCatalog(nil, "es"); len(got) != 0 {
		t.Fatalf("nil catalog without Require reported %+v", got)
	}
}

func TestPlaceholdersSortedDistinct(t *testing.T) {
	got := Placeholders("{{b}} and {{ a }} and {{b}} and {single} and {{open")
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("Placeholders = %q", got)
	}
}
