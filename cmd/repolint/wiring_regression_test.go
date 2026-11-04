package main

import "testing"

func TestEmittedRPCMarkupIsUntypedWiring(t *testing.T) {
	for _, source := range []string{
		"package main\nvar markup = `<button data-cui-rpc=\"save\">Save</button>`\n",
		"package main\nvar markup = \"<button data-cui-rpc=\\\"save\\\">Save</button>\"\n",
		"package main\nvar markup = `<button data-cui-rpc = 'save'>Save</button>`\n",
		"package generator\nvar attrs = map[string]string{`data-cui-rpc`: \"save\"}\n",
	} {
		findings := lintBytes("cmd/gofastr/emit.go", []byte(source))
		mustFindRule(t, findings, "untyped-fui-wiring")
	}
	for _, name := range []string{"data-cui-rpc-method", "data-cui-rpc-body", "data-cui-rpc-close"} {
		for _, source := range []string{"var attr = `" + name + "=\"x\"`", "var attrs = map[string]string{`" + name + "`: \"x\"}"} {
			findings := lintBytes("cmd/gofastr/emit.go", []byte(source))
			for _, finding := range findings {
				if finding.Rule == "untyped-fui-wiring" {
					t.Fatalf("prefix collision: %+v", finding)
				}
			}
		}
	}
}
