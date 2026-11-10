package entity

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Endpoint and EntityDeclaration carry JSON tags: Endpoint names its typed
// request/response descriptions "inputSchema" / "outputSchema" and tags its
// own func fields `json:"-"` so the struct encodes. An endpoint that
// declares an InputSchema encodes, and so does the declaration holding it.
//
// The bug this holds: schema.Field gained the untagged func field Mask, so
// neither could be encoded any more, though the same JSON still decoded.
func TestEndpointWithInputSchemaEncodes(t *testing.T) {
	ep := Endpoint{
		Method: "POST", Path: "{id}/publish", Name: "publish",
		InputSchema: []schema.Field{{Name: "rank", Type: schema.Int, Required: true}},
	}
	b, err := json.Marshal(ep)
	if err != nil {
		t.Fatalf("json.Marshal(Endpoint with InputSchema) = %v; want the tagged JSON shape", err)
	}
	if !strings.Contains(string(b), `"inputSchema"`) {
		t.Fatalf("encoded endpoint lost inputSchema: %s", b)
	}
}

// A declaration that decodes from JSON encodes again, endpoint schemas
// included.
func TestDeclarationWithEndpointSchemaRoundTrips(t *testing.T) {
	const src = `{"name":"posts","fields":[{"name":"title","type":"string"}],
		"endpoints":[{"method":"POST","path":"{id}/publish","inputSchema":[{"Name":"rank","Type":2,"Required":true}]}]}`
	var decl EntityDeclaration
	if err := json.Unmarshal([]byte(src), &decl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decl.Endpoints) != 1 || len(decl.Endpoints[0].InputSchema) != 1 {
		t.Fatalf("decode dropped the endpoint schema: %+v", decl.Endpoints)
	}
	b, err := json.Marshal(decl)
	if err != nil {
		t.Fatalf("a declaration that decoded cannot be encoded again: %v", err)
	}
	var again EntityDeclaration
	if err := json.Unmarshal(b, &again); err != nil {
		t.Fatalf("decode of the re-encoded declaration: %v", err)
	}
	if len(again.Endpoints) != 1 || len(again.Endpoints[0].InputSchema) != 1 ||
		again.Endpoints[0].InputSchema[0].Name != "rank" || again.Endpoints[0].InputSchema[0].Type != schema.Int {
		t.Fatalf("the endpoint schema did not survive the round trip: %s", b)
	}
}
