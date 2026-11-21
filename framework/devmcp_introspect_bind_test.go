package framework

import (
	"strings"
	"testing"
)

func devIntrospectionTools(a *App) []string {
	var out []string
	for _, tl := range a.MCP.ListTools() {
		if strings.HasPrefix(tl.Name, "app_") || strings.HasPrefix(tl.Name, "contracts_") {
			out = append(out, tl.Name)
		}
	}
	return out
}

func TestDevIntrospectionWithdrawnOnExposed(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "development")
	t.Setenv("GOFASTR_DEV_MCP", "")
	devApp := func(t *testing.T, guardFirst bool, addr string) *App {
		t.Helper()
		a := NewApp()
		if guardFirst {
			a.guardDevMCPBind(addr, "tcp")
		}
		if err := a.InitPlugins(); err != nil {
			t.Fatalf("InitPlugins: %v", err)
		}
		if !guardFirst {
			a.guardDevMCPBind(addr, "tcp")
		}
		return a
	}
	introspection := []string{"app_routes", "contracts_list", "contracts_explain", "contracts_capabilities"}
	t.Setenv("GOFASTR_DEV_MCP_EXPOSE", "")
	for _, guardFirst := range []bool{true, false} {
		if got := devIntrospectionTools(devApp(t, guardFirst, "0.0.0.0:8080")); len(got) != 0 {
			t.Errorf("SECURITY: [exposure] exposed dev bind (guard first %v) kept %v", guardFirst, got)
		}
	}

	loop := devIntrospectionTools(devApp(t, false, "localhost:8080"))
	if !hasAll(loop, introspection...) || !hasAll(loop, "app_config", "contracts_verify", "contracts_fix") {
		t.Errorf("a loopback dev bind must keep its introspection tools, got %v", loop)
	}

	t.Setenv("GOFASTR_DEV_MCP_EXPOSE", "1")
	if got := devIntrospectionTools(devApp(t, false, "0.0.0.0:8080")); !hasAll(got, introspection...) || !hasAll(got, "contracts_verify") {
		t.Errorf("GOFASTR_DEV_MCP_EXPOSE=1 must keep the introspection tools, got %v", got)
	}
	t.Setenv("GOFASTR_DEV_MCP_EXPOSE", "")

	// An explicit WithMCPIntrospection is a production choice, not dev's.
	t.Setenv("GOFASTR_DEV", "0")
	explicit := NewApp(WithMCPIntrospection())
	if err := explicit.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	explicit.guardDevMCPBind("0.0.0.0:8080", "tcp")
	if got := devIntrospectionTools(explicit); !hasAll(got, introspection...) {
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
