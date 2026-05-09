// Package protocol is Kiln's canonical agent tool surface.
//
// Each public method on Tools is one tool. Methods take typed Args, build
// a journal.Entry, push it through live.Live's Apply funnel, and return a
// structured Result with OK / Error / Kind / Hint. The same surface is
// later wrapped by transports — native Claude tool-use loop (Phase 7a),
// MCP server (Phase 7b), ACP adapter (Phase 7c) — without re-deriving
// behavior. Tests drive Tools directly without any LLM in the loop.
package protocol
