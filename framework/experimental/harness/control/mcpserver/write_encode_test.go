package mcpserver

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
)

// A result JSON cannot encode is answered with an internal error under the
// request's id: write dropped the marshal error and sent an empty line, so
// the client waited on a reply that never came.
func TestWriteAnswersUnencodableResult(t *testing.T) {
	var out bytes.Buffer
	s := New(nil, nil).WithIO(nil, &out)
	s.write(rpcResponse{ID: json.RawMessage(`7`), Result: map[string]any{"score": math.NaN()}})
	var got struct {
		ID    json.RawMessage `json:"id"`
		Error *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &got); err != nil {
		t.Fatalf("reply is not JSON: %q (%v)", out.String(), err)
	}
	if string(got.ID) != "7" || got.Error == nil || got.Error.Code != -32603 {
		t.Fatalf("reply = %q, want an internal error under id 7", out.String())
	}
}
