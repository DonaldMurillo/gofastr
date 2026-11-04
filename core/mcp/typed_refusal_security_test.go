package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

func TestToolCallsKeepTypedClientRefusals(t *testing.T) {
	type input struct {
		Query string `json:"q"`
	}
	for _, test := range []struct {
		name string
		args map[string]any
		err  error
		code int
	}{
		{"bind", map[string]any{"q": "hi", "bogus": 1}, nil, ErrInvalidParams},
		{"conflict", nil, handler.WrapError(409, handler.ReasonInvalidParams, errors.New("private driver detail")), ErrInvalidParams},
		{"wrapped", nil, fmt.Errorf("private wrapper: %w", handler.Fail(400, handler.ReasonInvalidParams)), ErrInvalidParams},
		{"internal", nil, handler.Errorf(500, "private server detail"), ErrInternalError},
		{"plain", nil, errors.New("private filesystem detail"), ErrInternalError},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := NewServer()
			adapter := handler.ToolAdapter(handler.Handler[input, string](func(context.Context, input) (string, error) {
				return "", test.err
			}))
			if err := server.RegisterTool("echo", "Echo", map[string]any{"type": "object"}, adapter); err != nil {
				t.Fatal(err)
			}
			params, err := json.Marshal(map[string]any{"name": "echo", "arguments": test.args})
			if err != nil {
				t.Fatal(err)
			}
			response := server.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})
			if response.Error == nil || response.Error.Code != test.code {
				t.Fatalf("error = %+v; want code %d", response.Error, test.code)
			}
			if strings.Contains(response.Error.Message, "private") {
				t.Fatalf("internal cause leaked: %s", response.Error.Message)
			}
			if test.code == ErrInvalidParams {
				data, ok := response.Error.Data.(map[string]any)
				if !ok || data["reason"] == "" || data["reason"] == nil {
					t.Fatalf("typed refusal lost its reason: %+v", response.Error)
				}
			} else if response.Error.Message != "internal tool error" {
				t.Fatalf("internal failure was not generic: %+v", response.Error)
			}
		})
	}
}
