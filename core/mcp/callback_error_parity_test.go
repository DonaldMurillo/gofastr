package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

func TestNilGateOptionsPanic(t *testing.T) {
	for name, opt := range map[string]func(){
		"WithToolGate":             func() { WithToolGate(nil) },
		"WithResourceGate":         func() { WithResourceGate(nil) },
		"WithResourceTemplateGate": func() { WithResourceTemplateGate(nil) },
		"WithPromptGate":           func() { WithPromptGate(nil) },
		"Gated":                    func() { Gated(nil, func(context.Context, map[string]any) (any, error) { return nil, nil }) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("a nil gate was accepted")
				}
			}()
			opt()
		})
	}
}

// One callback error answers the same on tools/call, prompts/get and resources/read.
func TestCallbackErrorAnswersAlikeOnEveryMethod(t *testing.T) {
	cases := []struct {
		name     string
		err      func() error
		code     int
		message  string
		httpCode any
	}{
		{"rpc", func() error { return &RPCError{Code: -32001, Message: "plan limit"} }, -32001, "plan limit", nil},
		{"wrapped_rpc", func() error {
			return fmt.Errorf("lookup: %w", &RPCError{Code: ErrInvalidParams, Message: "unknown version"})
		}, ErrInvalidParams, "unknown version", nil},
		{"typed_4xx", func() error { return &handler.Error{Code: 404, Message: "no such invoice"} }, ErrInvalidParams, "no such invoice", float64(404)},
		{"typed_5xx", func() error { return &handler.Error{Code: 500, Message: "dsn=secret"} }, ErrInternalError, "", nil},
		{"plain", func() error { return fmt.Errorf("open /etc/app.db: denied") }, ErrInternalError, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer()
			if err := s.RegisterTool("x", "", nil, func(context.Context, map[string]any) (any, error) { return nil, tc.err() }); err != nil {
				t.Fatal(err)
			}
			if err := s.RegisterPrompt("x", func(context.Context, map[string]string) ([]PromptMessage, error) { return nil, tc.err() }); err != nil {
				t.Fatal(err)
			}
			if err := s.RegisterResource("r://x", "x", "text/plain", func(context.Context) (ResourceContents, error) {
				return ResourceContents{}, tc.err()
			}); err != nil {
				t.Fatal(err)
			}
			for method, params := range map[string]string{
				"tools/call":     `{"name":"x"}`,
				"prompts/get":    `{"name":"x"}`,
				"resources/read": `{"uri":"r://x"}`,
			} {
				got := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: method, Params: json.RawMessage(params)}).Error
				if got == nil || got.Code != tc.code {
					t.Fatalf("%s: got %+v, want code %d", method, got, tc.code)
				}
				if tc.message != "" && got.Message != tc.message {
					t.Errorf("%s: message %q, want %q", method, got.Message, tc.message)
				}
				if tc.message == "" && (got.Data != nil || len(got.Message) > len("internal resource error")) {
					t.Errorf("%s: internal error carries detail: %+v", method, got)
				}
				if tc.httpCode != nil {
					b, _ := json.Marshal(got.Data)
					var data map[string]any
					_ = json.Unmarshal(b, &data)
					if data["code"] != tc.httpCode {
						t.Errorf("%s: data.code = %v, want %v", method, data["code"], tc.httpCode)
					}
				}
			}
		})
	}
}
