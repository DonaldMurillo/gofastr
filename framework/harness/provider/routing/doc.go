// Package routing will implement RoutingProvider: a Provider that
// composes {router, executors[]} so a single turn can use a cheap
// model for routing and an expensive model for execution.
//
// Lands in v0.3. Critically, this is a Provider composition — NOT
// request middleware. Routing decisions stay inside the composition;
// cache-control, thinking-block provider-binding, token counting, and
// CostIncremented attribution all remain per-underlying-provider.
//
// Until then this package is intentionally empty; the architecture
// doc commits to the surface at § Future extension shapes →
// Multi-model per turn.
package routing
