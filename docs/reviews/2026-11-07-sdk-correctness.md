# SDK generation correctness review — November 7, 2026

Reviewed revision: `953ccc08256e825bdc17254c463a5695a27092b6`.
Two independent root causes were reproduced. Neither is fixed.

## C04 — P3: generated response types omit injected timestamp fields

Roots: `cmd/gofastr/internal/appgen/generate_client.go:564` and
`cmd/gofastr/internal/appgen/generate_sdkjs.go:214` / `:267`.

The emitters walk declaration fields, but default `created_at` and `updated_at`
fields are injected later by entity definition. A normal CRUD response and
the same registry's OpenAPI schema both contain `createdAt` and `updatedAt`.
The generated Go response struct and TypeScript response interface omit them;
the JavaScript query-field constants omit these valid filter/sort columns too.

Independent evidence: create a normal entity with timestamps left at their
default setting, generate both SDK targets, and invoke its actual CRUD create
handler against an isolated auto-migrated SQLite database. The returned record
contains both timestamps; OpenAPI describes both as read-only date-time strings;
the generated Go `Gadgets` struct has only `ID`, `Name`, `TicketId`, `Included`.
The actual generated JavaScript client retains the timestamp values at runtime,
but its declarations and query-field catalog do not expose them.

**Narrowed from the reviewer claim:** omission from writable Input/Patch types
is not a bug: these timestamps are read-only. This finding concerns response
types and query-field catalogs, not making timestamps client-writable.

## C05 — P3: integer-key relations are declared as strings in TypeScript

Root: `cmd/gofastr/internal/appgen/generate_sdkjs.go:390` —
`tsTypeForField` resolves scalar integer fields but defaults relations to
`string`, without resolving the related entity's key type. The same underlying
conversion serves both the response interface and input interface.

Normal usage: a `tickets` entity has an incrementing integer id and a
`gadgets.ticket_id` relation targets it. Create ticket 1, then create a gadget
using `ticketId: 1`. The actual CRUD handler sends a JSON number; the actual
generated JavaScript client returns a number. OpenAPI correctly declares an
integer-key relation. However, generated `Gadgets` and `GadgetsInput` both
declare `ticketId?: string | null`, while `Tickets.id` is `number | string`.

The ordinary create/read chain therefore exposes a static type different
from the value the client actually returns. The Go client's separate relation
decoder is not the defect being counted. Read and write declaration symptoms
share one root cause and count once.

**Limits:** no TypeScript compiler is installed here, so no `tsc` diagnostic
is claimed. The generated declaration text, the actual HTTP-handler JSON,
and the generated JavaScript runtime value were independently checked.
SQLite was executed; no Postgres execution is claimed.

## Retained independent reproduction

The Go overlay invokes the real SDK emitters and actual CRUD HTTP create
handlers, then writes generated artifacts and the returned record beneath a
temporary directory. The Node probe invokes that generated client using
the captured real response through an injected fetch implementation. It uses
no browser, external service, credentials, network listener or dependency install.
Go AST inspection verifies the generated response struct directly.

The assertions reproduce today's mismatches; they are not correct behavior
to retain after implementing fixes.

```go
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
    "path/filepath"
    "strings"
    "testing"

    "github.com/DonaldMurillo/gofastr/framework"
    "github.com/DonaldMurillo/gofastr/framework/crud"
    "github.com/DonaldMurillo/gofastr/framework/entity"
    "github.com/DonaldMurillo/gofastr/framework/migrate"
    entityopenapi "github.com/DonaldMurillo/gofastr/framework/openapi"
    _ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func TestCoordinatorSDKSurfaceReproduction(t *testing.T) {
    require := func(err error) { t.Helper(); if err != nil { t.Fatal(err) } }
    output := os.Getenv("GOFASTR_REVIEW_OUT")
    if output == "" { t.Fatal("Set GOFASTR_REVIEW_OUT to a temporary artifact directory") }
    require(os.MkdirAll(output, 0700))
    crudEnabled := true
    declarations := []framework.EntityDeclaration{
        {Name: "tickets", Fields: []framework.FieldDeclaration{
            {Name: "id", Type: "int", AutoGenerate: "increment"},
            {Name: "subject", Type: "string", Required: true},
        }, Exposure: &framework.ExposureDeclaration{Public: true, CRUD: &crudEnabled}},
        {Name: "gadgets", Fields: []framework.FieldDeclaration{
            {Name: "name", Type: "string", Required: true},
            {Name: "ticket_id", Type: "relation", To: "tickets"},
        }, Exposure: &framework.ExposureDeclaration{Public: true, CRUD: &crudEnabled}},
    }
    options := SdkOptions{name: "coordinatorprobe", module: "example.com/coordinator-sdk"}
    specification, err := buildSDKSpec(declarations, &options)
    require(err)
    generated, err := renderSDKFiles(specification, []string{"go", "js"}, false)
    require(err)
    artifacts := map[string]string{}
    for _, file := range generated {
        artifacts[file.Name] = file.Content
        destination := filepath.Join(output, file.Name)
        require(os.MkdirAll(filepath.Dir(destination), 0700))
        require(os.WriteFile(destination, []byte(file.Content), 0600))
    }
    database, err := sql.Open("sqlite3", ":memory:")
    require(err)
    defer database.Close()
    database.SetMaxOpenConns(1)
    registry := framework.NewRegistry()
    for _, declaration := range declarations {
        configuration, err := declaration.Config()
        require(err)
        model := entity.Define(declaration.Name, configuration)
        model.SetDB(database)
        require(registry.Register(model))
    }
    require(migrate.AutoMigrate(database, registry))
    create := func(name, body string) map[string]any {
        model, err := registry.Get(name)
        require(err)
        handler := crud.NewCrudHandler(model, database).WithJSONCase(crud.CaseCamel)
        handler.Registry = registry
        request := httptest.NewRequest(http.MethodPost, "/"+name, strings.NewReader(body))
        request.Header.Set("Content-Type", "application/json")
        response := httptest.NewRecorder()
        handler.Create()(response, request)
        if response.Code != http.StatusCreated { t.Fatalf("Create %s: %d %s", name, response.Code, response.Body.String()) }
        var record map[string]any
        require(json.Unmarshal(response.Body.Bytes(), &record))
        if nested, ok := record["data"].(map[string]any); ok { record = nested }
        return record
    }
    ticket := create("tickets", `{"subject":"Example"}`)
    record := create("gadgets", `{"name":"Gadget","ticketId":1}`)
    if ticket["id"] != float64(1) || record["ticketId"] != float64(1) { t.Fatalf("Unexpected numeric relation: ticket=%v record=%v", ticket, record) }
    for _, key := range []string{"createdAt", "updatedAt"} {
        if value, ok := record[key].(string); !ok || value == "" { t.Fatalf("Missing timestamp control %s: %v", key, record) }
    }
    wire, err := json.Marshal(record)
    require(err)
    require(os.WriteFile(filepath.Join(output, "record.json"), wire, 0600))
    t.Logf("Actual CRUD response: %s", wire)
    document := entityopenapi.EntityOpenAPI(registry, "Review", "1", nil).Build()
    schemaDocument, err := json.Marshal(document)
    require(err)
    require(os.WriteFile(filepath.Join(output, "openapi.json"), schemaDocument, 0600))
    var plainDocument map[string]any
    require(json.Unmarshal(schemaDocument, &plainDocument))
    components := plainDocument["components"].(map[string]any)
    schemas := components["schemas"].(map[string]any)
    properties := schemas["gadgets"].(map[string]any)["properties"].(map[string]any)
    for _, key := range []string{"createdAt", "updatedAt", "ticketId"} {
        if _, ok := properties[key]; !ok { t.Fatalf("OpenAPI property %s missing", key) }
        t.Logf("OpenAPI %s: %v", key, properties[key])
    }
    parsed, err := parser.ParseFile(token.NewFileSet(), "client.go", artifacts["go/client.go"], 0)
    require(err)
    var fields []string
    ast.Inspect(parsed, func(node ast.Node) bool {
        declaration, ok := node.(*ast.TypeSpec)
        if !ok || declaration.Name.Name != "Gadgets" { return true }
        structure, ok := declaration.Type.(*ast.StructType)
        if !ok { t.Fatal("Gadgets is not a struct") }
        for _, field := range structure.Fields.List { for _, name := range field.Names { fields = append(fields, name.Name) } }
        return false
    })
    t.Logf("Generated Go Gadgets fields: %v", fields)
    if len(fields) < 3 { t.Fatal("Generated struct control missing") }
    for _, name := range fields {
        if name == "CreatedAt" || name == "UpdatedAt" { t.Fatal("Timestamp omission did not reproduce") }
    }
    declarationsJS := artifacts["js/client.d.ts"]
    if strings.Contains(declarationsJS, "createdAt") || strings.Contains(declarationsJS, "updatedAt") { t.Fatal("Timestamp declaration omission did not reproduce") }
    if !strings.Contains(declarationsJS, "ticketId?: string | null;") || !strings.Contains(declarationsJS, "id: number | string;") { t.Fatal("Relation type mismatch did not reproduce") }
    if strings.Contains(artifacts["js/client.js"], `"created_at": "created_at"`) { t.Fatal("Unexpected timestamp query constant") }
    t.Log("Confirmed: response timestamps absent from Go/TS shapes; integer relation declared string in TS")
}
```

```javascript
import fs from 'node:fs';
import path from 'node:path';
const directory = process.argv[2];
const source = fs.readFileSync(path.join(directory, 'js/client.js'), 'utf8');
const { Client, gadgetsFields } = await import('data:text/javascript;base64,' + Buffer.from(source).toString('base64'));
const record = JSON.parse(fs.readFileSync(path.join(directory, 'record.json'), 'utf8'));
const declarations = fs.readFileSync(path.join(directory, 'js/client.d.ts'), 'utf8');
const client = new Client({
  baseURL: 'https://fixture.invalid',
  fetch: async () => new Response(JSON.stringify(record), { status: 200, headers: { 'Content-Type': 'application/json' } }),
});
const result = await client.gadgets.create({ name: 'Gadget', ticketId: 1 });
if (typeof result.ticketId !== 'number') throw new Error('Numeric relation control failed');
if (typeof result.createdAt !== 'string' || typeof result.updatedAt !== 'string') throw new Error('Timestamp control failed');
if ('createdAt' in gadgetsFields || 'updatedAt' in gadgetsFields) throw new Error('Query constant omission did not reproduce');
if (!declarations.includes('ticketId?: string | null;')) throw new Error('Relation declaration mismatch did not reproduce');
console.log(JSON.stringify({ ticketId: result.ticketId, relationRuntimeType: typeof result.ticketId, createdAt: result.createdAt, updatedAt: result.updatedAt, queryFields: gadgetsFields, timestampFieldsInDeclarations: /createdAt|updatedAt/.test(declarations) }, null, 2));
```

Run from the checkout root:

```sh
probe="$(mktemp -d /tmp/gofastr-sdk-retained.XXXXXX)"
awk '/^```go$/{inside=1;next} /^```$/{inside=0} inside' \
  docs/reviews/2026-11-07-sdk-correctness.md > "$probe/sdk_test.go"
awk '/^```javascript$/{inside=1;next} /^```$/{inside=0} inside' \
  docs/reviews/2026-11-07-sdk-correctness.md > "$probe/check.mjs"
printf '{"Replace":{"%s/cmd/gofastr/internal/appgen/zz_coordinator_sdk_test.go":"%s/sdk_test.go"}}\n' \
  "$PWD" "$probe" > "$probe/overlay.json"
GOFASTR_REVIEW_OUT="$probe/artifacts" go test -p 2 -count=1 \
  -overlay "$probe/overlay.json" -run "^TestCoordinatorSDKSurfaceReproduction$" -v \
  ./cmd/gofastr/internal/appgen/
node "$probe/check.mjs" "$probe/artifacts"
```

Expected evidence: `ticketId` has runtime type `number`; timestamp values
are present in the captured/client result; query-field constants contain
only `name` and `ticketId`; TypeScript declarations lack timestamp fields.
The Go probe also logs the matching OpenAPI properties and generated Go field list.

## Existing scoped checks

- `go test -p 2 -count=1 ./cmd/gofastr/internal/appgen/ -run '^(TestRenderSDKFiles_Layout|TestSDKJSTypedSurface|TestSDKJSDeclarationsInSync|TestRenderClient_StructsAndMethods)$'`
- `go vet -p 2 ./cmd/gofastr/internal/appgen/`

These tests can remain green while the two declaration/runtime gaps reproduce.
