// Package agent is Kiln's transport-agnostic LLM driver.
//
// Provider abstracts an LLM (Claude, OpenAI, local). Loop runs a
// tool-use conversation against a Provider, dispatching each tool call
// through kiln/protocol.Tools and journaling user / assistant /
// tool_call / tool_result entries as they happen.
//
// Sub-packages:
//
//	kiln/agent/mcp — wraps protocol.Tools as an MCP server so Claude
//	                  Code, Cursor, and other MCP clients can drive Kiln.
//	kiln/agent/acp — exposes Kiln over the Agent Client Protocol so
//	                  attached harnesses (Codex, Copilot, Pi) can drive.
//
// All three transports share the same protocol.Tools surface; tests
// cover each transport against a fake Provider so no LLM key is needed.
package agent
