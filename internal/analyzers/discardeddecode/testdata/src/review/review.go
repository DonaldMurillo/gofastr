package review

import (
	"encoding/json"
	"io"
	"time"
)

type response struct {
	JSONRPC string `json:"jsonrpc"`
	Result  any    `json:"result"`
}

type pointerKey struct{}

func (*pointerKey) MarshalText() ([]byte, error) { return []byte("key"), nil }

type wrongKey struct{}

func (wrongKey) MarshalText() string { return "key" }

type wrongResultKey struct{}

func (wrongResultKey) MarshalText() ([]byte, bool) { return nil, false }

func interfaceKeys(output io.Writer) {
	json.NewEncoder(output).Encode(response{JSONRPC: "2.0", Result: map[any]string{"x": "y"}})
}

func pointerKeys(output io.Writer, keys map[pointerKey]string) {
	json.NewEncoder(output).Encode(response{JSONRPC: "2.0", Result: keys})                           // want `Encode error discarded on a response`
	json.NewEncoder(output).Encode(response{JSONRPC: "2.0", Result: map[pointerKey]string{{}: "y"}}) // want `Encode error discarded on a response`
}

func wrongMethods(output io.Writer, keys map[wrongKey]string, results map[wrongResultKey]string) {
	json.NewEncoder(output).Encode(response{JSONRPC: "2.0", Result: keys})    // want `Encode error discarded on a response`
	json.NewEncoder(output).Encode(response{JSONRPC: "2.0", Result: results}) // want `Encode error discarded on a response`
}

func emptyUnsupported(output io.Writer) {
	json.NewEncoder(output).Encode(response{JSONRPC: "2.0", Result: map[float64]string{}})
	json.NewEncoder(output).Encode(response{JSONRPC: "2.0", Result: map[any]string{}})
}

func supported(output io.Writer, pointers map[*pointerKey]string, stamps map[time.Time]string) {
	json.NewEncoder(output).Encode(response{JSONRPC: "2.0", Result: pointers})
	json.NewEncoder(output).Encode(response{JSONRPC: "2.0", Result: stamps})
	json.NewEncoder(output).Encode(response{JSONRPC: "2.0", Result: map[string]string{}})
	json.NewEncoder(output).Encode(response{JSONRPC: "2.0", Result: map[int]string{1: "y"}})
}
