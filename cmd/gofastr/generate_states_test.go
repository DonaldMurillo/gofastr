package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
)

// statesFixtureDecls is the invoices fixture the generator tests render:
// status is the guarded enum, paid_on the stamp the pay move writes,
// mark_overdue a System move that must appear in no generated client.
func statesFixtureDecls() []framework.EntityDeclaration {
	return []framework.EntityDeclaration{
		{
			Name:  "invoices",
			Table: "invoices",
			Fields: []framework.FieldDeclaration{
				{Name: "number", Type: "string", Required: true},
				{Name: "amount", Type: "decimal"},
				{Name: "status", Type: "enum", Values: []string{"draft", "open", "paid", "void"}, Default: "draft"},
				{Name: "paid_on", Type: "date"},
			},
			States: &framework.StatesConfig{
				Field:   "status",
				Initial: []string{"draft", "open"},
				Transitions: []framework.Transition{
					{Key: "issue", From: []string{"draft"}, To: "open"},
					{Key: "pay", From: []string{"open"}, To: "paid", Stamp: "paid_on"},
					{Key: "void", From: []string{"draft", "open"}, To: "void"},
					{Key: "mark_overdue", From: []string{"open"}, To: "open", System: true},
				},
			},
		},
	}
}

// The entity CLI emits one subcommand per non-system move, bound to a
// shared body that POSTs the move's route with the empty JSON body the
// content-type gate requires. System moves appear nowhere.
func TestCLIRendersTransitionCommands(t *testing.T) {
	spec, err := buildCLISpec(statesFixtureDecls(), cliOptions{binary: "myapp"}, "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("buildCLISpec: %v", err)
	}
	files := renderCLIFiles(spec)
	joined := map[string]string{}
	for _, f := range files {
		joined[f.name] = f.content
	}

	invoices := joined["invoices.go"]
	if invoices == "" {
		t.Fatalf("no invoices.go emitted; files: %v", fileNames(files))
	}
	for _, want := range []string{
		`{name: "invoices pay", summary: "move status from open to paid, stamps paid_on", run: runInvoicesPay}`,
		`{name: "invoices issue", summary: "move status from draft to open", run: runInvoicesIssue}`,
		`{name: "invoices void", summary: "move status from draft or open to void", run: runInvoicesVoid}`,
		"func runInvoicesPay(args []string) int {\n\treturn runTransitionVerb(\"invoices pay\", \"/invoices\", \"pay\", args)\n}",
	} {
		if !strings.Contains(invoices, want) {
			t.Errorf("invoices.go is missing %q:\n%s", want, invoices)
		}
	}
	if strings.Contains(invoices, "mark_overdue") || strings.Contains(invoices, "markOverdue") {
		t.Error("invoices.go mentions the System move; system moves appear nowhere")
	}

	verbs := joined["verbs.go"]
	if verbs == "" {
		t.Fatal("verbs.go not emitted for a transitions-only surface")
	}
	for _, want := range []string{
		"func runTransitionVerb(cmd, base, key string, args []string) int {",
		`path := base + "/" + url.PathEscape(id) + "/transitions/" + url.PathEscape(key)`,
		"g.client.Do(g.ctx, http.MethodPost, path, map[string]any{}, &out)",
	} {
		if !strings.Contains(verbs, want) {
			t.Errorf("verbs.go is missing %q (the empty JSON body carries the content type the route requires)", want)
		}
	}

	if _, err := fileSetFromGeneratedFiles(files, "cli"); err != nil {
		t.Fatalf("generated CLI does not parse: %v", err)
	}
}

// The typed Go client gets one method per non-system move, posting the
// move's route with the shared empty body (doJSON only sets the content
// type on a non-nil body).
func TestTypedClientRendersTransitionMethods(t *testing.T) {
	out := renderClient(statesFixtureDecls())
	for _, want := range []string{
		"func (c *Client) PayInvoices(ctx context.Context, id string) (Invoices, error) {",
		`path := "/invoices/"+url.PathEscape(id)+"/transitions/"+url.PathEscape("pay")`,
		"c.doSingleJSON(ctx, http.MethodPost, path, moveBody, &out)",
		"func (c *Client) VoidInvoices(ctx context.Context, id string) (Invoices, error) {",
		"func (c *Client) IssueInvoices(ctx context.Context, id string) (Invoices, error) {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("typed client is missing %q", want)
		}
	}
	if strings.Contains(out, "MarkOverdue") {
		t.Error("typed client has a method for the System move; system moves appear nowhere")
	}
}

// The JS SDK binds each move as a quoted property over the shared
// transition method, and the d.ts types it. Quoted slots only: the key
// never becomes an identifier in the executable artifact.
func TestJSSDKRendersTransitionBindings(t *testing.T) {
	opts := sdkOptions{name: "myapp", sdkVersion: "1.2.3"}
	spec, err := buildSDKSpec(statesFixtureDecls(), &opts)
	if err != nil {
		t.Fatalf("buildSDKSpec: %v", err)
	}
	files := renderSDKJSFiles(spec)
	js, dts := "", ""
	for _, f := range files {
		switch f.name {
		case "client.js":
			js = f.content
		case "client.d.ts":
			dts = f.content
		}
	}
	if js == "" || dts == "" {
		t.Fatalf("SDK files missing: %v", fileNames(files))
	}

	for _, want := range []string{
		`this["invoices"]["pay"] = (id) => this["invoices"].transition(id, "pay");`,
		`this["invoices"]["void"] = (id) => this["invoices"].transition(id, "void");`,
		"async transition(id, key) {",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("client.js is missing %q", want)
		}
	}
	if strings.Contains(js, `"mark_overdue"`) {
		t.Error("client.js binds the System move; system moves appear nowhere")
	}
	for _, want := range []string{
		"transition(id: string, key: string): Promise<T>;",
		"export interface InvoicesMoves {",
		"pay(id: string): Promise<Invoices>;",
		"readonly invoices: Resource<Invoices, InvoicesInput, InvoicesPatch> & InvoicesMoves;",
	} {
		if !strings.Contains(dts, want) {
			t.Errorf("client.d.ts is missing %q", want)
		}
	}
}

// The from-openapi CLI reads the spec `gofastr`'s own OpenAPI generator
// emits, so item 1's per-move operations become commands with no extra
// work: the document built from the live entity yields a pay command
// over the transition path.
func TestOpenAPICLIRendersMovesFromSpec(t *testing.T) {
	ent := entity.Define("invoices", entity.EntityConfig{
		Name:  "invoices",
		Table: "invoices",
		Fields: []schema.Field{
			{Name: "number", Type: schema.String, Required: true},
			{Name: "amount", Type: schema.Decimal},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "open", "paid", "void"}, Default: "draft"},
			{Name: "paid_on", Type: schema.Date},
		},
		States: &entity.StatesConfig{
			Field:   "status",
			Initial: []string{"draft", "open"},
			Transitions: []entity.Transition{
				{Key: "issue", From: []string{"draft"}, To: "open"},
				{Key: "pay", From: []string{"open"}, To: "paid", Stamp: "paid_on"},
				{Key: "mark_overdue", From: []string{"open"}, To: "open", System: true},
			},
		},
	}.WithTimestamps(false))
	raw, err := json.Marshal(openapi.EntityOpenAPI(staticRegistry{ent}, "Test", "1.0.0", nil).Build())
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	spec, err := buildOpenAPICLISpec(doc, cliGateOptions(), "example.com/app/cli/internal/client")
	if err != nil {
		t.Fatalf("buildOpenAPICLISpec over the emitted spec: %v", err)
	}
	var pay *cliOp
	for i, op := range spec.Ops {
		if op.ID == "pay_invoices" {
			pay = &spec.Ops[i]
		}
	}
	if pay == nil {
		t.Fatal("from-openapi CLI has no pay operation; the spec's per-move operations did not carry through")
	}
	if pay.PathTemplate != "/invoices/{id}/transitions/pay" || pay.Method != "POST" {
		t.Errorf("pay op = %s %s, want POST /invoices/{id}/transitions/pay", pay.Method, pay.PathTemplate)
	}
	if len(pay.PathParams) != 1 || pay.PathParams[0].Name != "id" {
		t.Errorf("pay path params = %+v, want the id", pay.PathParams)
	}
	for _, op := range spec.Ops {
		if op.ID == "mark_overdue_invoices" {
			t.Error("from-openapi CLI derived a command for the System move; the spec documents none")
		}
	}
}

type staticRegistry struct{ ent *entity.Entity }

func (r staticRegistry) All() map[string]*entity.Entity {
	return map[string]*entity.Entity{r.ent.GetName(): r.ent}
}

func (r staticRegistry) AllSorted() []*entity.Entity { return []*entity.Entity{r.ent} }

func (r staticRegistry) Get(name string) (*entity.Entity, error) {
	if name == r.ent.GetName() {
		return r.ent, nil
	}
	return nil, fmt.Errorf("entity not found: %s", name)
}

// The generators refuse what the server's boot check would have refused:
// a key outside the move-key grammar, a duplicate, or one colliding with
// any generated surface's own name (hand-written declarations never passed
// entity.Define; validateDeclarationStates re-runs the one boot check).
func TestGeneratorsRefuseBadTransitionKeys(t *testing.T) {
	cases := map[string][]framework.Transition{
		"uppercase key":         {{Key: "Pay", From: []string{"draft"}, To: "paid"}},
		"double underscore key": {{Key: "mark__paid", From: []string{"draft"}, To: "paid"}},
		"verb key":              {{Key: "patch", From: []string{"draft"}, To: "paid"}},
		"batch verb key":        {{Key: "batch_update", From: []string{"draft"}, To: "paid"}},
		"duplicate key":         {{Key: "pay", From: []string{"draft"}, To: "paid"}, {Key: "pay", From: []string{"draft"}, To: "void"}},
		"js member key":         {{Key: "transition", From: []string{"draft"}, To: "paid"}},
		"openapi id key":        {{Key: "events", From: []string{"draft"}, To: "paid"}},
	}
	for name, moves := range cases {
		decls := statesFixtureDecls()
		decls[0].States.Transitions = moves
		if _, err := buildCLISpec(decls, cliOptions{binary: "myapp"}, "example.com/app/entities/client"); err == nil {
			t.Errorf("CLI accepted %s", name)
		}
		opts := sdkOptions{name: "myapp"}
		if _, err := buildSDKSpec(decls, &opts); err == nil {
			t.Errorf("SDK accepted %s", name)
		}
	}
}

// The scaffolded project's typed client is emitted straight from the same
// declarations, so the states boot check runs on that path too: a move key
// registration refuses never reaches renderClient, whatever declared it
// (the blueprint path validates shape, not states).
func TestRenderGeneratedProjectRefusesBadStates(t *testing.T) {
	decls := statesFixtureDecls()
	decls[0].States.Transitions[0].Key = "patch"
	if _, err := renderGeneratedProject(decls); err == nil || !strings.Contains(err.Error(), `key "patch" is reserved`) {
		t.Fatalf("renderGeneratedProject accepted a reserved move key: %v", err)
	}
}
