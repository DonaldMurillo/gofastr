// Package render bridges the Kiln world IR to a runnable framework.App.
//
// Apply walks a *world.World and registers every surface — entities,
// pages, custom routes, hooks, seeds, middleware — onto an existing
// *framework.App. Phase 1 covers entities, pages, seeds, and middleware;
// hooks and custom routes carry declarative actions whose evaluator
// lands in Phase 3 and are stubbed with logged warnings until then.
//
// The package is the bridge: callers compose a framework.App as usual
// and pass it through Apply. No reflection; conversions are explicit.
package render
