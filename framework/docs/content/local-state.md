# Local state (`framework/local`)

`framework/local` is local-first state for a GoFastr app: declared in Go,
persisted in the browser, with a documented contract. A `local.Store` is
declared once per app with named collections; each collection is a Go
record type with a JSON round-trip, an optional key field, a size cap per
record and per collection, and a schema version with migrations the
browser runs once. The browser API is generated from that declaration and
served by the runtime as three modules on top of the kernel's `local`
storage primitive ([Runtime contract](runtime-contract.md)), IndexedDB
with a tiny-value `localStorage` fallback: `local-store` (requires
`local`, marker `[data-local-store]`) is the store, the caps and the
collection API; `local-bridge` (requires `local-store`, marker
`[data-local-seed]`) is every way the store reaches a Go handler, the
seed, the mirror cookie, the upload and the download, and a page whose
store keeps its records to itself never loads it; `local-migrate`
(requires `local-store`, `LoadIdle`) is the version steps, asked for by
name when a rewrite is due.

It is **local-first state, not offline-first**: no queue of pending
mutations, no conflict resolution, no background reconciliation
([UI capability map](ui-capability-map.md)). Every bridge to the server
is opt-in and explicit, and the server is still where truth lives.

The motivating shape is a server-rendered app whose user owns a large,
account-less dataset in the browser (a team builder's teams, a draft, a
filter set) while a Go screen reads it at render or action time and
occasionally pushes a value back.

## Declare

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/local"

type Draft struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}
type View struct {
	Compact bool `json:"compact"`
}
-->
```go
var Site = local.New("site")

var Drafts = local.Define[Draft](Site, "drafts", local.CollectionConfig{
	Version:        2,
	KeyField:       "id",      // put(value) reads the key from value.id
	MaxRecordBytes: 32 << 10,  // per record, JSON text
	MaxRecords:     200,
	MaxBytes:       1 << 20,   // per collection
	Migrations: []local.Migration{
		{Version: 2, Steps: []local.Step{
			local.Rename("body", "text"),
			local.Default("tags", []string{}),
			local.Remove("legacy"),
		}},
	},
})

// Tiny values the server must know at first paint ride a cookie too.
var Prefs = local.Define[View](Site, "prefs", local.CollectionConfig{Version: 1, Mirror: true})
_, _ = Drafts, Prefs
```

- **Names.** An app id is `^[a-z][a-z0-9-]{0,31}$`, a collection
  `^[a-z][a-z0-9-]{0,63}$`, a record key 1 to `local.KeyMaxLen` (256)
  bytes. Declaring an app id or a collection twice panics at startup.
- **Namespace.** A record is the primitive entry
  `local.<app>.<collection>:<key>` under `gofastr.state.`, so nothing an
  app writes can name another feature's storage. A collection's version
  lives at `local.<app>.<collection>`.
- **Caps.** Defaults are 64 KiB per record, 1000 records and 1 MiB per
  collection; the ceilings are 1 MiB, 100 000 and 64 MiB (the `Default*`
  and `*Limit` constants). A `Mirror` collection defaults to 512 bytes
  and 4 records, clamped to 1 KiB and 16 (`Mirror*`): every record rides
  a cookie on every request.
- **Caps across tabs.** Every write to a collection queues on one chain
  per document and holds a Web Lock on the collection while it reads the
  total and writes, so two tabs writing at once cannot pass the cap
  either. Safari has no Web Locks; there the cap holds per tab, and two
  tabs writing at the same instant can pass it by a few records. An app
  that must hold the cap there writes from one tab.
- **Versions.** `Version` is 1 or more, and every step from 2 to `Version`
  needs a `Migration` (an empty one is fine). The browser runs the steps
  in (stored, declared] once, before the page's first read or write of
  the collection, under the Web Locks API when the browser has it.
  `Rename`, `Default` and `Remove` are declared in Go; `local.Func("name")`
  runs `window.__gofastr._localMigrations["name"]`, a function the app
  serves on the extra-script rail (never inline). A missing function
  leaves records and version untouched, reason `migration`.
- **The record type** must round-trip through `encoding/json`; `Define`
  panics on one that does not.

## Serve the declaration

The browser reads the declaration from `window.__gofastr_local[<app>]`,
a script the store generates. It rides the host's extra-script rail, so
it loads after `runtime.js` on every full shell render and never inline;
a declaration read from the DOM would let markup planted in an island
response redefine a collection's caps or migrations.

`Store.Script` returns both halves, the URL for the rail and the step
that mounts the handler. Most `uihost` apps build the host before the
router, so the URL goes on the rail first and the mount runs later:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/framework/local"
import "github.com/DonaldMurillo/gofastr/framework/uihost"

var Site = local.New("docs-script")
var rt local.ScriptRouter // app.Router(), built later
var site = app.NewApp("docs2")
-->
```go
url, mount := Site.Script()
host := uihost.New(site, uihost.WithExtraScripts(url))
// … the app and its router are built here …
mount(rt) // mount(app.Router())
_ = host
```

**Both halves are required, together.** Doing only one fails silently at
runtime: the manifest 404s, `window.__gofastr_local` stays undefined and
`__gofastr.localStore(app)` answers `null`. The module warns in the
console with the app id and the manifest URL it expected, the only
breadcrumb there is. A host that mounts its own routes uses
`Store.ScriptPath()` and `Store.ScriptHandler()` for the route half.

Your own page script is never inline either (`make csp-check` enforces
`script-src 'self'`): name it in `uihost.WithExtraScripts` beside the
declaration and serve it on the router. It runs after `runtime.js`, so
it reaches the store with `await __gofastr.loadModule('local-store')`.

The global is readable and carries the caps `Define` resolved:
`window.__gofastr_local["<app>"]` is `{collections: {"<name>": {v, key,
maxRecord, maxRecords, maxBytes, mirror, migrations}}, mirrorMax}`. Read
it in a console to see why a write was refused; never edit it.

## The browser API

After `__gofastr.loadModule('local-store')` (or once any page markup
carries a `data-local-store` marker), `__gofastr.localStore('site')` is
the store and `.collection('drafts')` one collection:

| Call | Resolves |
| --- | --- |
| `get(key)` | the record, or `undefined` |
| `put(key, value)` / `put(value)` with a key field | `{ok, reason}`; `reason` is `key`, `encode`, `size` (over the record cap), `full` (over the collection's record or byte cap), `quota` or `unavailable` |
| `delete(key)` | `{ok, reason}` |
| `list({orderBy, desc})` | `[{key, value}]`; `orderBy` is a top-level field. A page filters or slices the array itself |
| `count()` | the number of records |
| `subscribe(fn)` | unsubscribe; `fn({app, collection, key, source})` after every write, `source` `local` for this tab's own (a response's included) and `tab` for another tab's |
| `clear()` | drops every record of the collection |

The store itself is `{app, collections, collection(name), clear()}`;
its `clear` empties every collection (logout). Every call settles; none
throws. A refusal raises `gofastr:local-error` on `window` with `{app,
collection, key, reason, size, max}`, and a completed migration raises
`gofastr:local-migrated` with `{app, collection, from, to}`. A collection
whose migration did not complete refuses every call with reason
`migration`, a stored version above the declared one with reason
`version`. Two tabs of one origin converge: a write in one is announced
to the other, which re-reads.

## The bridges to Go screens

All four are explicit and bounded; nothing undeclared is ever uploaded.

### Seed: a signal filled from a record

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core-ui/store"
import "github.com/DonaldMurillo/gofastr/framework/local"

type Draft struct{ Title string }
var Site = local.New("docs-seed")
var Drafts = local.Define[Draft](Site, "drafts", local.CollectionConfig{Version: 1})
var S = store.New("editor")
var ctx = context.Background()
-->
```go
current := local.SeedSignal(Drafts, "current", store.JSON[Draft](S, "current", Draft{Title: "untitled"}))
_ = current.Bind(ctx, "p", nil) // data-fui-signal + data-local-seed="drafts:current"
```

The server renders the slice's value. After hydration the runtime reads
the record and patches the signal in place, writes every later value of
the signal back to the record, and mirrors another tab's write in. The
marker rides on the bindings (`Bind` renders them, `Attrs()` returns them
for your own element), so a page that never binds the slice never
restores it. `SeedSignal` implies `Global()` and refuses a slice that is
already `store.Persist`-ed: one owner per browser value.

A page script writes the signal with `__gofastr.setSignal(name, value)`
and the bridge carries the value on to the record; there is no separate
"save" call. Read `name` off the bound element's `data-fui-signal`
attribute rather than retype it.

**A persisted value cannot appear at first paint.** IndexedDB is
asynchronous, so SSR always paints the server's value and the record
lands after hydration. A screen that must not flash needs a `Mirror`
collection, below.

**A seeded signal is a SCALAR.** A `store.Slice` renders its value as
text, so a slice of a struct paints its own JSON on the page. Seed a
string, a number or a bool and keep the records on the browser side of
the bridge, where the page script that draws them is.

### Mirror: tiny values on the request

A collection declared `Mirror: true` keeps each record in a cookie too,
`gofastr.local.<app>.<collection>.<key>`, written by the runtime on every
put and cleared on delete, `SameSite=Lax`, and `Secure` on https. A Go
render reads it with `local.Get` or `local.List` through the request on
the context (`app.WithRequest`, which the host installs for every
screen):

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/framework/local"

type View struct{ Compact bool }
var Site = local.New("docs-mirror")
var Prefs = local.Define[View](Site, "prefs", local.CollectionConfig{Version: 1, Mirror: true})
var ctx = context.Background()
-->
```go
view, src, err := local.Get(ctx, Prefs, "view")
_, _, _ = view, src, err
```

`src` says where the value came from, and reading it is not optional for
anything that matters. `local.SourceMirror` means the value came from a cookie;
`local.SourceUpload` means it came through `Upload.Wrap`, on a request
whose trigger declared it; `local.SourceNone` means the request carried
nothing (`src.Found()` is the plain "did anything arrive"). `List`
returns `[]local.Record[T]`, `{Key, Value, Source}`, and a list can mix
the two sources.

**A mirror read is a client hint.** The browser wrote that cookie, so any
script on the origin can write it and any client can forge it with
`curl`. It is validated (declared and mirrored collection, valid key,
size cap, JSON shape into `T`) and never trusted, and it cannot be
signed, because the server never saw the value before the browser stored
it. Treat it like a query parameter: fine for deciding what to paint,
never proof of anything. A handler that authorises on a record must
require `local.SourceUpload`, and even that is a record the browser
supplied.

It travels on every request, which is why the caps are cookie-sized and
why every store's mirrored collections share one budget
(`local.MirrorStoreMaxBytes`, 4 KiB, summed over every store the process
declares, because the Cookie header is per origin and not per store): a
Cookie header past the 8 to 16 KiB most proxies allow is a 431 the user
can only clear by hand. `Define` panics over the budget, naming the
stores that share it, and the browser refuses the cookie (not the
record) with `gofastr:local-error{reason:"mirror"}` if the encoded
reality passes it.

**What the mirror is for.** A few small preferences: a theme, a
collapsed sidebar, the last tab. Cookies are the only channel a
browser-held value has to a render at first paint, so the budget is the
budget. A larger record has two honest shapes, and neither is a mirror:
**read it at action time** through the upload bridge, or **render a
placeholder and let the seed fill it** after hydration.

### Upload: what accompanies a request

<!-- gofastr:compile
import "context"
import "net/http"
import "github.com/DonaldMurillo/gofastr/core-ui/html"
import "github.com/DonaldMurillo/gofastr/framework/local"
import "github.com/DonaldMurillo/gofastr/framework/ui"

type Draft struct{ Title string }
var Site = local.New("docs-upload")
var Drafts = local.Define[Draft](Site, "drafts", local.CollectionConfig{Version: 1})
var mux = http.NewServeMux()
var ctx = context.Background()
-->
```go
upload := local.Send(Drafts.Key("current")) // or a whole collection: local.Send(Drafts)

form := ui.Form(ui.FormConfig{Action: "/drafts/upload", SubmitLabel: "Upload",
	ExtraAttrs: upload.Merge(html.Attrs{"data-fui-rpc": "/drafts/upload"})})
_ = form

mux.Handle("/drafts/upload", upload.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	// Require the upload: a record that arrived on a cookie is a hint.
	draft, src, err := local.Get(r.Context(), Drafts, "current")
	_, _, _ = draft, src == local.SourceUpload, err
}))
```

`Send` takes any mix of collections and `Collection.Key(k)` refs.
`Merge` (or `Attrs`, for your own element) puts four attributes on the
RPC trigger: `data-local-store`, `data-local-send="drafts:current"`,
`data-local-max` and `data-fui-rpc-with="local-bridge"`. The runtime
loads the module before dispatching and its request hook attaches
exactly the named records as the reserved field `__local`
(`{"<collection>": [{"k": key, "v": value}, …]}`) in a JSON body, a form
body, or a fresh JSON body when the trigger had none. A GET trigger
carries nothing, and `Merge` panics on one.

**The upload fails closed.** A trigger that declares records is promising
the handler those records, so when they cannot be attached (no such
store, a body the field cannot ride on, a gather that failed, or a
payload past `data-local-max`) the request is not sent; the page hears
`gofastr:local-error` with the reason and `gofastr:rpc-refused` with the
path. Sending it anyway would hand the handler something that looks
complete and is not.

`Upload.Wrap` (or `HandlerFunc`) reads the field on the server: the body
is bounded by `Max` (413 past it, `local.ErrTooLarge`), a JSON body is
decoded strictly, a duplicate or case-folded key, an undeclared
collection or key, or a malformed key is a 400 with a text reason, a
record over a cap is a 413, and the field is stripped so the wrapped
handler decodes the body it always did. The records are then on the
context for `Get` and `List` as `local.SourceUpload`, which wins over a
mirror cookie for the same key.

`Max` defaults to what the `Send` named, and to no more than that: per
item the most it can put on the wire (a whole collection is bounded by
the SMALLER of `MaxBytes` and `MaxRecords x MaxRecordBytes`, one key by
one record) plus `local.UploadRecordOverhead` per record and
`local.UploadBodySlack` for the rest of the body, capped at
`local.UploadMaxBytesLimit`. There is no floor. A trigger that carries a
large body of its own beside its records raises the bound with `Max`.
`Wrap` re-encodes a JSON body after lifting the field out, so a handler
that hashes or signs the raw body must do it upstream of `Wrap`.

`Get` and `List` see the upload only inside `Wrap`, and the read is empty
rather than an error, so under `GOFASTR_DEV` (what `gofastr dev` sets)
the package logs a warning naming the collection the first time an
unwrapped handler reads an unmirrored one, the only case that can never
be a browser with an empty store.

### Download: records written from a response

<!-- gofastr:compile
import "net/http"
import "github.com/DonaldMurillo/gofastr/framework/local"

type Draft struct{ Title string }
var Site = local.New("docs-download")
var Drafts = local.Define[Draft](Site, "drafts", local.CollectionConfig{Version: 1})
var w http.ResponseWriter
var r *http.Request
-->
```go
_ = local.Put(w, Drafts, "current", Draft{Title: "saved on the server"})
_ = local.Delete(w, Drafts, "stale")
_ = local.Clear(w, Site)             // logout, on an RPC response
local.ClearOnNextLoad(w, r, Site)     // logout by a full navigation + redirect
```

`Put`, `Delete` and `Clear` accumulate on one `X-Gofastr-Local` header
(`local.ResponseHeader`; ASCII, 16 KiB cap, one store per response); the
runtime's response hook applies each op through the same put/delete a
page write uses, so the caps hold and subscribers hear it. A response
that needs more than a few records is pushing a dataset, which is what a
body is for. `ClearOnNextLoad` plants a script-readable cookie the module
honours once on its next load, for a logout that never reaches `rpc.js`.

**Logout is the app's line to write.** `battery/auth`'s logout does not
clear the store: it cannot know which stores the app declared, and the
records are the app's, not the session's. Nothing expires them either;
a mirror cookie lives a year, so the previous user's preferences are
what the next user's first paint renders. There are two wiring points.
A logout that answers an RPC calls `local.Clear(w, Site)` on the
response, and the runtime's response hook empties every collection and
drops every mirror cookie. A logout that is a full navigation (a form
POST answered with a redirect) calls `local.ClearOnNextLoad(w, r, Site)`
before the redirect, and the module clears the store on the page the
redirect lands on, marker or not. `examples/site` wires the second at
`/__site/local/logout`.

## Rules the package keeps

- **No inline scripts.** All three modules are registered behaviours, and
  the manifest, any migration function and your own page script ride the
  extra-script rail; `make csp-check` stays green.
- **State survives soft navigation and the route cache.** The seed
  bridge re-applies the record on every scan, including one whose DOM
  came back from the cache; a seeded slice is app-global, so the signal
  survives too. Proven in `framework/local/local_e2e_test.go`.
- **No server-side memory of browser state.** The server sees a record
  only on the request that carried it; nothing is kept between requests.
- **Multi-tab consistency.** Every write is announced to the origin's
  other tabs, which re-read; a seeded signal follows a sibling tab's write.
- **Best-effort.** Private mode, a blocked origin, a full quota and a
  hand-cleared store are all normal. Never keep something here whose
  loss is a bug.
- **A migration is all-or-nothing, and a collection that did not migrate
  answers nothing.** Every record's write is checked before the new
  version is stamped, so a half-finished rewrite is never recorded as
  done; until it succeeds, every method refuses with reason `migration`.
  A stored version ABOVE the declared one (a rolled-back deploy meeting a
  browser that already moved on) fails the collection with reason
  `version`. Roll a schema forward only: to roll back, ship a higher
  version with a no-op migration rather than lowering it.
- **The download bridge is advisory.** The response has already been
  written when the browser reads `X-Gofastr-Local`, so an op the browser
  refuses (a cap, a collection it does not know, no store at all) leaves
  the server believing it wrote. Every refusal is raised as
  `gofastr:local-error{reason:"download"}`; nothing reports back.
- **No secrets, no session tokens.** The store is readable by any script
  on the origin, and a mirrored record travels on every request as a
  cookie. A session is a signed token in an `HttpOnly` cookie
  (the Sessions section of [Reactivity](reactivity.md)); it never belongs here.

## The runnable proof

`examples/site` at `/forms/draft-notes`: a draft kept in the browser, a
signal seeded from a record, a mirrored preference rendered at first
paint, an upload whose Go handler reads the declared record and writes a
receipt back. Browser coverage: `examples/site/e2e_local_test.go` and
`framework/local/local_e2e_test.go`.

## See also

- [Signal store](signal-store.md): `store.Persist`, the one-slice cousin
  with no server bridge.
- [Reactivity](reactivity.md): where local state sits on the ladder.
- [Runtime contract](runtime-contract.md): the `local` primitive and the
  `data-fui-rpc-with` seam.
- [UI capability map](ui-capability-map.md): the state boundary.

## Common mistakes

- **Expecting a record at first paint.** The IndexedDB read lands after
  hydration. A value the server must render on the first byte is a
  `Mirror` collection read with `local.Get`, not a seeded signal.
- **Treating an upload or a cookie as trusted.** Both are client hints.
  The package validates shape, size and declaration; the handler still
  authorises and validates the content like any request body.
- **Looking for a WebSocket bridge.** There is none: the upload is a
  request body and the download a response header, and both need an HTTP
  request to ride on. A socket-driven app uploads through an action.
- **Raising `Version` without a migration.** Every step from 2 to
  `Version` needs a `Migration`, even an empty one; `Define` panics
  otherwise, so a typo in the version cannot silently orphan data.
- **Persisting the same value twice.** A slice cannot be both
  `store.Persist`-ed and seeded; `SeedSignal` panics on one that is.
- **Expecting logout to clear the store.** `battery/auth` ends the
  session and knows nothing about the app's stores; the records and the
  year-long mirror cookies stay for the next user of the browser. Call
  `local.Clear` on an RPC logout response, or `local.ClearOnNextLoad`
  before the redirect of a full-navigation logout.
