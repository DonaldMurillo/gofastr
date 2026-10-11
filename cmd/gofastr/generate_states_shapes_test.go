package main

import (
	"strings"
	"testing"
)

// shapeBody returns the text from open up to the type's closing brace, or
// the one line of a type alias.
func shapeBody(t *testing.T, src, open string) string {
	t.Helper()
	i := strings.Index(src, open)
	if i < 0 {
		t.Fatalf("missing %q", open)
	}
	rest := src[i:]
	if strings.HasSuffix(open, "{") {
		if j := strings.Index(rest, "\n}\n"); j >= 0 {
			return rest[:j]
		}
	}
	return rest[:strings.Index(rest, "\n")]
}

// renderStatesShapes renders the invoices fixture through the Go client
// and the JS SDK's d.ts, with the States block advisory or enforced.
func renderStatesShapes(t *testing.T, advisory bool) (goSrc, dts string) {
	t.Helper()
	decls := statesFixtureDecls()
	decls[0].States.Advisory = advisory
	goSrc = renderClient(decls)
	opts := sdkOptions{name: "myapp", sdkVersion: "1.2.3"}
	spec, err := buildSDKSpec(decls, &opts)
	if err != nil {
		t.Fatalf("buildSDKSpec: %v", err)
	}
	for _, f := range renderSDKJSFiles(spec) {
		if f.name == "client.d.ts" {
			dts = f.content
		}
	}
	return goSrc, dts
}

// Typed write shapes match the OpenAPI request schemas: the stamp leaves
// every write shape, the state field leaves the patch shapes, and Input
// keeps the state field for create and PUT write-back.
func TestTypedWriteShapesDropGuarded(t *testing.T) {
	goSrc, dts := renderStatesShapes(t, false)
	input := shapeBody(t, goSrc, "type InvoicesInput struct {")
	if !strings.Contains(input, "Status ") || strings.Contains(input, "PaidOn") {
		t.Errorf("InvoicesInput should keep Status and drop PaidOn:\n%s", input)
	}
	for _, open := range []string{"type InvoicesPatch struct {", "type InvoicesBatchPatch struct {"} {
		body := shapeBody(t, goSrc, open)
		if strings.Contains(body, "Status ") || strings.Contains(body, "PaidOn") {
			t.Errorf("%s should drop Status and PaidOn:\n%s", open, body)
		}
		if !strings.Contains(body, "Number ") {
			t.Errorf("%s lost an ordinary field:\n%s", open, body)
		}
	}
	tsInput := shapeBody(t, dts, "export interface InvoicesInput {")
	if !strings.Contains(tsInput, "status?:") || strings.Contains(tsInput, "paidOn") {
		t.Errorf("TS InvoicesInput should keep status and drop paidOn:\n%s", tsInput)
	}
	if want := `export type InvoicesPatch = Partial<Omit<InvoicesInput, "status">>;`; !strings.Contains(dts, want) {
		t.Errorf("client.d.ts is missing %q", want)
	}
}

// The CLI's create/update/patch share one flag table: the stamp gets no
// flag under enforced States, the state field keeps one for create, and
// Advisory States keep both.
func TestCLIFlagsDropStamp(t *testing.T) {
	for _, advisory := range []bool{false, true} {
		decls := statesFixtureDecls()
		decls[0].States.Advisory = advisory
		spec, err := buildCLISpec(decls, cliOptions{binary: "myapp"}, "example.com/app/entities/client")
		if err != nil {
			t.Fatalf("buildCLISpec: %v", err)
		}
		var src string
		for _, f := range renderCLIFiles(spec) {
			if f.name == "invoices.go" {
				src = f.content
			}
		}
		table := shapeBody(t, src, "var invoicesMutationFields = []mutationField{")
		if !strings.Contains(table, `{flag: "status"`) {
			t.Errorf("advisory=%v: mutation flags lost status:\n%s", advisory, table)
		}
		if got := strings.Contains(table, `{flag: "paid-on"`); got != advisory {
			t.Errorf("advisory=%v: paid-on flag present=%v:\n%s", advisory, got, table)
		}
	}
}

// Advisory States guard nothing, so the write shapes keep every field.
func TestAdvisoryWriteShapesKeepAll(t *testing.T) {
	goSrc, dts := renderStatesShapes(t, true)
	for _, open := range []string{"type InvoicesInput struct {", "type InvoicesPatch struct {", "type InvoicesBatchPatch struct {"} {
		body := shapeBody(t, goSrc, open)
		if !strings.Contains(body, "Status ") || !strings.Contains(body, "PaidOn") {
			t.Errorf("%s should keep Status and PaidOn:\n%s", open, body)
		}
	}
	if want := "export type InvoicesPatch = Partial<InvoicesInput>;"; !strings.Contains(dts, want) {
		t.Errorf("client.d.ts is missing %q", want)
	}
}
