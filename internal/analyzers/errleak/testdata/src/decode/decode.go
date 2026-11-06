// Package decode reconstructs the 1a4d9e2a shapes: encoding/json's own
// text, naming the server's Go types, sent to the client on a 4xx.
package decode

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"jsonhelp"
)

type Error struct {
	Status int
	Detail any
}

func (e *Error) Error() string { return fmt.Sprint(e.Detail) }

func Fail(status int, reason string, params map[string]any) *Error {
	return &Error{Status: status, Detail: params}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {}

// jsonErrorDetail is handler.JSONErrorDetail: a phrase in JSON's terms.
func jsonErrorDetail(err error) error {
	if err == nil {
		return nil
	}
	return errors.New("the value could not be decoded")
}

// unmarshalStrictPreFix is handler.UnmarshalStrict before 1a4d9e2a.
func unmarshalStrictPreFix(data []byte, dst any) error {
	if err := json.Unmarshal(data, dst); err != nil {
		return Fail(400, "invalid_json", map[string]any{"detail": err.Error()}) // want `sends encoding/json's error text to the client`
	}
	return nil
}

// unmarshalStrict is the fix.
func unmarshalStrict(data []byte, dst any) error {
	if err := json.Unmarshal(data, dst); err != nil {
		return Fail(400, "invalid_json", map[string]any{"detail": jsonErrorDetail(err)})
	}
	return nil
}

func decoderPreFix(w http.ResponseWriter, r *http.Request) {
	var v struct{ IDs []string }
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest) // want `sends encoding/json's error text to the client`
	}
}

// decodeBatch is crud_batch.go's shape: the decode error wrapped with
// %w and returned, so its callers carry it.
func decodeBatch(body []byte, v any) error { // want decodeBatch:`rawJSONErr`
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func batchHandler(w http.ResponseWriter, body []byte) {
	var v []int
	if err := decodeBatch(body, &v); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error()) // want `sends encoding/json's error text to the client`
	}
}

// A helper in another package carries the origin through its fact.
func crossPackage(w http.ResponseWriter, r *http.Request) {
	var v map[string]any
	if err := jsonhelp.ReadBody(r.Body, &v); err != nil {
		writeJSONError(w, 422, fmt.Sprintf("invalid body: %v", err)) // want `sends encoding/json's error text to the client`
	}
}

// fromJSONLit: a decoder bound to a func literal is the function's own
// code, and its error is still encoding/json's own.
func fromJSONLit(w http.ResponseWriter, body []byte) {
	dec := func(b []byte, v any) error { return json.Unmarshal(b, v) }
	var v struct{ IDs []string }
	if err := dec(body, &v); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error()) // want `sends encoding/json's error text to the client`
	}
}

// ---- JSON-RPC ------------------------------------------------------------

const (
	ErrParse         = -32700
	ErrInvalidParams = -32602
)

type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string { return e.Message }

func newErrorResponse(id int, code int, message string) {}

// handlePromptsGetPreFix is core/mcp prompts.go before 1a4d9e2a.
func handlePromptsGetPreFix(id int, raw json.RawMessage) {
	var params struct{ Name string }
	if err := json.Unmarshal(raw, &params); err != nil {
		newErrorResponse(id, ErrInvalidParams, "invalid params: "+err.Error()) // want `sends encoding/json's error text to the client`
	}
}

func handlePromptsGet(id int, raw json.RawMessage) {
	var params struct{ Name string }
	if err := json.Unmarshal(raw, &params); err != nil {
		newErrorResponse(id, ErrInvalidParams, "invalid params: "+jsonErrorDetail(err).Error())
	}
}

// The ACP envelope shape: a Code/Message literal.
func envelopePreFix(line []byte) *RPCError {
	var frame struct{ Method string }
	if err := json.Unmarshal(line, &frame); err != nil {
		return &RPCError{Code: ErrParse, Message: err.Error()} // want `sends encoding/json's error text to the client`
	}
	return nil
}

// ---- quiet -----------------------------------------------------------------

// A 4xx echoing a parser written for clients stays the useful answer.
func ownParser(w http.ResponseWriter, path string) {
	if _, err := os.ReadFile(path); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

// Logged, not sent.
func logged(w http.ResponseWriter, body []byte) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		log.Printf("decode: %v", err)
		http.Error(w, "invalid body", http.StatusBadRequest)
	}
}

// The variable was reassigned from another source before the sink.
func reassigned(w http.ResponseWriter, body []byte, path string) {
	var v any
	err := json.Unmarshal(body, &v)
	_ = err
	_, err = os.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

// A 5xx is the internal-error arm's; one finding, not two.
func fiveHundred(w http.ResponseWriter, body []byte) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError) // want `sends an internal error's text on a 5xx response`
	}
}

// Near miss: printed to the operator, no status, no RPC code.
func printed(body []byte) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		fmt.Println("decode failed:", err.Error())
	}
}
