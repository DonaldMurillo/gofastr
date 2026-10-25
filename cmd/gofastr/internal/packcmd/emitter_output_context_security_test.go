package packcmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Property: `gofastr pack` must emit a scalar that re-parses as the same single
// scalar; encodeAppSpecYAML is documented as the exact inverse of
// decodeAppSpec (see the banner comment in pack.go).
//
// Surfaces: every list-of-strings the writer emits as a YAML FLOW list via
// writeFlowList: entity field `values:` (enums), `search_fields:`, entity_list
// `fields:` / `filters:`, `pagination.cursor_fields`, index `columns:`, and any
// scalar list a future construct adds. Of those, `search_fields` / `fields` /
// `filters` / `columns` are re-checked against the declaration on decode, so a
// smuggled sibling there fails the re-parse loudly rather than widening the
// surface. `values:` (the enum value set) is the one with no such check, hence
// the assertion below.
//
// needsQuote decides whether to quote a scalar. It checks `,` only as the FIRST
// byte (`strings.ContainsAny(s[:1], "…,…")`), so an interior comma is emitted
// bare, and bare is exactly where a comma is a flow-list ITEM SEPARATOR. One
// item becomes two on re-parse.
//
// This is the reverse trust boundary: `pack` reads a generated app's Go source
// and writes the YAML you regenerate from. A list element gains a sibling that
// the app never declared, and the regenerated app spec then validates it
// happily; `search_fields` and `filters:` are the sharp ones, since both widen
// the query surface of the generated API.
func TestPackYAMLListItemStaysOneItem(t *testing.T) {
	// One shape per YAML flow-list metacharacter. The comma is the live one.
	for _, payload := range []string{
		"open,closed",   // flow-list item separator
		"open]  x",      // flow terminator
		"open}  x",      // mapping terminator
		"open: closed",  // key/value separator
		"open #comment", // comment introducer
	} {
		bp := appspec.AppSpec{
			App: appspec.AppSpecApp{Name: "app", Module: "local/app"},
			Entities: []framework.EntityDeclaration{{
				Name: "tickets",
				Fields: []framework.FieldDeclaration{
					{Name: "title", Type: "string"},
					{Name: "status", Type: "enum", Values: []string{payload, "closed"}},
				},
			}},
		}
		yml, err := encodeAppSpecYAML(bp)
		if err != nil {
			t.Errorf("SECURITY: [yaml-injection] %q: pack refused a value-only hostile app spec: %v", payload, err)
			continue
		}
		back, err := decodeAppSpecString(yml)
		if err != nil {
			t.Errorf("SECURITY: [yaml-injection] %q: pack emitted YAML that no longer parses: %v\n%s", payload, err, yml)
			continue
		}
		if got := back.Entities[0].Fields[1].Values; len(got) != 2 || got[0] != payload {
			t.Errorf("SECURITY: [yaml-injection] field.values %q round-tripped as %#v — pack invented list items\n%s", payload, got, yml)
		}
	}
	// The predicate itself: flow indicators are special ANYWHERE inside a
	// scalar, not only as its first byte, because writeFlowList emits scalars
	// into `[a, b]`. Testing only s[:1] was the bug.
	for _, s := range []string{"open,closed", "a[b", "a]b", "a{b", "a}b"} {
		if !needsQuote(s) {
			t.Errorf("needsQuote(%q) = false — an interior flow indicator is emitted bare", s)
		}
	}
	// Control: an ordinary scalar still emits bare, so pack output stays close
	// to hand-written YAML.
	if needsQuote("open") || needsQuote("in_progress") {
		t.Error("needsQuote now quotes plain scalars — pack output would churn")
	}
}
