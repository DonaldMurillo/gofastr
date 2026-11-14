package appspec

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/settings"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestMCPRequiresCRUD(t *testing.T) {
	for _, exposure := range []string{"    exposure:\n      mcp: true\n      crud: false\n", "    mcp: true\n    crud: false\n"} {
		n, err := coreyaml.Parse("app:\n  name: Demo\nentities:\n  - name: tickets\n" + exposure + "    fields:\n      - name: title\n        type: string\n")
		if err != nil {
			t.Fatal(err)
		}
		bp, err := DecodeAppSpec(n)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateAppSpec(bp); err == nil || !strings.Contains(err.Error(), "exposure") || !strings.Contains(err.Error(), "MCP=true with CRUD=false") {
			t.Fatalf("validate = %v; want entity exposure refusal", err)
		}
	}
}

func TestEndpointOwnRoutes(t *testing.T) {
	for _, tc := range []struct{ method, path string }{{"GET", "_events"}, {"POST", "_bulk"}, {"POST", "_pick"}, {"POST", "_batch"}} {
		bp := AppSpec{App: AppSpecApp{Name: "Demo", APIPrefix: "v2"},
			Entities:  []framework.EntityDeclaration{{Name: "tickets", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}},
			Endpoints: []settings.Endpoint{{Name: "ticket_action", Entity: "tickets", Method: tc.method, Path: tc.path, Handler: "entities/tickets_endpoints.go#TicketAction"}},
		}
		err := ValidateAppSpec(bp)
		if err == nil || !strings.Contains(err.Error(), "endpoints[0]") || !strings.Contains(err.Error(), `segment "`+tc.path+`"`) {
			t.Errorf("%s %s: validate = %v; want indexed endpoint shape refusal", tc.method, tc.path, err)
		}
	}
}

func TestNavWarningsOnly(t *testing.T) {
	bp := AppSpec{App: AppSpecApp{Name: "Demo"}, Nav: []AppSpecNavItem{{Label: "Owned route", Href: "/owned"}}}
	if err := ValidateAppSpec(bp); err != nil {
		t.Fatalf("nav href must only warn: %v", err)
	}
}

func TestEndpointGroupRoutes(t *testing.T) {
	bp := AppSpec{App: AppSpecApp{Name: "Demo", APIPrefix: "api"}, Entities: []framework.EntityDeclaration{{Name: "tickets", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}}, Endpoints: []settings.Endpoint{
		{Name: "events", Entity: "tickets", Method: "GET", Path: "events/{id}", Handler: "entities/tickets_endpoints.go#Events"},
		{Name: "history", Entity: "tickets", Method: "GET", Path: "{id}/history", Handler: "entities/tickets_endpoints.go#History"},
	}}
	if err := ValidateAppSpecEndpoints(bp); err == nil || !strings.Contains(err.Error(), "endpoints[1].path") || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("sibling overlap must name endpoints[1].path: %v", err)
	}
	malformed := bp.Endpoints[0]
	malformed.Name = "malformed"
	malformed.Path = "_bulk"
	bp.Endpoints = append([]settings.Endpoint{malformed}, bp.Endpoints...)
	if err := ValidateAppSpecEndpoints(bp); err == nil || !strings.Contains(err.Error(), "endpoints[2].path") || !strings.Contains(err.Error(), `segment "_bulk"`) {
		t.Fatalf("skipping bad shapes must preserve original spec indices: %v", err)
	}
	bp.Endpoints = bp.Endpoints[1:]
	bp.Endpoints = bp.Endpoints[:1]
	bp.Endpoints[0].Path = "_events"
	if err := ValidateAppSpecEndpoints(bp); err == nil || !strings.Contains(err.Error(), `segment "_events"`) || strings.Contains(err.Error(), "would register") {
		t.Fatalf("shape must be refused before route collision: %v", err)
	}
}
