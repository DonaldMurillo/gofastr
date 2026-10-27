package framework

import (
	"strings"
	"testing"
)

// devIntrospectionTools names the dev-implied introspection surface in a's
// registry: the app_* tools (routes, goroutine stacks, config) and
// contracts_verify (source-tree lint output).
func devIntrospectionTools(a *App) []string {
	var out []string
	for _, tl := range a.MCP.ListTools() {
		if strings.HasPrefix(tl.Name, "app_") || tl.Name == "contracts_verify" {
			out = append(out, tl.Name)
		}
	}
	return out
}

// The bind guard withdrew the dev control tools and the WithDevImplied set
// on a non-loopback bind, but the introspection tools dev turns on stayed
// live to any TCP client that sets Host: localhost. They go the way the
// log read tools go, unless GOFASTR_DEV_MCP_EXPOSE accepts the risk.
func TestDevIntrospectionWithdrawnOnExposed(t *testing.T) {
	devApp := func(t *testing.T, guardFirst bool, addr string) *App {
		t.Helper()
		a := NewApp()
		a.mcpIntrospection, a.mcpIntrospectionDevImplied = true, true
		if guardFirst {
			a.guardDevMCPBind(addr)
		}
		if err := a.InitPlugins(); err != nil {
			t.Fatalf("InitPlugins: %v", err)
		}
		if !guardFirst {
			a.guardDevMCPBind(addr)
		}
		return a
	}
	t.Setenv("GOFASTR_DEV_MCP_EXPOSE", "")
	for _, guardFirst := range []bool{true, false} {
		if got := devIntrospectionTools(devApp(t, guardFirst, "0.0.0.0:8080")); len(got) != 0 {
			t.Errorf("SECURITY: [exposure] exposed dev bind (guard first %v) kept %v", guardFirst, got)
		}
	}

	loop := devIntrospectionTools(devApp(t, false, "localhost:8080"))
	if !hasAll(loop, "app_routes", "app_config", "contracts_verify") {
		t.Errorf("a loopback dev bind must keep its introspection tools, got %v", loop)
	}

	t.Setenv("GOFASTR_DEV_MCP_EXPOSE", "1")
	if got := devIntrospectionTools(devApp(t, false, "0.0.0.0:8080")); !hasAll(got, "app_routes", "contracts_verify") {
		t.Errorf("GOFASTR_DEV_MCP_EXPOSE=1 must keep the introspection tools, got %v", got)
	}
	t.Setenv("GOFASTR_DEV_MCP_EXPOSE", "")

	// An explicit WithMCPIntrospection is a production choice, not dev's.
	explicit := NewApp(WithMCPIntrospection())
	explicit.guardDevMCPBind("0.0.0.0:8080")
	if err := explicit.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	if got := devIntrospectionTools(explicit); !hasAll(got, "app_routes") {
		t.Errorf("an explicit WithMCPIntrospection was withdrawn by the dev bind guard, got %v", got)
	}
}

func hasAll(have []string, want ...string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
