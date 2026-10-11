package framework

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

// App pre-flight: an endpoint whose MCP tool name equals one of the
// entity's move tools is refused before anything registers, the same
// guard that holds the five CRUD tool names. A System move claims no tool
// name, so its spelling stays free.
func TestAppEndpointCollidingWithMoveTool(t *testing.T) {
	app := atomicTestApp(t)
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	tool := func(context.Context, map[string]any) (any, error) { return nil, nil }

	err := app.TryEntity("invoices", EntityConfig{
		Table:    "invoices",
		Fields:   statesAuditFields(),
		States:   statesAuditStates(),
		Exposure: &entity.ExposureConfig{MCP: true},
		Endpoints: []entity.Endpoint{{
			Method: "POST", Path: "/resend", Name: "invoices_pay",
			MCP: true, Handler: noop, MCPHandler: tool,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), `MCP tool name "invoices_pay" is already claimed`) {
		t.Fatalf("err = %v, want the move-tool collision named", err)
	}
	if _, gerr := app.Registry.Get("invoices"); gerr == nil {
		t.Fatal("rejected entity must not remain in the registry")
	}

	// The same spelling as a System move registers: a System move claims
	// no tool, so the name is free for an endpoint.
	if rerr := app.TryEntity("invoices", EntityConfig{
		Table:    "invoices",
		Fields:   statesAuditFields(),
		States:   withSystemMove(statesAuditStates()),
		Exposure: &entity.ExposureConfig{MCP: true},
		Endpoints: []entity.Endpoint{{
			Method: "POST", Path: "/flag", Name: "invoices_mark_overdue",
			MCP: true, Handler: noop, MCPHandler: tool,
		}},
	}); rerr != nil {
		t.Fatalf("a System move's tool name must stay free: %v", rerr)
	}
	if !app.MCP.HasTool("invoices_mark_overdue") {
		t.Fatal("the retried endpoint tool did not register")
	}
	// The moves that declared non-system keys own their tool names.
	for _, name := range []string{"invoices_pay", "invoices_void"} {
		if !app.MCP.HasTool(name) {
			t.Fatalf("move tool %q missing after registration", name)
		}
	}
}

// withSystemMove adds the System move whose flat tool name the retry uses.
func withSystemMove(st *entity.StatesConfig) *entity.StatesConfig {
	st.Transitions = append(st.Transitions, entity.Transition{
		Key: "mark_overdue", From: []string{"open"}, To: "void", System: true,
	})
	return st
}

// GroupEntity runs the same preflight before it registers anything: a
// grouped endpoint on the generated transition route, or one whose tool
// name a namespaced move tool claims, is refused with the registry, the
// router and the MCP server untouched.
func TestGroupEntityPreflightBeforeRegister(t *testing.T) {
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	tool := func(context.Context, map[string]any) (any, error) { return nil, nil }
	cases := map[string]entity.Endpoint{
		"route": {Method: "POST", Path: "{id}/transitions/{key}", Handler: noop},
		"tool":  {Method: "POST", Path: "/resend", Name: "invoices.pay", MCP: true, Handler: noop, MCPHandler: tool},
	}
	for name, ep := range cases {
		t.Run(name, func(t *testing.T) {
			app := atomicTestApp(t)
			g := app.Group("/v1", routegroup.WithMCPNamespace("billing"))
			routes := len(app.router.Routes())
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("GroupEntity accepted the colliding endpoint")
					}
				}()
				app.GroupEntity(g, "invoices", EntityConfig{
					Table:     "invoices",
					Fields:    statesAuditFields(),
					States:    statesAuditStates(),
					Exposure:  &entity.ExposureConfig{MCP: true},
					Endpoints: []entity.Endpoint{ep},
				})
			}()
			if _, err := app.Registry.GetVersioned("invoices", "/v1"); err == nil {
				t.Fatal("refused grouped entity remains in the registry")
			}
			if n := len(app.router.Routes()); n != routes {
				t.Fatalf("refused grouped entity mounted %d routes", n-routes)
			}
			if app.MCP.HasTool("billing.invoices.list") {
				t.Fatal("refused grouped entity registered its MCP tools")
			}
		})
	}
}
