// Package local is a local-first state API for GoFastr apps: declared
// in Go, persisted in the browser, with a documented contract.
//
// A Store is declared once per app with named collections. Each
// Collection[T] is a Go type with a JSON round-trip, an optional key
// field, a size cap per record and per collection, and a schema
// version with migrations the browser runs once. The browser API is
// generated from that declaration and served by the runtime as three
// modules beside this file: "local-store" (the store and its caps),
// "local-bridge" (the seed, the mirror cookie, the upload and the
// download; Requires the first) and "local-migrate" (the version
// steps, loaded at idle). All three keep every record in the kernel's
// `local` primitive
// (core-ui/runtime/src/local.js: IndexedDB, with a tiny-value
// localStorage fallback). No dependency was added: both engines are
// browser APIs.
//
// The bridges to Go screens are explicit and bounded:
//
//   - Seed: SeedSignal fills a core-ui/store slice from a record after
//     hydration and writes the slice back on change. The server renders
//     the slice's default; the runtime patches in place. IndexedDB is
//     asynchronous, so a persisted value cannot appear at FIRST PAINT.
//   - Mirror: a collection declared Mirror keeps each (tiny) record in
//     a cookie under gofastr.local.<app>.<collection>.<key>, which a
//     Go render reads through Get/List at first paint. The cookie is a
//     client hint: the browser wrote it, so it is validated (declared
//     collection, size cap, JSON shape into T) and never trusted.
//   - Upload: Send declares which collections or keys accompany an RPC
//     trigger's request as the reserved field __local, and Upload.Wrap
//     reads them on the server into the request context for
//     Get and List. Nothing undeclared is ever uploaded; an
//     undeclared collection in the field is refused.
//   - Download: Put, Delete and Clear on an RPC response write records
//     into the browser through the X-Gofastr-Local header, so a
//     handler can push state without SSE.
//
// Sync is opt-in and explicit through those four; there is no
// background reconciliation, no queue of pending mutations and no
// conflict resolution. This is local-first STATE, not offline-first,
// which remains a stated non-goal (ui-capability-map.md).
//
// The contract is best-effort, the primitive's own: private mode, a
// blocked origin, a full quota and a hand-cleared store are all
// normal. Every browser call settles, none throws, and a refusal says
// why. Never keep something here whose loss is a bug, and never keep
// a secret or a session token here: the store is readable by any
// script on the origin, and a mirrored record travels on every
// request as a cookie. See framework/docs/content/local-state.md.
package local

import (
	"fmt"
	"regexp"
	"sync"
)

// Names are one URL-safe token each so they can sit in a storage key,
// a cookie name, an attribute value and a JSON key without escaping:
// the same alphabet registry.RegisterBehavior accepts for a module
// name. An app id is capped shorter because it prefixes every key.
var (
	reAppID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	reName  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
)

// KeyMaxLen bounds a record key. Keys are component-encoded into the
// storage key and a cookie name, so any string is representable; the
// cap keeps a key from being the payload.
const KeyMaxLen = 256

// validRecordKey reports whether key may name a record: non-empty, at
// most KeyMaxLen bytes, and not a name the browser's object model
// reserves. Mirrors validKey in local-store.js, which counts the same
// UTF-8 bytes. Unexported: Collection.Key panics with the reason, Put
// and Delete return an error that names the key, and the browser
// refuses with reason 'key'; nothing a caller does needs to ask the
// question separately.
func validRecordKey(key string) bool {
	if key == "" || len(key) > KeyMaxLen {
		return false
	}
	switch key {
	case "__proto__", "constructor", "prototype":
		return false
	}
	return true
}

// Store is one app's declaration: an id and its collections. Declare
// it once, at package level or in main, and define every collection
// before the first render: the browser manifest is fixed when it is
// first served.
type Store struct {
	app string

	mu     sync.Mutex
	colls  map[string]*collectionDef
	frozen bool
}

var (
	appsMu sync.Mutex
	apps   = map[string]*Store{}
)

// New declares the store for app, an id matching ^[a-z][a-z0-9-]{0,31}$
// that prefixes every key this app keeps in the browser
// (local.<app>.<collection>:<key> under the primitive's gofastr.state.
// namespace) and every cookie it mirrors. Declaring the same app id
// twice in one process panics, like registering a screen twice: two
// stores sharing a prefix would share records without sharing a
// schema.
func New(app string) *Store {
	if !reAppID.MatchString(app) {
		panic(fmt.Sprintf("local: app id %q must match ^[a-z][a-z0-9-]{0,31}$", app))
	}
	appsMu.Lock()
	defer appsMu.Unlock()
	if _, dup := apps[app]; dup {
		panic(fmt.Sprintf("local: store %q is already declared — one Store per app id", app))
	}
	s := &Store{app: app, colls: map[string]*collectionDef{}}
	apps[app] = s
	return s
}

// freeze marks the declaration served; a Define after this point
// panics, because the browser already holds the manifest.
func (s *Store) freeze() {
	s.mu.Lock()
	s.frozen = true
	s.mu.Unlock()
}

// defOf returns a declared collection, or nil.
func (s *Store) defOf(name string) *collectionDef {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.colls[name]
}

// resetForTest forgets every declared store so a test file can declare
// the same app id in more than one test. Never called outside tests.
func resetForTest() {
	appsMu.Lock()
	apps = map[string]*Store{}
	appsMu.Unlock()
	unwrappedWarned.Clear()
}
