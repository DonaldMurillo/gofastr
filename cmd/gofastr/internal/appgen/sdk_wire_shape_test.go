package appgen

import (
	"database/sql"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func wireShapeDecls() []framework.EntityDeclaration {
	stampsOff := false
	declarations := []framework.EntityDeclaration{
		{Name: "tickets", Fields: []framework.FieldDeclaration{
			{Name: "id", Type: "int", AutoGenerate: "increment"},
			{Name: "subject", Type: "string", Required: true},
		}},
		{Name: "accounts", Fields: []framework.FieldDeclaration{
			{Name: "label", Type: "string", Required: true},
		}},
		{Name: "gadgets", Fields: []framework.FieldDeclaration{
			{Name: "name", Type: "string", Required: true},
			{Name: "ticket_id", Type: "relation", To: "tickets"},
			{Name: "account_id", Type: "relation", To: "accounts"},
		}},
		{Name: "archives", Timestamps: &stampsOff, Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string", Required: true},
		}},
		{Name: "journals", Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string", Required: true},
			{Name: "created_at", Type: "timestamp"},
		}},
	}
	crudOn := true
	for index := range declarations {
		declarations[index].Exposure = &framework.ExposureDeclaration{Public: true, CRUD: &crudOn}
	}
	return declarations
}

// wireShapeRecords creates one row per entity through the real CRUD
// handler on SQLite and returns each response body, keyed by entity.
func wireShapeRecords(t *testing.T, declarations []framework.EntityDeclaration) map[string]map[string]any {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	registry := framework.NewRegistry()
	for _, declaration := range declarations {
		configuration, err := declaration.Config()
		if err != nil {
			t.Fatal(err)
		}
		model := entity.Define(declaration.Name, configuration)
		model.SetDB(database)
		if err := registry.Register(model); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrate.AutoMigrate(database, registry); err != nil {
		t.Fatal(err)
	}
	create := func(name, body string) map[string]any {
		model, err := registry.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		handler := crud.NewCrudHandler(model, database).WithJSONCase(crud.CaseCamel)
		handler.Registry = registry
		request := httptest.NewRequest(http.MethodPost, "/"+name, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.Create()(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", name, response.Code, response.Body.String())
		}
		var record map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if nested, ok := record["data"].(map[string]any); ok {
			record = nested
		}
		return record
	}
	records := map[string]map[string]any{}
	records["tickets"] = create("tickets", `{"subject":"Example"}`)
	records["accounts"] = create("accounts", `{"label":"Main"}`)
	account, _ := records["accounts"]["id"].(string)
	records["gadgets"] = create("gadgets", `{"name":"Gadget","ticketId":1,"accountId":"`+account+`"}`)
	records["archives"] = create("archives", `{"title":"Old"}`)
	records["journals"] = create("journals", `{"title":"Day","createdAt":"2026-11-07T00:00:00Z"}`)
	return records
}

// goStructTags returns the JSON names of a generated Go struct's fields.
func goStructTags(t *testing.T, source, name string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), "client.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok || spec.Name.Name != name {
			return true
		}
		found = true
		for _, field := range spec.Type.(*ast.StructType).Fields.List {
			if field.Tag == nil {
				continue
			}
			tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Get("json")
			key, _, _ := strings.Cut(tag, ",")
			if slices.Contains(tags, key) {
				t.Fatalf("struct %s tags %s twice", name, key)
			}
			if key != "-" {
				tags = append(tags, key)
			}
		}
		return false
	})
	if !found {
		t.Fatalf("client.go declares no %s", name)
	}
	return tags
}

// dtsMembers returns property name → type for one d.ts interface.
func dtsMembers(t *testing.T, declarations, name string) map[string]string {
	t.Helper()
	start := strings.Index(declarations, "export interface "+name+" {\n")
	if start < 0 {
		t.Fatalf("client.d.ts declares no interface %s", name)
	}
	body := declarations[start+len("export interface "+name+" {\n"):]
	body = body[:strings.Index(body, "\n}")]
	members := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		key, typ, ok := strings.Cut(strings.TrimSpace(line), ": ")
		if !ok {
			continue
		}
		key = strings.TrimSuffix(key, "?")
		if _, dup := members[key]; dup {
			t.Fatalf("interface %s declares %s twice", name, key)
		}
		members[key] = strings.TrimSuffix(typ, ";")
	}
	return members
}

// jsFieldConstants returns the snake_case values of <prop>Fields in client.js.
func jsFieldConstants(t *testing.T, source, prop string) map[string]string {
	t.Helper()
	open := "export const " + prop + "Fields = Object.freeze({\n"
	start := strings.Index(source, open)
	if start < 0 {
		t.Fatalf("client.js declares no %sFields", prop)
	}
	body := source[start+len(open):]
	body = body[:strings.Index(body, "});")]
	constants := map[string]string{}
	for _, match := range regexp.MustCompile(`"([^"]+)": "([^"]+)"`).FindAllStringSubmatch(body, -1) {
		constants[match[1]] = match[2]
	}
	return constants
}

// TestSDKShapesMatchCRUDResponse holds the Go struct, the TS interface and
// the JS field constants to what the CRUD handler actually sends: the
// timestamps Define injects are in every response shape, never in a write
// shape, and a relation is typed as the key of the entity it names.
func TestSDKShapesMatchCRUDResponse(t *testing.T) {
	declarations := wireShapeDecls()
	options := SdkOptions{name: "wireshape", module: "example.com/wireshape-sdk"}
	specification, err := buildSDKSpec(declarations, &options)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := renderSDKFiles(specification, []string{"go", "js"}, false)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := map[string]string{}
	for _, file := range generated {
		artifacts[file.Name] = file.Content
	}
	records := wireShapeRecords(t, declarations)
	if _, ok := records["archives"]["createdAt"]; ok {
		t.Fatalf("timestamps-off control carries createdAt: %v", records["archives"])
	}
	for _, key := range []string{"createdAt", "updatedAt"} {
		if _, ok := records["gadgets"][key]; !ok {
			t.Fatalf("gadget response has no %s: %v", key, records["gadgets"])
		}
	}

	goSource, declarationsTS, clientJS := artifacts["go/client.go"], artifacts["js/client.d.ts"], artifacts["js/client.js"]
	for name, record := range records {
		structName := strings.ToUpper(name[:1]) + name[1:]
		tags := goStructTags(t, goSource, structName)
		members := dtsMembers(t, declarationsTS, structName)
		for key := range record {
			if !slices.Contains(tags, key) {
				t.Errorf("Go %s has no field for response key %s (tags %v)", structName, key, tags)
			}
			if _, ok := members[key]; !ok {
				t.Errorf("TS %s has no member for response key %s", structName, key)
			}
		}
		if name == "journals" {
			continue
		}
		for _, write := range []string{structName + "Input", structName + "Patch"} {
			if slices.Contains(goStructTags(t, goSource, write), "createdAt") {
				t.Errorf("Go %s carries the read-only createdAt", write)
			}
		}
		if _, ok := dtsMembers(t, declarationsTS, structName+"Input")["createdAt"]; ok {
			t.Errorf("TS %sInput carries the read-only createdAt", structName)
		}
	}
	if members := dtsMembers(t, declarationsTS, "Archives"); members["createdAt"] != "" {
		t.Errorf("Archives declares createdAt with timestamps off")
	}

	gadgets := jsFieldConstants(t, clientJS, "gadgets")
	for wire, column := range map[string]string{"createdAt": "created_at", "updatedAt": "updated_at", "ticketId": "ticket_id"} {
		if gadgets[wire] != column {
			t.Errorf("gadgetsFields[%s] = %q; want %q (all: %v)", wire, gadgets[wire], column, gadgets)
		}
	}
	if _, ok := jsFieldConstants(t, clientJS, "archives")["createdAt"]; ok {
		t.Errorf("archivesFields names createdAt with timestamps off")
	}

	gadgetMembers := dtsMembers(t, declarationsTS, "Gadgets")
	ticketKey := dtsMembers(t, declarationsTS, "Tickets")["id"]
	accountKey := dtsMembers(t, declarationsTS, "Accounts")["id"]
	if gadgetMembers["ticketId"] != ticketKey+" | null" {
		t.Errorf("Gadgets.ticketId = %q; want the Tickets key %q or null", gadgetMembers["ticketId"], ticketKey)
	}
	if gadgetMembers["accountId"] != accountKey+" | null" || accountKey != "string" {
		t.Errorf("Gadgets.accountId = %q; want the Accounts key %q or null", gadgetMembers["accountId"], accountKey)
	}
	if input := dtsMembers(t, declarationsTS, "GadgetsInput")["ticketId"]; input != ticketKey+" | null" {
		t.Errorf("GadgetsInput.ticketId = %q; want %q or null", input, ticketKey)
	}

	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Log("node is not installed; the client.js runtime check is skipped")
		return
	}
	directory := t.TempDir()
	wire, err := json.Marshal(records["gadgets"])
	if err != nil {
		t.Fatal(err)
	}
	probe := `const { Client } = await import("./client.js");
const client = new Client({ baseURL: "https://fixture.invalid", fetch: async () =>
  new Response(` + "`" + string(wire) + "`" + `, { status: 201, headers: { "Content-Type": "application/json" } }) });
const result = await client.gadgets.create({ name: "Gadget", ticketId: 1 });
console.log(JSON.stringify({ ticketId: typeof result.ticketId, createdAt: typeof result.createdAt }));
`
	for name, content := range map[string]string{"client.js": clientJS, "probe.mjs": probe} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command(nodePath, "probe.mjs")
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, output)
	}
	var runtime map[string]string
	if err := json.Unmarshal(output, &runtime); err != nil {
		t.Fatalf("node output %q: %v", output, err)
	}
	if runtime["ticketId"] != "number" || !strings.Contains(gadgetMembers["ticketId"], "number") {
		t.Errorf("client.js returns ticketId as %s; Gadgets.ticketId is declared %q", runtime["ticketId"], gadgetMembers["ticketId"])
	}
	if runtime["createdAt"] != "string" || gadgetMembers["createdAt"] != "string" {
		t.Errorf("client.js returns createdAt as %s; Gadgets.createdAt is declared %q", runtime["createdAt"], gadgetMembers["createdAt"])
	}
}
