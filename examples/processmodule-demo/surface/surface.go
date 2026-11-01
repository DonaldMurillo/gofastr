// Package surface holds the parts of the processmodule-demo's surface that
// the module and its host both need byte-for-byte: the MCP tool the module
// lists, whose digest the host pins in the descriptor. The module
// (examples/processmodule-demo) serves it and the host
// (examples/processmodule-demo/host) pins framework.ModuleToolDigest of
// it, so the two can never drift. It depends only on core/moduleproto,
// the same as the module itself.
package surface

import (
	"encoding/json"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// PingTool is the one MCP tool the demo exposes. Its canonical digest is
// what the host byte-compares against the descriptor at handshake
// (framework.ModuleToolDigest).
func PingTool() moduleproto.Tool {
	return moduleproto.Tool{
		ID:          "ping",
		Name:        "module.demo.ping",
		Description: "Reverse-queries the granted host entity and reports the row count.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
	}
}
