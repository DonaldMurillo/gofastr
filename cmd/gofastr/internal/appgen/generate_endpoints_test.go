package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

// A move binds the same JS method and CLI command an endpoint of its
// name would.
func TestEndpointNameRefusesMoveKey(t *testing.T) {
	decl := statesFixtureDecls()[0]
	for _, name := range []string{"pay", "issue"} {
		if err := checkEndpointName(decl, name); err == nil || !strings.Contains(err.Error(), "state move") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := checkEndpointName(decl, "refund"); err != nil {
		t.Errorf("refund: %v", err)
	}
}

// endpointDecls is statesFixtureDecls with a second entity and one SDK
// endpoint on the invoices entity.
func endpointDecls(ep framework.Endpoint) []framework.EntityDeclaration {
	decls := statesFixtureDecls()
	decls = append(decls, framework.EntityDeclaration{
		Name: "customers", Table: "customers",
		Fields: []framework.FieldDeclaration{{Name: "name", Type: "string"}},
	})
	ep.SDK = true
	decls[0].Endpoints = []framework.Endpoint{ep}
	return decls
}

func TestEndpointGenerationRefusals(t *testing.T) {
	ret := framework.Endpoint{Name: "best_customer", Method: "GET", Path: "best", Returns: "customers"}
	if _, err := buildSDKSpec(endpointDecls(ret), &SdkOptions{only: []string{"invoices"}}); err == nil || !strings.Contains(err.Error(), "selection leaves out") {
		t.Errorf("sdk: an endpoint returning an unselected entity: %v", err)
	}
	if _, err := buildCLISpec(endpointDecls(ret), CliOptions{Binary: "shop", exclude: []string{"customers"}}, "x/client"); err == nil || !strings.Contains(err.Error(), "selection leaves out") {
		t.Errorf("cli: an endpoint returning an unselected entity: %v", err)
	}
	if _, err := buildSDKSpec(endpointDecls(ret), &SdkOptions{}); err != nil {
		t.Errorf("sdk: a full selection: %v", err)
	}

	flagged := framework.Endpoint{Name: "fetch", Method: "GET", Path: "fetch", InputSchema: []schema.Field{{Name: "url", Type: schema.String}}}
	if _, err := buildCLISpec(endpointDecls(flagged), CliOptions{Binary: "shop"}, "x/client"); err == nil || !strings.Contains(err.Error(), "derives CLI flag --url") {
		t.Errorf("cli: an input named url: %v", err)
	}
	flagged.InputSchema[0].Name = "o" // the shared body builder skips --o
	if _, err := buildCLISpec(endpointDecls(flagged), CliOptions{Binary: "shop"}, "x/client"); err == nil || !strings.Contains(err.Error(), "derives CLI flag --o") {
		t.Errorf("cli: an input named o: %v", err)
	}
	flagged.InputSchema[0].Name = "limit" // a list flag, free on an endpoint command
	if _, err := buildCLISpec(endpointDecls(flagged), CliOptions{Binary: "shop"}, "x/client"); err != nil {
		t.Errorf("cli: an input named limit: %v", err)
	}

	clash := framework.Endpoint{Name: "audit", Method: "POST", Path: "audit", InputSchema: []schema.Field{{Name: "note", Type: schema.String}}}
	decls := endpointDecls(clash)
	decls[1].Name, decls[1].Table = "audit_request", "audit_request"
	if _, err := buildSDKSpec(decls, &SdkOptions{}); err == nil || !strings.Contains(err.Error(), "AuditRequest is already declared by entity audit_request") {
		t.Errorf("sdk: a request type named like an entity: %v", err)
	}
}

// A path value named like a reserved word gets the Arg suffix in both
// clients, where the bare name would not compile or would shadow.
func TestEndpointArgsAvoidReservedNames(t *testing.T) {
	ep := framework.Endpoint{Name: "retype", Method: "POST", Path: "{type}/{input}/{class}/{ctx}/{encode_u_r_i_component}/{undefined}", InputSchema: []schema.Field{
		{Name: "type", Type: schema.String, Required: true}, {Name: "input", Type: schema.String, Required: true},
		{Name: "class", Type: schema.String, Required: true}, {Name: "ctx", Type: schema.String, Required: true},
		{Name: "encode_u_r_i_component", Type: schema.String, Required: true}, {Name: "undefined", Type: schema.String, Required: true},
		{Name: "note", Type: schema.String},
	}}
	goSrc := renderClientEndpoint(ep, "posts")
	if !strings.Contains(goSrc, "Retype(ctx context.Context, typeArg string, input string, class string, ctxArg string, encodeURIComponent string, undefined string, in RetypeRequest)") {
		t.Errorf("go args:\n%s", goSrc)
	}
	js := jsEndpointBinding("posts", ep)
	if !strings.Contains(js, "(type, inputArg, classArg, ctx, encodeURIComponentArg, undefinedArg, input) =>") {
		t.Errorf("js args: %s", js)
	}
}

// An entity whose only commands are endpoints still registers them.
func TestCLIEndpointOnlyEntityRegisters(t *testing.T) {
	decls := endpointDecls(framework.Endpoint{Name: "merge", Method: "POST", Path: "{id}/merge", InputSchema: []schema.Field{{Name: "id", Type: schema.String, Required: true}}})
	decls[1].Endpoints, decls[0].Endpoints = decls[0].Endpoints, nil
	spec, err := buildCLISpec(decls, CliOptions{Binary: "shop", verbs: "customers="}, "x/client")
	if err != nil {
		t.Fatal(err)
	}
	if main := renderCLIMain(spec); !strings.Contains(main, "cmds = append(cmds, customersCommands()...)") {
		t.Errorf("customers commands not registered:\n%s", main)
	}
}
