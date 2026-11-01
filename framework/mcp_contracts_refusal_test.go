package framework

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// toolCall is a tools/call request body for name with JSON arguments.
func toolCall(name, args string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args)
}

// A contract tool's refusal is the agent's to act on: it crosses
// tools/call with its message (unknown rule and the suggestion, no
// autofix, an unknown capability), never as "internal tool error".
func TestContractRefusalsReachTheAgent(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{Name: "mcp-refusals"}), WithMCP(), WithMCPIntrospection())
	app.mcpIntrospectionDevImplied = true
	app, cleanup := startApp(t, app)
	t.Cleanup(cleanup)

	manual := ""
	for _, r := range contracts.AllRules() {
		if !r.Autofix {
			manual = r.ID
			break
		}
	}
	cases := []struct{ tool, args, want string }{
		{"contracts_explain", `{"rule":"GOFASTR1oo5"}`, "did you mean"},
		{"contracts_explain", `{"rule":" "}`, "rule is required"},
		{"contracts_list", `{"capability":"nope"}`, "nope"},
		{"contracts_fix", `{"rule":"GOFASTR9999"}`, "GOFASTR9999"},
		{"contracts_verify", `{"capability":"nope"}`, "unknown capability"},
	}
	if manual != "" {
		cases = append(cases, struct{ tool, args, want string }{"contracts_fix", fmt.Sprintf(`{"rule":%q}`, manual), "no autofix"})
	}
	for _, tc := range cases {
		out, isErr := mcpCall(t, app, toolCall(tc.tool, tc.args))
		if !isErr || !strings.Contains(out, tc.want) {
			t.Errorf("%s %s: got %q (error=%v); want a refusal naming %q", tc.tool, tc.args, out, isErr, tc.want)
		}
	}
}

// A contracts_fix that stopped part way has already rewritten files.
// The admission naming them reaches the agent over tools/call.
func TestContractsFixPartialWriteOverMCP(t *testing.T) {
	dir := writeTreeAndChdir(t, map[string]string{
		"a.go": "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core/router\"\n\n" +
			"func wireA(r *router.Router) {\n\tr.Handle(\"post\", \"/a\", nil)\n}\n",
		"b.go": "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core/router\"\n\n" +
			"func wireB(r *router.Router) {\n\tr.Handle(\"put\", \"/b\", nil)\n}\n",
	})
	if err := os.Chmod(filepath.Join(dir, "b.go"), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "b.go"), 0o644) })
	app := NewApp(WithConfig(AppConfig{Name: "mcp-partial"}), WithMCP(), WithMCPIntrospection())
	app.mcpIntrospectionDevImplied = true
	app, cleanup := startApp(t, app)
	t.Cleanup(cleanup)

	out, isErr := mcpCall(t, app, toolCall("contracts_fix", `{"rule":"GOFASTR1005"}`))
	if !isErr || !strings.Contains(out, "a.go") || !strings.Contains(out, "already written") {
		t.Fatalf("partial write answered %q (error=%v); want the admission naming a.go", out, isErr)
	}
}
