package framework

import (
	"context"
	"testing"
)

func TestContractToolsFollowDevMode(t *testing.T) {
	for _, testCase := range []struct {
		name, environment, dev, mcp string
		explicit, want              bool
	}{
		{"explicit dev", "development", "1", "", true, true},
		{"implicit dev", "development", "1", "", false, true},
		{"disabled", "development", "1", "0", true, false},
		{"production", "production", "1", "", true, false},
		{"outside dev", "development", "0", "", true, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("GOFASTR_DEV", testCase.dev)
			t.Setenv("GOFASTR_ENV", testCase.environment)
			t.Setenv("GOFASTR_DEV_MCP", testCase.mcp)
			options := []AppOption{WithMCP()}
			if testCase.explicit {
				options = append(options, WithMCPIntrospection())
			}
			application := NewApp(options...)
			if err := application.InitPlugins(); err != nil {
				t.Fatal(err)
			}
			names := toolNames(t, application, context.Background())
			for _, name := range []string{"contracts_verify", "contracts_fix"} {
				if got := hasTool(names, name); got != testCase.want {
					t.Errorf("%s registered = %v, want %v", name, got, testCase.want)
				}
			}
		})
	}
}
