// Package acp implements the server (agent) side of the Agent Client
// Protocol v1 (agentclientprotocol.com): a session-based, stdio
// JSON-RPC 2.0 protocol that lets editors and agent harnesses drive
// an agent. This package is transport- and domain-agnostic; the
// embedder supplies the conversational brain through the Agent and
// Session interfaces and the server owns everything on the wire:
// initialize capability negotiation, session lifecycle, streamed
// session/update notifications, and server-to-client
// session/request_permission calls.
//
// Deliberately unimplemented, and declared so at initialize: prompt
// content beyond text and resource_link (promptCapabilities all
// false), MCP server connections (mcpCapabilities all false, plus
// session/new rejects a non-empty mcpServers list instead of silently
// ignoring it), client filesystem access (fs/read_text_file,
// fs/write_text_file) and terminals (terminal/*) are client-side
// methods this agent never calls, and no session lifecycle capability
// beyond loadSession is advertised. A client learns every one of
// those absences from the initialize response.
//
// kiln/acp adapts Kiln's tool surface onto this package; see
// framework/docs/content/acp.md for the embedding guide.
package acp
