package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// movingDecl is an entity with a status field and one routable move.
func movingDecl(name, key string) framework.EntityDeclaration {
	return framework.EntityDeclaration{
		Name:  name,
		Table: name,
		Fields: []framework.FieldDeclaration{
			{Name: "status", Type: "enum", Values: []string{"open", "done"}, Default: "open"},
		},
		States: &framework.StatesConfig{
			Field:       "status",
			Initial:     []string{"open"},
			Transitions: []framework.Transition{{Key: key, From: []string{"open"}, To: "done"}},
		},
	}
}

// orders' mark_paid and paid_orders' mark both mint Client.MarkPaidOrders.
func clientClashDecls() []framework.EntityDeclaration {
	return []framework.EntityDeclaration{movingDecl("orders", "mark_paid"), movingDecl("paid_orders", "mark")}
}

// orders' mark_paid and orders_mark's paid both mint runOrdersMarkPaid.
func cliClashDecls() []framework.EntityDeclaration {
	return []framework.EntityDeclaration{movingDecl("orders", "mark_paid"), movingDecl("orders_mark", "paid")}
}

func TestDuplicateDeclsNamesIdentifier(t *testing.T) {
	err := refuseDuplicateDecls([]generatedFile{{name: "client/client.go", content: renderClient(clientClashDecls())}})
	if err == nil || !strings.Contains(err.Error(), "Client.MarkPaidOrders") {
		t.Fatalf("err = %v, want the clashing method named", err)
	}
	if err := refuseDuplicateDecls([]generatedFile{{name: "client/client.go", content: renderClient(statesFixtureDecls())}}); err != nil {
		t.Fatalf("distinct names refused: %v", err)
	}
	// Same name in two package directories is two identifiers.
	if err := refuseDuplicateDecls([]generatedFile{
		{name: "a/x.go", content: "package a\nfunc F() {}\n"},
		{name: "b/x.go", content: "package b\nfunc F() {}\n"},
	}); err != nil {
		t.Fatalf("same name in two packages refused: %v", err)
	}
}

func TestProjectRefusesClientNameClash(t *testing.T) {
	_, err := renderGeneratedProject(clientClashDecls())
	if err == nil || !strings.Contains(err.Error(), "MarkPaidOrders") {
		t.Fatalf("renderGeneratedProject = %v, want the clash refused", err)
	}
}

func TestSDKRefusesClientNameClash(t *testing.T) {
	opts := sdkOptions{name: "myapp", module: "example.com/sdk"}
	spec, err := buildSDKSpec(clientClashDecls(), &opts)
	if err == nil {
		_, err = renderSDKGoFiles(spec)
	}
	if err == nil || !strings.Contains(err.Error(), "MarkPaidOrders") {
		t.Fatalf("SDK = %v, want the clash refused", err)
	}
}

func TestCLIRefusesWrapperNameClash(t *testing.T) {
	spec, err := buildCLISpec(cliClashDecls(), cliOptions{binary: "myapp"}, "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("buildCLISpec: %v", err)
	}
	opts := cliOptions{binary: "myapp", outDir: t.TempDir(), dryRun: true}
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { emitCLIFiles(opts, spec) })
	})
	if code != 1 || !strings.Contains(out, "runOrdersMarkPaid") {
		t.Fatalf("exit %d, output %q: want the clash refused", code, out)
	}
}
