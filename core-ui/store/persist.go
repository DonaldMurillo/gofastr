package store

import (
	_ "embed"
	"fmt"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

// Browser-persisted slices.
//
// A signal is a UI projection, and the framework's answer to "where
// does truth live" is still the server. Some state has no server to
// live on: a draft, a filter set, a scratch list a signed-out visitor
// builds in one browser. Before this, an app reached for a hand-written
// document script and owned the DOM from there.
//
// Persist keeps such a value in the signal store and lets the browser
// remember it, under four promises and no more:
//
//   - BEST-EFFORT. Private mode, a blocked origin, a full quota and a
//     cleared store are all normal. A read that fails leaves the
//     server's value in place; a write that fails raises
//     gofastr:persist-overflow in the page and nothing else. Never
//     persist something whose loss is a bug.
//   - NAMESPACED. The runtime's `local` primitive stores every entry
//     under "gofastr.state." + the component-encoded key, and the key
//     is the slice name, which carries its Store's namespace: the
//     entry for Store("teambuilder").String("teams", …) is
//     gofastr.state.teambuilder.teams. An app cannot choose the raw
//     key: core-ui/check's storage-key lint refuses one.
//   - SIZE-BOUNDED. Every slice declares a cap and a value over it is
//     not written; the page hears gofastr:persist-overflow instead. A
//     signal is re-serialised on every change, so the cap is about
//     what a projection should cost, not about what the engine could
//     hold.
//   - NEVER TRUSTED AS MARKUP. The runtime marks a signal untrusted
//     when its value came from an input the page does not author (the
//     deep-link seed in widgets.js). Such a value is not persisted, and
//     a value restored from the browser is set untrusted, so BindHTML
//     renders it as text: persistence must not launder a flag that
//     lives only in memory into stored markup.
//   - INVISIBLE TO THE SERVER. Nothing is posted, no cookie is set, no
//     header is added. A Go render never sees the value. A screen that
//     must know a browser-held value at FIRST PAINT wants the cookie
//     mirror ui.Banner uses, not this.
//
// It does not make GoFastr offline-first (see the UI capability map's
// non-goals): there is no conflict resolution, no queue of pending
// mutations and no sync. It is a browser remembering a projection.
//
// The runtime half is two pieces, and the split is deliberate. The
// browser store itself is the kernel's `local` primitive
// (core-ui/runtime/src/local.js): IndexedDB with a tiny-value
// localStorage fallback, one namespace, get/set/remove/keys/subscribe/
// available, and no opinion about what a value means. persist.js is
// the thin module that joins it to the signal bus, registered as
// signal-persist with Requires("local") and loaded by the kernel when
// a persisted slice rendered a binding.
//
// Anything more opinionated than "a browser remembers a projection",
// such as schemas, migrations, an upload channel or conflict
// resolution, is a layer ABOVE core-ui and does not belong in this
// package.

// PersistDefaultMaxBytes is the cap Persist applies: 64 KiB of
// JSON-encoded value, generous for a preference set or a draft and
// small enough that a slice cannot quietly consume the origin's
// storage on its own.
const PersistDefaultMaxBytes = 64 << 10

// PersistMaxBytesLimit is the largest cap PersistMax accepts: 1 MiB.
// IndexedDB would allow far more, and that is the point: a signal is a
// UI projection the whole page re-serialises on every change, so a
// slice asking to round-trip megabytes through JSON on each keystroke
// is a design mistake, refused where it is written. Large, durable
// data wants the `local` primitive directly (or the
// server), not a signal.
const PersistMaxBytesLimit = 1 << 20

// persistAttr marks a rendered binding as browser-persisted and carries
// the slice's cap. It is the module's marker: the kernel loads
// signal-persist when it appears.
const persistAttr = "data-fui-signal-persist"

//go:embed persist.js
var persistJS string

// The behaviour registers the way a component's stylesheet does: the
// JavaScript is embedded beside the Go that renders the markup it
// binds, and the kernel loads it on the marker. The marker is spelled
// as a literal because the hard-rule-5 gate reads every
// registry.Markers call in the tree.
var _ = registry.RegisterBehavior("signal-persist", persistJS,
	registry.Markers("[data-fui-signal-persist]"),
	registry.Requires("local"))

// persistCfg is one slice's browser-persistence declaration.
type persistCfg struct{ maxBytes int }

// Persist marks the slice browser-persisted with the default cap
// (PersistDefaultMaxBytes). Read the package's "Browser-persisted
// slices" notes before reaching for it: the contract is best-effort,
// namespaced, size-bounded, and invisible to the server.
//
//	var Prefs = store.New("teambuilder")
//	var Teams = store.JSON[[]Team](Prefs, "teams", nil).Persist()
//
// Persist implies Global: the browser's value must survive a
// client-side navigation, and the app-global merge rule ("seed a
// global only the first time it is seen") is what keeps a partial
// render from clobbering it.
func (sl *Slice[T]) Persist() *Slice[T] { return sl.PersistMax(PersistDefaultMaxBytes) }

// PersistMax is Persist with an explicit cap in bytes of JSON-encoded
// value. A value over the cap is not written and the page hears
// gofastr:persist-overflow instead. maxBytes outside
// (0, PersistMaxBytesLimit] panics: a cap nobody chose is the bug this
// primitive exists to avoid.
func (sl *Slice[T]) PersistMax(maxBytes int) *Slice[T] {
	if sl.comp != nil {
		panic(fmt.Sprintf("store: slice %q is computed; a derived value is recomputed from its dependencies on every load, so persisting it would restore a stale answer; persist the dependencies", sl.name))
	}
	if maxBytes <= 0 || maxBytes > PersistMaxBytesLimit {
		panic(fmt.Sprintf("store: slice %q: persist cap %d is outside (0, %d]; a signal is re-serialised on every change, so a megabyte-scale value belongs in the local primitive directly", sl.name, maxBytes, PersistMaxBytesLimit))
	}
	sl.persist = &persistCfg{maxBytes: maxBytes}
	return sl.Global()
}

// Persisted reports whether the slice is browser-persisted.
func (sl *Slice[T]) Persisted() bool { return sl.persist != nil }

// PersistMaxBytes returns the slice's declared cap, or 0 when it is not
// persisted.
func (sl *Slice[T]) PersistMaxBytes() int {
	if sl.persist == nil {
		return 0
	}
	return sl.persist.maxBytes
}

// applyPersist stamps the persistence marker on a rendered binding. The
// runtime reads the cap from it and the slice name from the
// data-fui-signal beside it, so a binding is the whole declaration: a
// persisted slice the page never binds is never restored, exactly as a
// page-scoped slice the page never references is never seeded.
func (sl *Slice[T]) applyPersist(a map[string]string) {
	if sl.persist == nil {
		return
	}
	a[persistAttr] = strconv.Itoa(sl.persist.maxBytes)
}
