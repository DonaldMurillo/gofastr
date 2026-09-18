package local

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed local-store.js
var localStoreJS string

//go:embed local-bridge.js
var localBridgeJS string

//go:embed local-migrate.js
var localMigrateJS string

// BehaviorName is the runtime module that binds this package's
// data-local-* markers and exposes window.__gofastr.localStore(app).
// The host serves it at /__gofastr/runtime/local-store.js and the
// kernel loads it when a marker is on the page, or an RPC trigger
// names it in data-fui-rpc-with, or a script asks for it with
// __gofastr.loadModule('local-store').
const BehaviorName = "local-store"

// BridgeName is the second module: every way the store reaches a Go
// handler, the seed, the mirror cookie, the upload and the download.
// It Requires BehaviorName and binds the data-local-seed marker (a
// record's value joined to a core-ui/store slice); an RPC trigger
// rendered by Upload.Attrs names it in data-fui-rpc-with so rpc.js has
// it loaded before the fetch, and local-store asks for it by name when
// a declaration mirrors a collection or a logout is pending. A page
// whose store keeps its records to itself never loads it.
const BridgeName = "local-bridge"

// MigrateName is the third module: schema evolution, the steps that
// rewrite a collection from one version to the next. A different job
// from keeping records: it runs once per browser per version, over
// every record of a collection at a time, and a failure leaves a
// collection on two schemas. So it is its own file, registered
// LoadIdle because it is never on the critical path, and asked for by
// name at the moment a rewrite is due. A collection that declares no
// version step never runs a line of it.
const MigrateName = "local-migrate"

// The marker: every element this package renders carries
// data-local-store="<app>", and the seed or send attribute beside it
// says what to do. Spelled as a literal because the runtime's
// hard-rule-5 gate reads every registry.Markers call in the tree.
//
// Requires("local"): the storage primitive is registered before this
// module evaluates, so it can call window.__gofastr.local without a
// guard for a primitive still in flight.
var _ = registry.RegisterBehavior(BehaviorName, localStoreJS,
	registry.Markers(`[data-local-store]`),
	registry.Requires("local"))

var _ = registry.RegisterBehavior(BridgeName, localBridgeJS,
	registry.Markers(`[data-local-seed]`),
	registry.Requires(BehaviorName))

var _ = registry.RegisterBehavior(MigrateName, localMigrateJS,
	registry.Markers(`[data-local-store]`),
	registry.Requires(BehaviorName),
	registry.LoadIdle())
