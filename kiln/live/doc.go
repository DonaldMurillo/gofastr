// Package live is the runtime that ties the Kiln components together
// during a session: it owns the current Session (world + chat + plans),
// the Journal that persists every edit, the framework.App that serves
// the live preview, and the SSE broadcaster that notifies the panel.
//
// Apply is the single funnel through which the world ever changes.
// Anything that wants to mutate state — the agent's tool surface, the
// chat panel's undo button, an external MCP client — converges on this
// one path so journal, app, and SSE stay consistent.
package live
