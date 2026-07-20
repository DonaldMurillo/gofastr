// Package island is the runtime-side manager for server-driven SSE updates.
//
// Islands themselves are server-rendered HTML fragments produced elsewhere
// (e.g. via core-ui/component and the blueprint generator's
// island.NewIsland(...).Render()). This package owns the LIVE side: the
// per-session update streams that carry fresh island HTML to the browser over
// Server-Sent Events, plus the cross-replica fanout and presence roster that
// make those streams work across a multi-replica deployment.
//
// Basic usage:
//
//	mgr := island.NewManager()
//
//	// Each SSE connection subscribes and gets its own channel; cancel
//	// removes exactly that subscription.
//	ch, cancel := mgr.Subscribe("session-abc")
//	defer cancel()
//
//	// Push a fresh island HTML fragment to every subscriber of the
//	// session — every tab sharing the cookie receives it.
//	mgr.PushUpdate(island.IslandUpdate{IslandID: "counter-1", HTML: html}, "session-abc")
//
//	// Stream updates to the browser via SSE.
//	http.HandleFunc("/islands/sse", mgr.ServeSSE)
//
// # Cross-replica fanout (real-time lane)
//
// By default delivery is per-process: an update pushed on replica A reaches
// only the browsers connected to A. Manager.SetFanout attaches a
// core/fanout.Fanout so PushUpdate also broadcasts to other replicas and
// updates from other replicas are re-delivered to the local session stream.
// This fixes delivery-WHERE-connected — a session whose SSE connection lives
// on another replica still sees updates.
//
// The manager retains no island objects: callers render HTML from
// reconstructable state (screen definition + DB + request params) and
// PushUpdate transports it, so any replica can serve any RPC — no sticky
// routing. Delivery is lossy best-effort (this is the real-time lane;
// durable delivery is the transactional outbox's job).
package island
