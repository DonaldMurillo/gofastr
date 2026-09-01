package main

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// declaration-derived string: can it break out of the literal or identifier
// it lands in?
//
// Two input paths with different trust boundaries:
//
//   - --from-openapi (the real boundary: the spec can be a URL). Every
//     spec-derived string lands via %q or behind an identifier-grammar
//     check (oaValidIdent/oaValidFlag). Proven here, not assumed.
//   - entity mode (reads the app's own entities/ via packReadEntities).
//     Names land in identifier position (ent.Struct via toCamelCase, which
//     passes quotes through) and tables land RAW inside "/%s/" Go string
//     literals. The blueprint path into entities/ is guarded upstream
//     (validateBlueprint: isGoIdentifier + query.SafeIdent + the
//     parse-backstop), but `generate cli` reads hand-written entity files
//     too, so the boundary has to hold HERE as well, mirroring the same
//     guards validateBlueprint applies.

// TestGenerateCLI_HostileTableCannotBreakOutOfPathLiterals: a table
// containing a Go string-literal delimiter must be refused, not emitted.
// Before the guard, `table: x"); pwn(); ("` rendered as
//
//	g.client.Do(g.ctx, http.MethodGet, "/x"); pwn(); ("/"+url.PathEscape(id), ...)
//
// — the payload at expression position in generated, syntactically valid Go.
func TestGenerateCLI_HostileTableCannotBreakOutOfPathLiterals(t *testing.T) {
	for _, table := range []string{
		`x"); pwn(); ("`,
		`x\` + "\n" + `y`,
		"x\"y",
	} {
		decls := []framework.EntityDeclaration{{
			Name:  "events",
			Table: table,
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string"},
			},
		}}
		_, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client")
		if err == nil {
			t.Errorf("table %q: buildCLISpec accepted a table that breaks the emitted Go string literal", table)
			continue
		}
		if !strings.Contains(err.Error(), "table") {
			t.Errorf("table %q: error does not name the table: %v", table, err)
		}
	}
}

// TestGenerateCLI_EntityNameMustDeriveIdentifier: ent.Struct lands in
// identifier position (run%sList, %sCommands). toCamelCase splits only on
// _/-/space, so a name with a quote or paren carries it straight into the
// identifier — refused here, exactly as validateBlueprint refuses it for
// `gofastr generate`.
func TestGenerateCLI_EntityNameMustDeriveIdentifier(t *testing.T) {
	for _, name := range []string{`po"sts`, "po sts()", "2fa_tokens", "日本"} {
		decls := []framework.EntityDeclaration{{
			Name: name,
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string"},
			},
		}}
		_, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client")
		if err == nil {
			t.Errorf("name %q: buildCLISpec accepted a name that does not derive a Go identifier", name)
		}
	}
}

// TestGenerateCLI_PlainNamesStillGenerate: the guards above must not
// over-fire. Dashed table names generate valid literals and command words
// today and keep doing so.
func TestGenerateCLI_PlainNamesStillGenerate(t *testing.T) {
	decls := []framework.EntityDeclaration{{
		Name:  "events",
		Table: "blog-posts",
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
		},
	}}
	spec, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("plain dashed table rejected: %v", err)
	}
	files := renderCLIFiles(spec)
	joined := ""
	for _, f := range files {
		joined += f.content
	}
	if !strings.Contains(joined, `"/blog-posts/"`) {
		t.Errorf("expected the dashed table in a path literal")
	}
}

// TestGenerateCLI_SelectionSinkIsCommentBodied demonstrates WHY the
// spec-build guard exists: renderCLIMain itself cannot defend a comment —
// a newline in spec.Selection lands at statement position and the file
// stops parsing. The buildCLISpec guard is what keeps such a spec from
// rendering; this pins the hazard so the guard is never "simplified" away.
func TestGenerateCLI_SelectionSinkIsCommentBodied(t *testing.T) {
	opts := defaultCLIOptions()
	opts.apiPrefix = "api\nx()"
	spec := cliSpec{Binary: "myapp", EnvPrefix: "MYAPP", APIPrefix: "/api", Selection: cliSelectionNote(opts)}
	main := renderCLIMain(spec)
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "main.go", main, 0); err == nil {
		t.Errorf("a newline in the baked selection did NOT break main.go — the sink moved; revisit the buildCLISpec guard's coverage")
	}
}

// TestGenerateCLI_SelectionControlCharsRefusedAtSpecBuild: the fix —
// buildCLISpec refuses a selection carrying control characters, naming the
// sink it protects.
func TestGenerateCLI_SelectionControlCharsRefusedAtSpecBuild(t *testing.T) {
	opts := defaultCLIOptions()
	opts.apiPrefix = "api\nx()"
	_, err := buildCLISpec(cliFixtureDecls(), opts, "example.com/app/entities/client")
	if err == nil {
		t.Fatalf("buildCLISpec accepted a --api-prefix carrying a newline into the generated source header")
	}
}

// TestGenerateCLI_OpenAPIHostileStringsStayQuoted proves the --from-openapi
// boundary: hostile PATH templates, summaries, and parameter names from a
// URL-fetched spec stay inside quoted literals, and the emitted operations.go
// still parses. Identifiers (operationIds) are grammar-checked and refused
// separately (TestOpenAPICLI_BadIdentifierFails).
func TestGenerateCLI_OpenAPIHostileStringsStayQuoted(t *testing.T) {
	hostilePath := `/a"); pwn(); ("/{id}`
	hostileSummary := "sum`mary \" \\ \n end"
	doc := fmt.Sprintf(`{"openapi":"3.0.3","paths":{%q:{"get":{"operationId":"getThing",`+
		`"summary":%q,"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}]}}}}`,
		hostilePath, hostileSummary)
	m := decodeJSONMap(t, doc)
	spec, err := buildOpenAPICLISpec(m, defaultCLIOptions(), "example.com/app/internal/client")
	if err != nil {
		t.Fatalf("hostile-but-valid spec refused: %v", err)
	}
	if len(spec.Ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(spec.Ops))
	}
	ops := renderCLIOpsFile(spec)
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "operations.go", ops, 0); err != nil {
		t.Fatalf("hostile spec strings produced unparseable operations.go: %v\n%s", err, ops)
	}
	// The payload must never appear outside a quoted literal: strip every
	// double-quoted Go string and search the residue.
	residue := stripGoDoubleQuoted(ops)
	for _, needle := range []string{"pwn()", "sum`mary"} {
		if strings.Contains(residue, needle) {
			t.Errorf("payload %q reached code position in operations.go:\n%s", needle, ops)
		}
	}
	// And the path literal must decode back to the exact hostile path.
	if !strings.Contains(ops, fmt.Sprintf("%q", hostilePath)) {
		t.Errorf("path template not emitted as the exact quoted literal")
	}
}

// --- helpers ---------------------------------------------------------------

func decodeJSONMap(t *testing.T, doc string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	return m
}

// stripGoDoubleQuoted removes double-quoted string regions (with \" escapes)
// from Go source, leaving identifiers, keywords, and structure.
func stripGoDoubleQuoted(src string) string {
	var b strings.Builder
	inStr := false
	esc := false
	for _, r := range src {
		switch {
		case inStr && esc:
			esc = false
		case inStr && r == '\\':
			esc = true
		case inStr && r == '"':
			inStr = false
		case !inStr && r == '"':
			inStr = true
		case !inStr:
			b.WriteRune(r)
		}
	}
	return b.String()
}
