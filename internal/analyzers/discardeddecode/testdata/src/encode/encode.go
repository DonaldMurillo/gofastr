// Package encode pins the encode arm (2026-11): a JSON-RPC envelope
// encoded with its error dropped, when the envelope can fail to encode.
// Reduced from core/mcp/transport.go before 3516c4d9b.
package encode

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Response is core/mcp's envelope: its Result is any.
type Response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

type rawResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  string          `json:"result"`
}

// scoreResponse fails on a NaN score.
type scoreResponse struct {
	JSONRPC string  `json:"jsonrpc"`
	Score   float64 `json:"score"`
}

// ackResponse holds nothing that can fail to encode.
type ackResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	OK      bool   `json:"ok"`
	Note    []byte `json:"note"`
}

// hiddenResponse keeps its failing fields out of the encoding.
type hiddenResponse struct {
	JSONRPC string  `json:"jsonrpc"`
	Debug   any     `json:"-"`
	ratio   float64 // unexported
}

// Money marshals itself: its float is formatted by its own contract.
type Money struct{ Amount float64 }

func (m Money) MarshalJSON() ([]byte, error) { return []byte(`"0.00"`), nil }

// stampedResponse holds types with their own MarshalJSON.
type stampedResponse struct {
	Price   Money     `json:"price"`
	JSONRPC string    `json:"jsonrpc"`
	At      time.Time `json:"at"`
}

type handleFunc func(Response) Response

// serveHTTP is ServeHTTP before 3516c4d9b: an empty 200 on a NaN.
func serveHTTP(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) // want `Encode error discarded on a Response, which can fail to encode`
}

// serveStdio is ServeStdio before 3516c4d9b: no line at all.
func serveStdio(out io.Writer, resps []Response) {
	encoder := json.NewEncoder(out)
	for _, resp := range resps {
		encoder.Encode(resp) // want `Encode error discarded on a Response`
	}
}

// ssePost is ssePostHandler before 3516c4d9b: an empty data event.
func ssePost(w io.Writer, resp *Response) {
	data, _ := json.Marshal(resp) // want `json.Marshal error discarded on a \*Response`
	w.Write(data)
}

func blanked(w io.Writer, r rawResponse, s scoreResponse) {
	_ = json.NewEncoder(w).Encode(r)        // want `Encode error discarded on a rawResponse`
	b, _ := json.MarshalIndent(s, "", "  ") // want `json.MarshalIndent error discarded on a scoreResponse`
	w.Write(b)
}

// literal sets a field that can fail.
func literal(w io.Writer, v any) {
	json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", Result: v}) // want `Encode error discarded on a Response`
}

// ---- quiet ----

// encodeResponse is the fix spelling.
func encodeResponse(resp Response) []byte {
	b, err := json.Marshal(resp)
	if err != nil {
		slog.Error("encode failed", "err", err)
		return []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"internal error"}}`)
	}
	return b
}

func checkedEncode(w io.Writer, resp Response) error {
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		return err
	}
	return nil
}

// tooLong is a struct literal that sets only fields that cannot fail:
// the omitted Result and ID are nil and encode as null.
func tooLong(w io.Writer) {
	json.NewEncoder(w).Encode(Response{
		JSONRPC: "2.0",
		Error:   &RPCError{Code: -32602, Message: "request line too long"},
	})
}

// hiddenLiteral sets a field the encoder never reaches.
func hiddenLiteral(w io.Writer, v any) {
	json.NewEncoder(w).Encode(hiddenResponse{JSONRPC: "2.0", Debug: v})
}

func safeShapes(w io.Writer, a ackResponse, h hiddenResponse, s stampedResponse) {
	json.NewEncoder(w).Encode(a)
	_ = json.NewEncoder(w).Encode(h)
	b, _ := json.Marshal(s)
	w.Write(b)
}

// notEnvelope: a map or a page of rows can fail too, but the arm is
// scoped to protocol envelopes (see the package doc for the counts).
func notEnvelope(w io.Writer, rows []map[string]any, f float64) {
	json.NewEncoder(w).Encode(rows)
	b, _ := json.Marshal(map[string]any{"ratio": f})
	w.Write(b)
}

// otherEncoder is not encoding/json.
type otherEncoder struct{}

func (otherEncoder) Encode(v any) error { return nil }

func nearMiss(resp Response) {
	otherEncoder{}.Encode(resp)
	var buf bytes.Buffer
	_ = buf
}

var _ handleFunc
