// Package island provides a server-driven island architecture using SSE.
//
// Islands are server-rendered UI widgets that can receive live HTML updates
// via Server-Sent Events. Each island wraps a component.Component and is
// tracked by a Manager which handles registration, update pushing, and
// SSE streaming to connected clients.
//
// Basic usage:
//
//	mgr := island.NewManager()
//	isl := island.NewIsland("counter-1", myComponent)
//	isl.SessionID = "session-abc"
//	mgr.Register(isl)
//
//	// Push an update (e.g. from a handler or goroutine)
//	mgr.Push("counter-1")
//
//	// Serve SSE endpoint for client EventSource connections
//	http.HandleFunc("/islands/sse", mgr.ServeSSE)
//
// # Cross-replica fanout (real-time lane)
//
// By default delivery is per-process: an update pushed on replica A reaches
// only the browsers connected to A. Manager.SetFanout attaches a
// core/fanout.Fanout so Push/PushUpdate also broadcast to other replicas and
// updates from other replicas are re-delivered to the local session stream.
// This fixes delivery-WHERE-connected — a session whose SSE connection lives
// on another replica still sees updates.
//
// It does NOT move island OBJECTS or signal state across replicas. An RPC
// that lands on a replica without the island object cannot re-render it, so
// sticky sessions remain the recommendation for stateful widget apps.
// Delivery is lossy best-effort (this is the real-time lane; durable
// delivery is the transactional outbox's job).
package island
