// Package eifix holds the emitident fixed spellings and deliberate
// silences: every positive of package ei, in the form its fix shipped.
package eifix

import (
	"fmt"
	"go/token"
	"strings"
)

type cliField struct {
	Flag string
}

func toCamelCase(s string) string {
	return strings.ToUpper(s[:1]) + s[1:]
}

// buildCLIField identifier-checks the flag's derivation at the package's
// boundary. With the field checked here, the emitter below renders
// through toCamelCase and the analyzer's package field memo stays quiet:
// the gate may live in the validator while the emitter only renders.
func buildCLIField(f cliField) error {
	if !token.IsIdentifier(toCamelCase(f.Flag)) {
		return fmt.Errorf("field %s does not derive a valid identifier", f.Flag)
	}
	return nil
}

func renderFilterFlag(sb *strings.Builder, f cliField) {
	fmt.Fprintf(sb, "\tflt%s := fs.String(%q, \"\", %q)\n", toCamelCase(f.Flag), f.Flag, "filter")
}

type hook struct {
	Handler string
}

func isGoIdentifier(value string) bool {
	for _, r := range value {
		if !(r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return value != ""
}

// renderHookStubFixed is the blueprint fix's own shape: an isGoIdentifier
// guard on the very local the emitter prints.
func renderHookStubFixed(sb *strings.Builder, h hook) {
	handler := strings.TrimSpace(h.Handler)
	if !isGoIdentifier(handler) {
		return
	}
	fmt.Sprintf("func %s(ctx context.Context, data any) error {\n", handler)
}

// QuoteIdent stands in for core/query.QuoteIdent.
func QuoteIdent(v string) string { return `"` + strings.ReplaceAll(v, `"`, `""`) + `"` }

type column struct {
	Name string
}

// alterAddColumnFixed is kiln's fix: every identifier routed through the
// quoting helper, type keywords accepted as gated parts of the column
// definition.
func alterAddColumnFixed(table string, f column) string {
	col := QuoteIdent(f.Name) + " " + sqlType()
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", QuoteIdent(table), col)
}

func sqlType() string { return "TEXT" }

func tableColumnsFixed(table string) string {
	return fmt.Sprintf("PRAGMA table_info(%s)", QuoteIdent(table))
}

func createTableFixed(table string) string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id TEXT)", QuoteIdent(table))
}

type webFont struct {
	Family   string
	Style    string
	FileName string
}

// sanitizeSlot is fontface.go's quotedSlotSanitized.
func sanitizeSlot(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch r {
		case '\'', '"', '\\', ';', '{', '}', '<', '>', '\n', '\r':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// FontFaceCSSFixed: every quoted-slot argument arrives sanitized.
func FontFaceCSSFixed(dir string, fonts []webFont) string {
	var b strings.Builder
	for _, f := range fonts {
		fmt.Fprintf(&b,
			"@font-face { font-family: '%s'; font-style: %s; src: url('%s/%s.woff2') }\n",
			sanitizeSlot(f.Family), f.Style, sanitizeSlot(dir), sanitizeSlot(f.FileName))
	}
	return b.String()
}

// renderPathFixed: the path segment is escaped for a URL.
func renderPathFixed(table string) string {
	return fmt.Sprintf("path := \"/%s\"\n", urlPathEscape(table))
}

func urlPathEscape(v string) string { return strings.ReplaceAll(v, "/", "%2F") }

// ---- deliberate silences ------------------------------------------------

// plainVerb: no keyword around the verb — a message, not code.
func plainVerb(name string) string {
	return fmt.Sprintf("hello %s, welcome", name)
}

// listVerb: `List%s(` only completes an identifier the call already
// spells; the verb does not stand alone, so the slot is not scanned.
func listVerb(structName string) string {
	return fmt.Sprintf("c.List%s(ctx, nil)\n", structName)
}

// receiverVerb: `func (%s)` is not one of the scanned declaration
// spellings (func/type/var keyword, :=, callee).
func receiverVerb(name string) string {
	return fmt.Sprintf("func (%s) Name() string { return \"plugin\" }", name)
}

// stringsResult: strconv/strings results are silent by design.
func stringsResult(name string) string {
	return fmt.Sprintf("var %s = 1\n", strings.ToUpper(name))
}

// argSlotVerb: a verb in ordinary argument position of the emitted call.
func argSlotVerb(name string) string {
	return fmt.Sprintf("fs.String(%q, \"\", %q)", name, "help")
}

// concatNoKeyword: concatenation into SQL is GOFASTR1401's shape, and
// this format has no identifier slot anyway.
func concatNoKeyword(table string) string {
	return "SELECT * FROM " + table
}

// proseType: "type" in English prose is not a declaration; the keyword
// must start a code context.
func proseType(kind string) string {
	return fmt.Sprintf("%s has component type %s from another package", "x", kind)
}

// callShape: the bare `%s(` shape is human-readable rendering here, not
// emitted code; the rule leaves it to the declaration spellings.
func callShape(kind, cond string) string {
	return fmt.Sprintf("%s:%d %s(%s)", "f", 1, kind, cond)
}

// apostropheProse: an apostrophe in prose must not open a CSS string run.
func apostropheProse(slot string) string {
	return fmt.Sprintf("// fontFaceCSS holds the app's fonts.\nvar fontFaceCSS = style.FontFaceCSS(\"\"%s)\n", slot)
}
