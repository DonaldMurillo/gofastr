package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
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
	err := refuseDuplicateDecls([]GeneratedFile{{Name: "client/client.go", Content: renderClient(clientClashDecls())}})
	if err == nil || !strings.Contains(err.Error(), "Client.MarkPaidOrders") {
		t.Fatalf("err = %v, want the clashing method named", err)
	}
	if err := refuseDuplicateDecls([]GeneratedFile{{Name: "client/client.go", Content: renderClient(statesFixtureDecls())}}); err != nil {
		t.Fatalf("distinct names refused: %v", err)
	}
	// Same name in two package directories is two identifiers.
	if err := refuseDuplicateDecls([]GeneratedFile{
		{Name: "a/x.go", Content: "package a\nfunc F() {}\n"},
		{Name: "b/x.go", Content: "package b\nfunc F() {}\n"},
	}); err != nil {
		t.Fatalf("same name in two packages refused: %v", err)
	}
}

func TestProjectRefusesClientNameClash(t *testing.T) {
	_, err := RenderGeneratedProject(clientClashDecls())
	if err == nil || !strings.Contains(err.Error(), "MarkPaidOrders") {
		t.Fatalf("renderGeneratedProject = %v, want the clash refused", err)
	}
}

func TestSDKRefusesClientNameClash(t *testing.T) {
	opts := SdkOptions{name: "myapp", module: "example.com/sdk"}
	spec, err := buildSDKSpec(clientClashDecls(), &opts)
	if err == nil {
		_, err = renderSDKGoFiles(spec)
	}
	if err == nil || !strings.Contains(err.Error(), "MarkPaidOrders") {
		t.Fatalf("SDK = %v, want the clash refused", err)
	}
}

func TestCLIRefusesWrapperNameClash(t *testing.T) {
	spec, err := buildCLISpec(cliClashDecls(), CliOptions{Binary: "myapp"}, "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("buildCLISpec: %v", err)
	}
	opts := CliOptions{Binary: "myapp", OutDir: t.TempDir(), dryRun: true}
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { emitCLIFiles(opts, spec) })
	})
	if code != 1 || !strings.Contains(out, "runOrdersMarkPaid") {
		t.Fatalf("exit %d, output %q: want the clash refused", code, out)
	}
}
