package settings

import (
	"strings"
	"testing"
)

const refundEntry = `endpoints:
  - name: refund_order
    entity: orders
    method: POST
    path: "{id}/refund"
    handler: entities/refunds.go#RefundOrder
    description: Refund a delivered order.
    mcp: true
    public: true
`

func parseEndpoints(t *testing.T, doc string) ([]Endpoint, error) {
	t.Helper()
	f, err := Parse(FileName, []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return f.Endpoints()
}

func TestEndpointsDecode(t *testing.T) {
	got, err := parseEndpoints(t, refundEntry)
	if err != nil {
		t.Fatal(err)
	}
	want := Endpoint{Name: "refund_order", Entity: "orders", Method: "POST", Path: "{id}/refund",
		Handler: "entities/refunds.go#RefundOrder", Description: "Refund a delivered order.", MCP: true, Public: true}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v", got)
	}
	e := got[0]
	if e.HandlerFile() != "entities/refunds.go" || e.HandlerName() != "RefundOrder" {
		t.Fatalf("handler parts = %q, %q", e.HandlerFile(), e.HandlerName())
	}
	if p := e.PathParams(); len(p) != 1 || p[0] != "id" {
		t.Fatalf("path params = %v", p)
	}
}

func TestEndpointsRefuseBadEntries(t *testing.T) {
	entry := func(over map[string]string) string {
		fields := map[string]string{
			"name": "refund_order", "entity": "orders", "method": "POST",
			"path": `"{id}/refund"`, "handler": "entities/refunds.go#RefundOrder",
		}
		for k, v := range over {
			fields[k] = v
		}
		var b strings.Builder
		b.WriteString("endpoints:\n  -")
		first := true
		for _, k := range []string{"name", "entity", "method", "path", "handler", "description", "mcp", "public", "extra"} {
			v, ok := fields[k]
			if !ok || v == "<none>" {
				continue
			}
			if !first {
				b.WriteString("   ")
			}
			first = false
			b.WriteString(" " + k + ": " + v + "\n")
		}
		return b.String()
	}
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"not a list", "endpoints:\n  refund: x\n", "must be a list"},
		{"unknown key", entry(map[string]string{"extra": "1"}), `unknown key "extra"`},
		{"missing handler", entry(map[string]string{"handler": "<none>"}), "handler is required"},
		{"camel name", entry(map[string]string{"name": "refundOrder"}), "must be snake_case"},
		{"lower method", entry(map[string]string{"method": "post"}), `method "post" must be one of`},
		{"absolute path", entry(map[string]string{"path": "/orders/{id}/refund"}), "must be relative"},
		{"trailing slash", entry(map[string]string{"path": `"{id}/refund/"`}), "no leading or trailing slash"},
		{"params only", entry(map[string]string{"path": `"{id}"`}), "needs a literal segment"},
		{"param twice", entry(map[string]string{"path": `"{id}/x/{id}"`}), "names {id} twice"},
		{"dot segment", entry(map[string]string{"path": `"{id}/.."`}), `segment ".."`},
		{"underscore segment", entry(map[string]string{"path": "_batch"}), `segment "_batch"`},
		{"move route", entry(map[string]string{"path": `"{id}/transitions/{key}"`}), "state-move route"},
		{"handler outside entities", entry(map[string]string{"handler": "app/refunds.go#RefundOrder"}), "must name a file in entities/"},
		{"handler traversal", entry(map[string]string{"handler": "entities/../main.go#Main"}), "must name a file in entities/"},
		{"handler test file", entry(map[string]string{"handler": "entities/refunds_test.go#RefundOrder"}), "must name a file in entities/"},
		{"handler gen file", entry(map[string]string{"handler": "entities/endpoints.gen.go#RefundOrder"}), "must name a file in entities/"},
		{"handler unexported", entry(map[string]string{"handler": "entities/refunds.go#refundOrder"}), "must name a file in entities/"},
		{"handler no name", entry(map[string]string{"handler": "entities/refunds.go"}), "must name a file in entities/"},
		{"mcp string", entry(map[string]string{"mcp": "yes"}), "mcp must be true or false"},
		{"public string", entry(map[string]string{"public": `"true"`}), "public must be true or false"},
		{"description newline", entry(map[string]string{"description": `"one\ntwo"`}), "one line of text"},
		{"name not string", entry(map[string]string{"name": "12"}), "name must be a string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseEndpoints(t, tc.doc)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q\n%s", err, tc.want, tc.doc)
			}
		})
	}
}

func TestEndpointsRefuseDuplicates(t *testing.T) {
	second := `  - name: refund_order
    entity: products
    method: POST
    path: restock
    handler: entities/stock.go#Restock
`
	if _, err := parseEndpoints(t, refundEntry+second); err == nil || !strings.Contains(err.Error(), "name refund_order is taken") {
		t.Fatalf("duplicate name: err = %v", err)
	}
	sameRoute := `  - name: refund_again
    entity: orders
    method: POST
    path: "{order}/refund"
    handler: entities/refunds.go#RefundAgain
`
	if _, err := parseEndpoints(t, refundEntry+sameRoute); err == nil || !strings.Contains(err.Error(), "is endpoint refund_order's route already") {
		t.Fatalf("duplicate route: err = %v", err)
	}
	otherMethod := strings.Replace(sameRoute, "POST", "DELETE", 1)
	if _, err := parseEndpoints(t, refundEntry+otherMethod); err != nil {
		t.Fatalf("same path, other method: %v", err)
	}
}
