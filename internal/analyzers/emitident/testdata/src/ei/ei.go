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

// ---- round 2: prose defenses and deeper slots -------------------------

// e1/e2: a format-INITIAL keyword in a lowercase message is prose, not
// a declaration — no positive code evidence after the verb.
func e1(kind string) string {
	return fmt.Sprintf("type %s is not a struct", kind)
}

func e2(fn string) string {
	return fmt.Sprintf("func %s(ctx) was replaced by New; kept for compat", fn)
}

// e3: a deeper route segment rewrites the route the same way the
// first one does.
func e3(table string) string {
	return fmt.Sprintf("path := \"/api/v1/%s\"\n", table) // want `identifier slot ".*/api/v1/%s"`
}

// e4/e5: index and INSERT identifier positions are the same breakout
// slot the table spellings are.
func e4(table string) string {
	return fmt.Sprintf("CREATE INDEX idx_%s ON %s(col)", table, table) // want `identifier slot "create index %s"`
}

func e5(table string) string {
	return fmt.Sprintf("INSERT INTO %s(a) VALUES (1)", table) // want `identifier slot "insert into %s"`
}

// e6: a CSS string slot outside @font-face — the quote-breakout works
// identically in content.
func e6(text string) string {
	return fmt.Sprintf("a.hint::after { content: '%s' }", text) // want `identifier slot "'%s'"`
}

func isGoIdentifier(value string) bool {
	for _, r := range value {
		if !(r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return value != ""
}

func warned(format string, args ...any) {}

// e7: a check that only warns does not gate — warn-and-continue is
// not rejection; the hostile name still reaches the Fprintf.
func e7(sb *strings.Builder, hook string) {
	if !isGoIdentifier(hook) {
		warned("ignoring invalid hook name %q", hook)
	}
	fmt.Fprintf(sb, "func %s(ctx context.Context, data any) error {\n", hook) // want `identifier slot "func %s"`
}

// e8: a struct field whose NAME says type is not a gate — nothing
// validated the value.
type decl struct {
	HandlerType string
}

func e8(d decl) string {
	return fmt.Sprintf("func %s(ctx context.Context) error {\n", d.HandlerType) // want `identifier slot "func %s"`
}

// e9: a suffix on the emitted identifier — "func %sHandler(ctx
// context.Context) error {" — is the same declaration slot; the
// parenthesis follows the identifier RUN, not the verb.
func e9(hook string) string {
	return fmt.Sprintf("func %sHandler(ctx context.Context) error {\n", hook) // want `identifier slot "func %s"`
}

// e10: continue only skips the current iteration. The post-loop emit
func e10(names []string, sb *strings.Builder) {
	var n string
	for _, n = range names {
		if !isGoIdentifier(n) {
			continue
		}
		fmt.Fprintf(sb, "func %s(ctx context.Context) error {\n", n)
	}
	fmt.Sprintf("func %s(ctx context.Context) error {\n", n) // want `identifier slot "func %s"`
}

// emitType/emitVar: format-initial declarations WITH code evidence
// after the verb still fire.
func emitType(name string) string {
	return fmt.Sprintf("type %s struct {\n\tID string\n}\n", name) // want `identifier slot "type %s"`
}

func emitVar(name string) string {
	return fmt.Sprintf("var %s = 1\n", name) // want `identifier slot "var %s"`
}
