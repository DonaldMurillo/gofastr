// Package protocol is Kiln's canonical agent tool surface.
//
// Each public method on Tools is one tool. Methods take typed Args, build
// a journal.Entry, push it through live.Live's Apply funnel, and return a
// structured Result with OK / Error / Kind / Hint. The same surface is
// wrapped by transports: the native agent tool-use loop (agent/), the MCP
// server (mcp/), and the ACP adapter (acp/), without re-deriving behavior.
// Tests drive Tools directly without any LLM in the loop.
package protocol
