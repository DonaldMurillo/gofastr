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
package island
