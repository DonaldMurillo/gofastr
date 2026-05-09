// Package acp adapts Kiln to the Agent Client Protocol so external
// agent harnesses (Codex, Copilot, Pi, Claude Code) can drive Kiln as
// an attached agent server. ACP is a stdio-transported JSON-RPC
// protocol modeled after LSP. Kiln implements the server side: the
// harness connects, sends prompts, receives streamed messages and tool
// calls, and tool calls are dispatched to kiln/protocol.Tools.
//
// This implementation covers the core methods needed for end-to-end
// chat: initialize, prompt, tools/list, tools/call. Capabilities the
// spec defines but Kiln doesn't yet need (workspace edits, file IO)
// return the standard "method not found" error.
package acp
