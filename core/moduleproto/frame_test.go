package moduleproto

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestFrameRejectsBadJSONRPC verifies the jsonrpc:"2.0" requirement (design
// §4.3: "required; rejected otherwise").
func TestFrameRejectsBadJSONRPC(t *testing.T) {
	cases := map[string]string{
		"missing":  `{"id":1,"method":"x"}`,
		"wrong":    `{"jsonrpc":"1.0","id":1,"method":"x"}`,
		"empty":    `{"jsonrpc":"","id":1,"method":"x"}`,
		"nonstart": `{"id":1,"method":"x","jsonrpc":null}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var f Frame
			err := json.Unmarshal([]byte(raw), &f)
			if err == nil {
				t.Fatalf("expected error for %s, got nil; frame=%+v", name, f)
			}
			if !errors.Is(err, ErrInvalidFrame) {
				t.Fatalf("error not ErrInvalidFrame: %v", err)
			}
		})
	}
}

// TestFrameRejectsIDZero pins the load-bearing property from §4.3: id:0 is
// impossible. mcpclient treated id:0 as the notification sentinel; moduleproto
// rejects it on decode so a misrouted frame cannot be silently dropped.
func TestFrameRejectsIDZero(t *testing.T) {
	var f Frame
	err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":0,"method":"x"}`), &f)
	if err == nil {
		t.Fatalf("expected error for id:0, got frame=%+v", f)
	}
	if !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("error not ErrInvalidFrame: %v", err)
	}
	if !strings.Contains(err.Error(), "id must be > 0") {
		t.Fatalf("error message wrong: %v", err)
	}
}

// TestFrameRequestMarshalOmitsNilID verifies the wire shape: a request marshals
// with id, a notification marshals WITHOUT id (no zero-sentinel).
func TestFrameRequestMarshalOmitsNilID(t *testing.T) {
	req := NewRequest(7, "module.health", nil)
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"id":7`) {
		t.Fatalf("request missing id:7: %s", b)
	}
	if !strings.Contains(string(b), `"method":"module.health"`) {
		t.Fatalf("request missing method: %s", b)
	}

	// Notification: id key must be absent.
	notif := NewNotification("module.cancel", json.RawMessage(`{"request_id":"3"}`))
	b2, err := json.Marshal(notif)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b2), `"id"`) {
		t.Fatalf("notification must not marshal id: %s", b2)
	}
}

// TestFrameResponseShape verifies a response frame carries id + result/error
// but no method.
func TestFrameResponseShape(t *testing.T) {
	success := NewSuccessResponse(3, json.RawMessage(`{"ok":true}`))
	b, err := json.Marshal(success)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"method"`) {
		t.Fatalf("response must not carry method: %s", b)
	}
	if !strings.Contains(string(b), `"id":3`) {
		t.Fatalf("response missing id:3: %s", b)
	}
	if !strings.Contains(string(b), `"result":{"ok":true}`) {
		t.Fatalf("response missing result: %s", b)
	}

	errResp := NewErrorResponse(5, CodeInternalError, "boom", nil)
	b3, err := json.Marshal(errResp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b3), `"error":{"code":-32603,"message":"boom"}`) {
		t.Fatalf("error response shape wrong: %s", b3)
	}
}

// TestFrameRejectsResponseNoResultOrError: a response with neither is invalid.
func TestFrameRejectsResponseNoResultOrError(t *testing.T) {
	var f Frame
	err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":1}`), &f)
	if err == nil {
		t.Fatalf("expected error for response without result/error")
	}
}

// TestFrameRejectsMethodWithResult: a request with result is malformed.
func TestFrameRejectsMethodWithResult(t *testing.T) {
	var f Frame
	err := json.Unmarshal(
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"x","result":{"a":1}}`), &f)
	if err == nil {
		t.Fatalf("expected error for request with result")
	}
}

// TestFrameRejectsResponseMissingID: a response without id is malformed.
func TestFrameRejectsResponseMissingID(t *testing.T) {
	var f Frame
	err := json.Unmarshal(
		[]byte(`{"jsonrpc":"2.0","result":{"a":1}}`), &f)
	if err == nil {
		t.Fatalf("expected error for response without id")
	}
}

// TestFrameDiscriminationMethods verifies the IsRequest/IsNotification/IsResponse
// helpers used by the read loop.
func TestFrameDiscriminationMethods(t *testing.T) {
	id := uint64(1)
	req := &Frame{JSONRPC: "2.0", ID: &id, Method: "x"}
	if !req.IsRequest() || req.IsNotification() || req.IsResponse() {
		t.Fatalf("request discrimination wrong: %+v", req)
	}
	notif := &Frame{JSONRPC: "2.0", Method: "x"}
	if !notif.IsNotification() || notif.IsRequest() || notif.IsResponse() {
		t.Fatalf("notification discrimination wrong: %+v", notif)
	}
	resp := &Frame{JSONRPC: "2.0", ID: &id, Result: json.RawMessage(`{}`)}
	if !resp.IsResponse() || resp.IsRequest() || resp.IsNotification() {
		t.Fatalf("response discrimination wrong: %+v", resp)
	}
}

// TestFrameLargeIDRoundTrips verifies ids above uint32 work — the protocol is
// uint64 and IDs accumulate over a long-lived connection.
func TestFrameLargeIDRoundTrips(t *testing.T) {
	const big = uint64(1)<<40 + 7
	f := NewRequest(big, "x", nil)
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var got Frame
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.IDValue() != big {
		t.Fatalf("id round-trip mismatch: want %d got %d", big, got.IDValue())
	}
}
