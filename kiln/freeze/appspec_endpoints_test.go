package freeze_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/settings"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func ordersWorld() *world.World {
	w := world.New()
	w.Entities["orders"] = &world.Entity{Name: "orders", Fields: []world.Field{{Name: "total", Type: "decimal"}}}
	return w
}

// Freeze writes each endpoint the way gofastr.settings.yml holds it: under
// its entity, the path relative to the entity's route, a handler link.
func TestFreezeEndpointsSettingsShape(t *testing.T) {
	w := ordersWorld()
	w.Entities["orders"].Endpoints = []world.EntityEndpoint{{Method: "post", Path: "/orders/{id}/refund", Name: "refund_order", Description: "Refund an order.", MCP: true}}
	w.Endpoints = []*world.EndpointStub{{Method: "GET", Path: "totals", Entity: "orders", Handler: "order_totals"}, {Method: "GET", Path: "summary", Entity: "orders", Handler: "OrderSummary"}}
	buf, err := freeze.AppSpecYAML(w)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := coreyaml.Parse(string(buf))
	if err != nil {
		t.Fatalf("core/yaml rejected freeze output: %v\n%s", err, buf)
	}
	got, err := settings.DecodeEndpoints(doc.Map["endpoints"], "endpoints")
	if err != nil {
		t.Fatalf("the settings decoder refuses freeze's endpoints: %v\n%s", err, buf)
	}
	want := []settings.Endpoint{
		{Name: "refund_order", Entity: "orders", Method: "POST", Path: "{id}/refund", Handler: "entities/orders_endpoints.go#RefundOrder", Description: "Refund an order.", MCP: true},
		{Name: "get_totals", Entity: "orders", Method: "GET", Path: "totals", Handler: "entities/orders_endpoints.go#OrderTotals"},
		{Name: "get_summary", Entity: "orders", Method: "GET", Path: "summary", Handler: "entities/orders_endpoints.go#OrderSummary"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("endpoints = %+v\nwant %+v\n%s", got, want, buf)
	}
	if entity := doc.Map["entities"].List[0]; entity.Map["endpoints"] != nil {
		t.Errorf("the entity still lists endpoints:\n%s", buf)
	}
}

// A surface with no entity to hang under has no app spec spelling, so
// freeze refuses it instead of moving its URL.
func TestFreezeRefusesEntitylessRoutes(t *testing.T) {
	for name, edit := range map[string]func(*world.World){
		"route": func(w *world.World) {
			w.Routes = []*world.Route{{Method: "GET", Path: "/healthz", Action: world.Action{Kind: "respond"}}}
		},
		"no entity": func(w *world.World) {
			w.Endpoints = []*world.EndpointStub{{Method: "GET", Path: "/healthz"}}
		},
		"outside its entity": func(w *world.World) {
			w.Endpoints = []*world.EndpointStub{{Method: "GET", Path: "/reports/daily", Entity: "orders"}}
		},
		"unknown entity": func(w *world.World) {
			w.Endpoints = []*world.EndpointStub{{Method: "GET", Path: "daily", Entity: "reports"}}
		},
	} {
		w := ordersWorld()
		edit(w)
		_, err := freeze.AppSpecYAML(w)
		if err == nil {
			t.Errorf("%s: froze", name)
			continue
		}
		if name != "unknown entity" && !strings.Contains(err.Error(), "owned Go") {
			t.Errorf("%s: %v does not point to a hand-registered route", name, err)
		}
	}
}

// The app serves an entity's endpoints under its table, so an absolute
// path is stripped of /<table>/, not /<name>/.
func TestFreezeStripsTheEntityTable(t *testing.T) {
	w := ordersWorld()
	w.Entities["orders"].Table = "shop_orders"
	w.Endpoints = []*world.EndpointStub{{Method: "POST", Path: "/shop_orders/{id}/refund", Entity: "orders", Handler: "RefundOrder"}}
	buf, err := freeze.AppSpecYAML(w)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := coreyaml.Parse(string(buf))
	if err != nil {
		t.Fatalf("core/yaml rejected freeze output: %v\n%s", err, buf)
	}
	got, err := settings.DecodeEndpoints(doc.Map["endpoints"], "endpoints")
	if err != nil || len(got) != 1 || got[0].Path != "{id}/refund" {
		t.Fatalf("endpoints = %+v, err = %v\n%s", got, err, buf)
	}

	w.Endpoints[0].Path = "/orders/{id}/refund"
	if _, err := freeze.AppSpecYAML(w); err == nil {
		t.Error("froze a path under the entity's name, which is not its route")
	}
}

// Two endpoints whose paths differ only in a path value's name are one
// route; freeze refuses them as generate would.
func TestFreezeRefusesOneRouteTwice(t *testing.T) {
	w := ordersWorld()
	w.Endpoints = []*world.EndpointStub{
		{Method: "POST", Path: "{id}/refund", Entity: "orders", Name: "refund"},
		{Method: "POST", Path: "{order_id}/refund", Entity: "orders", Name: "refund_again"},
	}
	if _, err := freeze.AppSpecYAML(w); err == nil || !strings.Contains(err.Error(), "route already") {
		t.Fatalf("err = %v, want the route refusal", err)
	}
}
