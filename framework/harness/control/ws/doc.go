// Package ws will implement the WebSocket transport for the control
// plane. Lands in v0.2 alongside the mcpserver stdio transport.
//
// WebSocket framing carries the canonical event envelope verbatim
// (see § Protocol versioning → Canonical event envelope); reconnect
// resumes from `lastEventId` query param.
//
// Until then this package is intentionally empty; the architecture
// doc commits to the surface at § WebSocket surface.
package ws
