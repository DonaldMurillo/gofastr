// Package ei holds the emitident positives, reduced from the pre-fix
// code the probes broke (7bd789e9; fixes 29219c04 / f06f4412 / e936f791)
// with the real API names kept. Nothing in this package validates or
// quotes a name: that is the pre-fix state. The fixed spellings live in
// package eifix, separately, because the analyzer's package-scoped
// field memo would otherwise silence these on purpose.
package ei

import (
	"fmt"
	"strings"
)

// ---- Go declaration slots ----------------------------------------------

type cliField struct {
	Flag string
}

// toCamelCase is deliberately NOT a gate: it changes case, it does not
// validate. The fix wrapped the derivation in token.IsIdentifier
// instead of trusting the casing.
func toCamelCase(s string) string {
	return strings.ToUpper(s[:1]) + s[1:]
}

// renderFilterFlag is generate_cli.go's pre-fix shape: the derived name
// lands in `flt%s :=` with only toCamelCase in front of it
// (TestCLIFieldPayloadBecomesStatements).
func renderFilterFlag(sb *strings.Builder, f cliField) {
	fmt.Fprintf(sb, "\tflt%s := fs.String(%q, \"\", %q)\n", toCamelCase(f.Flag), f.Flag, "filter") // want `identifier slot "flt%s :="`
}

type hook struct {
	Handler string
}

// renderHookStub is the blueprint hook-stub shape the fix added WITH its
// gate; ungated is the bug (TestHookHandlerMustDeriveIdentifier).
func renderHookStub(sb *strings.Builder, h hook) {
	fmt.Sprintf("func %s(ctx context.Context, data any) error {\n", h.Handler) // want `identifier slot "func %s"`
}

// ---- SQL DDL slots ------------------------------------------------------

type column struct {
	Name string
}

// alterAddColumn is kiln/db/migrate.go pre-fix: raw table and column
// names into ALTER TABLE / ADD COLUMN (TestAlterTableQuotesHostileIdentifiers).
func alterAddColumn(table string, f column) string {
	col := f.Name + " " + "TEXT"
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table, col) // want `identifier slot "alter table %s"`
}

func tableColumns(table string) string {
	return fmt.Sprintf("PRAGMA table_info(%s)", table) // want `identifier slot "pragma table_info\(%s"`
}

func createTable(table string) string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id TEXT)", table) // want `identifier slot "create table if not exists %s"`
}

// ---- CSS string slots ---------------------------------------------------

type webFont struct {
	Family   string
	Style    string
	FileName string
}

// FontFaceCSS is core-ui/style/fontface.go pre-fix: family and file name
// into the single-quoted slots of an @font-face rule
// (TestFontFaceCSSRejectsDeclarationBreakers).
func FontFaceCSS(dir string, fonts []webFont) string {
	var b strings.Builder
	for _, f := range fonts {
		fmt.Fprintf(&b, // want `identifier slot "'%s'"`
			"@font-face { font-family: '%s'; font-style: %s; src: url('%s/%s.woff2') }\n",
			f.Family, f.Style, dir, f.FileName)
	}
	return b.String()
}

// ---- route path slots ---------------------------------------------------

// renderPath is generate_client.go's `path := "/%s"` shape
// (TestSDKSpecRefusesHostileDeclarations): the table lands raw in the
// generated client's route literal.
func renderPath(table string) string {
	return fmt.Sprintf("path := \"/%s\"\n", table) // want `/%s" of emitted code`
}

// renderBehaviorURL is the /__gofastr/widget/ shape from
// TestWidgetBehaviorURLMatchesRuntimeGate, in its Sprintf spelling.
func renderBehaviorURL(id string) string {
	return fmt.Sprintf("data-behavior=\"/__gofastr/widget/%s.js\"", id) // want `identifier slot "/__gofastr/widget/%s"`
}
