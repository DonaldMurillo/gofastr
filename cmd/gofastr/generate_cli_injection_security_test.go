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

// TestScaffoldNameGuardedByValidate: the `gofastr generate entity|screen
// <name>` argv is the same trust boundary the entity mode above defends —
// a name typed (or scripted) at the CLI becomes the blueprint fragment's
// entity/screen name, which lands in identifier position and in the emitted
// file name (entities/<name>.go, screens via screenFileName). The scaffold
// path must inherit validateBlueprint's identifier guard rather than
// synthesizing a fragment that bypasses it: generateBlueprint runs the same
// validateBlueprint, so a literal-breaking or path-shaped name must be
// refused there. Pinned green: the guard exists; this keeps a future
// "scaffold skips validation for convenience" refactor from opening it.
func TestScaffoldNameGuardedByValidate(t *testing.T) {
	for _, name := range []string{
		`x"); PWN() //`, // breaks out of the emitted Go literal
		`../evil`,       // path traversal via the derived file name
		`a/b`,           // ditto, slash survives toCamelCase
		`2fa_tokens`,    // leading digit: no valid Go identifier
		"x`y",           // backtick: raw struct-tag literal
	} {
		bp := Blueprint{Entities: []framework.EntityDeclaration{{
			Name:   name,
			Fields: []framework.FieldDeclaration{{Name: "name", Type: "string", Required: true}},
		}}}
		err := validateBlueprint(bp)
		if err == nil {
			t.Errorf("scaffold entity name %q: accepted; it must be refused before entities/%s.go is emitted", name, name)
			continue
		}
		if !strings.Contains(err.Error(), "identifier") && !strings.Contains(err.Error(), name) {
			t.Errorf("scaffold entity name %q: error neither names the value nor says identifier: %v", name, err)
		}
	}
	// Control: the names the scaffold is FOR still pass.
	bp := Blueprint{Entities: []framework.EntityDeclaration{{
		Name:   "two_fa_tokens",
		Fields: []framework.FieldDeclaration{{Name: "name", Type: "string", Required: true}},
	}}}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("plain scaffold name must validate: %v", err)
	}
}

// TestGenerateCLI_FieldNameMustDeriveIdentifier: buildCLIEntity guards
// ent.Struct (isGoIdentifier) and ent.Table (literal-safe), but a FIELD name
// lands in generated identifier positions with no guard of its own:
//
//	flt%s := fs.String(...)  (list filters)
//	fld%s := fs.String(...)  (create/update/patch flags)
//	set(%q, *flt%s)          (list param wiring)
//	body[%q] = *fld%s        (mutation payload wiring)
//
// toCamelCase only splits on _/-/space, so every other byte survives into
// `fld`+name. A NoQuery writable field reaches only the statement-context
// sinks (flag decl + body assignment), where `x;pwn();y` renders
//
//	fldX;pwn();y := fs.String("x;pwn();y", "", "…")
//	body["x;pwn();y"] = *fldX;pwn();y
//
// — syntactically valid Go with pwn() at statement position, which passes
// the format.Source gate emitCLIFiles relies on and ships in the operator's
// CLI. Same trust boundary as the entity/table guards above (hand-written
// entities/*.go via packReadEntities); the field is the one input the guard
// forgot.
func TestCLIFieldNameMustDeriveIdentifier(t *testing.T) {
	for _, name := range []string{
		"x;PWN();y",  // statement-position call, parses (the NoQuery shape below)
		"x:=pwn();y", // walrus at flag decl
		`ti"tle`,     // closes the emitted identifier into expression position
		"x`y",        // backtick through identifier + struct-tag-ish help text
		"x\npwn();y", // newline: line-splitting escape
	} {
		decls := []framework.EntityDeclaration{{
			Name:  "events",
			Table: "events",
			Fields: []framework.FieldDeclaration{
				{Name: name, Type: "string", NoQuery: true},
			},
		}}
		_, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client")
		if err == nil {
			t.Errorf("field name %q: buildCLISpec accepted a name that does not derive a Go identifier (it is emitted as generated variable names)", name)
			continue
		}
		if !strings.Contains(err.Error(), name) {
			t.Errorf("field name %q: error does not name the offending field: %v", name, err)
		}
	}
	// Control: real field names, including unicode and digits, still derive
	// identifiers and must pass.
	for _, name := range []string{"title", "created_at", "x2fa", "café"} {
		decls := []framework.EntityDeclaration{{
			Name:  "events",
			Table: "events",
			Fields: []framework.FieldDeclaration{
				{Name: name, Type: "string"},
			},
		}}
		if _, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client"); err != nil {
			t.Errorf("clean field name %q must generate: %v", name, err)
		}
	}
}

// TestGenerateCLI_FieldPayloadBecomesStatements: the hazard the guard above
// must close, pinned the way TestGenerateCLI_SelectionSinkIsCommentBodied
// pins its sink. Today the emitted file PARSES with pwn() at statement
// position — the format.Source gate cannot see it — so refusal at spec
// build is the only boundary. This subtest proves the payload is live code,
// not data, until the guard exists.
func TestCLIFieldPayloadBecomesStatements(t *testing.T) {
	decls := []framework.EntityDeclaration{{
		Name:  "events",
		Table: "events",
		Fields: []framework.FieldDeclaration{
			{Name: "x;PWN();y", Type: "string", NoQuery: true},
		},
	}}
	spec, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client")
	if err != nil {
		t.Skipf("spec build now refuses the payload (guard landed): %v", err)
	}
	files := renderCLIFiles(spec)
	for _, f := range files {
		if !strings.Contains(f.content, "PWN") {
			continue
		}
		assertIRStayedData(t, "cli-field-identifier", f.name, f.content)
	}
}

// TestCLIFileCollisionsFailGeneration: two entities whose tables derive the
// same command form (user_posts / user-posts) would emit the same
// <command>.go twice, and a table carrying parent traversal (../evil) would
// emit a file name that escapes the output dir. Both must fail generation
// with an error naming the file, not silently overwrite or write outside
// --out. Pinned green: FileSet.Add + SafeRelativePath carry this; the pin
// keeps a future "skip the fileset, write directly" refactor from opening it.
func TestCLIFileCollisionsFailGeneration(t *testing.T) {
	t.Run("same command form collides", func(t *testing.T) {
		decls := []framework.EntityDeclaration{
			{Name: "userPosts", Table: "user_posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
			{Name: "userposts", Table: "user-posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
		}
		spec, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client")
		if err != nil {
			t.Fatalf("distinct tables must build: %v", err)
		}
		_, err = fileSetFromGeneratedFiles(renderCLIFiles(spec), "cli")
		if err == nil {
			t.Fatal("SECURITY: [silent-overwrite] colliding command forms wrote one entity over the other with no error")
		}
		if !strings.Contains(err.Error(), "user-posts.go") {
			t.Errorf("collision error must name the colliding file: %v", err)
		}
	})
	t.Run("traversal table refused", func(t *testing.T) {
		decls := []framework.EntityDeclaration{{
			Name:   "events",
			Table:  "../evil",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}}
		spec, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client")
		if err != nil {
			t.Skipf("spec build now refuses traversal tables (guard landed): %v", err)
		}
		_, err = fileSetFromGeneratedFiles(renderCLIFiles(spec), "cli")
		if err == nil {
			t.Fatal("SECURITY: [path-escape] table with parent traversal produced a file name outside the output dir")
		}
		if !strings.Contains(err.Error(), "parent traversal") {
			t.Errorf("traversal error must say so: %v", err)
		}
	})
}
