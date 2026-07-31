// Package render bridges the Kiln world IR to a runnable framework.App.
//
// Apply walks a *world.World and registers every surface — entities,
// pages, custom routes, hooks, seeds, middleware — onto an existing
// *framework.App. Hooks and routes carry declarative actions that
// kiln/effect evaluates; entity endpoints carry actions too but are not
// mounted in the live build-mode app (applyEntities screams per dropped
// endpoint; they graduate to owned-Go stubs via freeze).
//
// The package is the bridge: callers compose a framework.App as usual
// and pass it through Apply. No reflection; conversions are explicit.
package render
