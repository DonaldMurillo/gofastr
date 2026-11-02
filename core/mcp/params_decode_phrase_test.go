package mcp

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
)

// goInternals is what encoding/json's own text carried into a JSON-RPC
// error message: the params struct's name and Go type spellings.
var goInternals = regexp.MustCompile(`interface \{\}|map\[|struct|Params\.|Go value|of type|json: |cannot unmarshal|invalid character`)

// A params object of the wrong shape answered "invalid params: json:
// cannot unmarshal number into Go struct field promptsGetParams.name of
// type string": the server's own type names. The message now names the
// JSON path and the JSON type.
func TestParamsDecodeErrorsNameNoGoTypes(t *testing.T) {
	s := NewServer()
	cases := []struct {
		method, params, want string
	}{
		{"prompts/get", `{"name":5}`, "invalid params: name: expected a string, got a number"},
		{"resources/read", `{"uri":[1]}`, "invalid params: uri: expected a string, got an array"},
		{"tools/call", `{"name":true}`, "invalid params: name: expected a string, got a boolean"},
		{"tools/list", `{"cursor":{}}`, "invalid list params: cursor: expected a string, got an object"},
	}
	for _, tc := range cases {
		resp := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: tc.method, Params: json.RawMessage(tc.params),
		})
		if resp.Error == nil {
			t.Errorf("%s %s: no error", tc.method, tc.params)
			continue
		}
		if resp.Error.Message != tc.want {
			t.Errorf("%s %s: message %q, want %q", tc.method, tc.params, resp.Error.Message, tc.want)
		}
		if m := goInternals.FindString(resp.Error.Message); m != "" {
			t.Errorf("%s: message %q carries Go internals %q", tc.method, resp.Error.Message, m)
		}
	}
}
