package main

import "testing"

func TestEmittedRPCMarkupIsUntypedWiring(t *testing.T) {
	for _, source := range []string{
		"package main\nvar markup = `<button data-cui-rpc=\"save\">Save</button>`\n",
		"package main\nvar markup = \"<button data-cui-rpc=\\\"save\\\">Save</button>\"\n",
		"package main\nvar markup = `<button data-cui-rpc = 'save'>Save</button>`\n",
	} {
		findings := lintBytes("cmd/gofastr/emit.go", []byte(source))
		mustFindRule(t, findings, "untyped-fui-wiring")
	}
	for _, name := range []string{"data-cui-rpc-method", "data-cui-rpc-body", "data-cui-rpc-close"} {
		findings := lintBytes("cmd/gofastr/emit.go", []byte("var attr = `"+name+"=\"x\"`"))
		for _, finding := range findings {
			if finding.Rule == "untyped-fui-wiring" {
				t.Fatalf("prefix collision: %+v", finding)
			}
		}
	}
}
