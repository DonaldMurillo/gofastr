package framework

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Property (#669): an entity endpoint whose MCPHandler is mcp.Gated keeps
// its gate ahead of argument validation once the framework wraps it in the
// endpoint twin. A refused caller gets the unknown-tool answer whatever
// arguments it sends, so a missing or mistyped argument cannot confirm the
// tool exists or name its required fields. Root and grouped registration
// both wrap the handler, so both are covered.
func TestEndpointTwinKeepsGatedGate(t *testing.T) {
	for _, grouped := range []bool{false, true} {
		name := "root"
		if grouped {
			name = "group"
		}
		t.Run(name, func(t *testing.T) {
			app := NewApp(WithoutDefaultMiddleware())
			gateCalls := 0
			config := entity.EntityConfig{
				Exposure: &entity.ExposureConfig{CRUD: new(false)},
				Endpoints: []entity.Endpoint{{
					Name: "private_export", Method: "POST", Path: "export", MCP: true, Public: true,
					InputSchema: []schema.Field{{Name: "secret", Type: schema.String, Required: true}},
					MCPHandler: mcp.Gated(func(context.Context) error {
						gateCalls++
						return errors.New("private")
					}, func(context.Context, map[string]any) (any, error) {
						t.Fatal("refused handler executed")
						return nil, nil
					}),
				}},
			}.WithTimestamps(false)
			if grouped {
				app.GroupEntity(app.Group("/v2"), "reports", config)
			} else {
				app.Entity("reports", config)
			}

			call := func(args map[string]any) *mcp.RPCError {
				t.Helper()
				gateCalls = 0
				_, err := app.MCP.CallTool(context.Background(), "private_export", args)
				var rpc *mcp.RPCError
				if !errors.As(err, &rpc) {
					t.Fatalf("args %v: err = %v, want an RPC error", args, err)
				}
				if gateCalls != 1 {
					t.Fatalf("args %v: gate ran %d times, want exactly once", args, gateCalls)
				}
				return rpc
			}

			want := call(map[string]any{"secret": "valid"})
			if want.Code != mcp.ErrMethodNotFound {
				t.Fatalf("valid args = {%d %q}, want the unknown-tool answer", want.Code, want.Message)
			}
			for _, tc := range []struct {
				label string
				args  map[string]any
			}{
				{"missing", map[string]any{}},
				{"mistyped", map[string]any{"secret": 42}},
				{"extra", map[string]any{"secret": "valid", "extra": true}},
			} {
				got := call(tc.args)
				if got.Code != want.Code || got.Message != want.Message {
					t.Errorf("%s args = {%d %q}, valid args = {%d %q}; want the identical hidden-tool refusal",
						tc.label, got.Code, got.Message, want.Code, want.Message)
				}
			}
		})
	}
}
