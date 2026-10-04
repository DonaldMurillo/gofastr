# Browser-local databases (localdb)

`core-ui/localdb` declares IndexedDB databases in Go. The data lives in
the visitor's browser: no server table, no request per write. The
`localdb` runtime module reads the declarations and gives page scripts
a small API over them: read and write records, list them in index
order, run several writes as one transaction, and hear about changes
from this tab and every other tab of the site.

Use it for state that belongs to one visitor's browser and must outlive
the tab: a draft, a saved list, settings, a game save. It survives
reloads, closed tabs and restarts. For records you want rendered as
forms and lists without writing JavaScript, use
[local entities](local-entities.md), which are built on this package.

## Declare a database

Declare databases at package level, before the UI host serves its first
page. Each store keys its records by `id` unless you say otherwise.

<!-- gofastr:compile
-->
```go
package main

import "github.com/DonaldMurillo/gofastr/core-ui/localdb"

var (
	Pokedex = localdb.New("pokedex")

	// Records keyed by "id"; AutoKey mints a UUIDv7 when a record has none.
	Members = Pokedex.Store("members",
		localdb.AutoKey(),
		localdb.Index("by_level", "level"),
		localdb.Index("by_team_slot", "team", "slot"), // compound
		localdb.MultiEntryIndex("by_type", "types"),   // one entry per array element
	)

	// Records keyed by their own "slug"; a duplicate label is refused.
	Tags = Pokedex.Store("tags",
		localdb.KeyPath("slug"),
		localdb.UniqueIndex("by_label", "label"),
	)
)

func main() {
	_ = Members
	_ = Tags
}
```

Names (database, store, index) are 1-64 lowercase letters, digits, `-`
or `_`. Key paths are dotted ASCII identifiers (`meta.slug`);
`__proto__`, `constructor` and `prototype` are refused. A bad
declaration panics at startup. The browser stores the database as
`gofastr.<name>`, so it never collides with one your own scripts open.

`framework/uihost` puts the declarations in every page head as an inert
`<script type="application/json" id="gofastr-localdb">` block, on live
pages and in static export. There is nothing to mount.

The browser module ships with this package, not with the core runtime.
An app that never imports `core-ui/localdb` contains none of it, and a
page downloads it only when something calls
`__gofastr.loadModule('localdb')`.

## Use it from a page script

Load the module, open the database, call it. Every call returns a
promise.

```js
await __gofastr.loadModule('localdb');
const db = await __gofastr.localdb.open('pokedex');

const id = await db.put('members', { name: 'Pikachu', level: 30, types: ['electric'] });
const pika = await db.get('members', id);

// Highest level first, six at most: IndexedDB does the ordering.
const top = await db.list('members', { index: 'by_level', direction: 'prev', limit: 6 });

// Levels 20 to 40, inclusive.
const mid = await db.list('members', { index: 'by_level', lower: 20, upper: 40 });

await db.delete('members', id);
```

| Method | Does |
| --- | --- |
| `get(store, key)` | The record, or `undefined`. |
| `put(store, record)` | Insert or replace. Resolves the key. |
| `add(store, record)` | Insert only; an existing key rejects `constraint`. |
| `delete(store, key)` / `clear(store)` | Remove one record / all of them. |
| `count(store, query?)` | How many records match. |
| `list(store, query?)` | Records in key order, or in `query.index` order. |
| `tx(stores, mode, fn)` | Several operations as one transaction. |
| `watch(store, fn)` | Change notifications. Returns the unsubscribe function. |

A `query` takes `index`, then either `only` or `lower`/`upper` (with
`lowerOpen`/`upperOpen` for exclusive bounds), plus `direction`
(`next`, `prev`, `nextunique`, `prevunique`), `offset`, `limit`, and
`keys: true` to get primary keys instead of records.

Each single call is its own transaction and resolves once the write is
committed to disk, not before.

### Transactions

`tx` runs a function against several stores at once. If the function
throws, nothing it wrote is kept.

```js
await db.tx(['members', 'tags'], 'readwrite', async (t) => {
  await t.put('members', { name: 'Eevee', level: 5 });
  await t.put('tags', { slug: 'normal', label: 'Normal' });
});
```

Only await `t`'s own methods inside the function. IndexedDB commits a
transaction as soon as it has no request pending, so awaiting a `fetch`
in the middle ends it early.

### Watching for changes

```js
const stop = db.watch('members', (e) => {
  // e.origin is "local" (this tab) or "remote" (another tab).
  // e.changes is [{op: "put" | "add" | "delete" | "clear", key}].
  refreshTeam();
});
```

The watcher runs after the transaction commits. Other tabs hear about
the change through a `BroadcastChannel` that carries store names, ops
and keys only, never record values; the other tab reads the record from
the database the tabs share. A message on that channel naming an
undeclared store or an unknown op is ignored.

## Schema changes

There is no version number to keep. When a page opens a database whose
stored schema is missing a declared store or index, the module raises
the IndexedDB version and creates it.

Nothing is ever deleted. A tab still open on the previous deploy may
need the store or index the new deploy removed, and a page from an
older deploy opening a newer database finds everything it declares.
Unused stores and indexes stay until the visitor clears site data.

Changing a store's key path, or an existing index's key path or
options, is not done in place. Opening such a store rejects with code
`schema`. A key path change would need every record re-keyed; an index
rebuilt in place would be rebuilt back by any tab still on the previous
deploy, each tab's upgrade closing the other's connection. Declare the
new shape under a new name (a new store, copying records over in a page
script, or a new index name, which needs no copying).

When another tab upgrades the database, this tab's open connection
closes and the next call reopens it. `document` gets a
`gofastr:localdb` event (`detail: {type, db}`, where `type` is
`versionchange`, `close` or `blocked`) if a page wants to show it.

## Keeping the data

Browsers can delete site data when the disk is short. Ask them not to:

```js
const kept = await __gofastr.localdb.persist();     // true if granted
const usage = await __gofastr.localdb.estimate();  // {usage, quota} in bytes, or null
```

Each browser decides differently. Chromium grants it to sites the
visitor uses often, Firefox may ask the visitor, and Safari grants it to
sites added to the home screen. `false` is an answer, not an error.
Data the visitor cannot afford to lose belongs on a server too.

## Errors

Every rejection is an `Error` with `name: "LocalDBError"` and a `code`:

| Code | Meaning |
| --- | --- |
| `unsupported` | The browser has no IndexedDB. |
| `unknown-db`, `unknown-store`, `unknown-index` | Not declared in Go. |
| `invalid` | A record that is not a plain object, a bad key, or a bad query. |
| `constraint` | A unique index or `add` refused a duplicate. |
| `quota` | The browser is out of storage for this site. |
| `schema` | A store's key path changed, or the schema could not be applied. |
| `version`, `closed`, `aborted`, `failed` | IndexedDB refused for another reason. |

The browser's own error message is dropped, so a record's contents
never end up in an error string. The module never writes to the
console.

## What it is not

- Not shared between visitors or devices. Each browser has its own copy.
- Not private from the visitor. Anyone with devtools can read and edit
  it, so treat every record you read back as untrusted input.
- Not a cache of server data. Server rows stay on the server and reach
  the page through [RPC](reactivity.md); this is for data that has no
  server copy.

## Common mistakes

- **Awaiting other work inside `tx`.** IndexedDB commits a transaction
  once no request is pending. A `fetch` or a timer awaited inside the
  function ends it, and the next `t.put` fails with `aborted`. Do the
  other work before or after.
- **Treating a read-back record as trusted.** The visitor (or any
  script on your origin) can change it. Put values into the page with
  `textContent`, never `innerHTML`, and check types before using them.
- **Changing a store's key path or an index's definition in place.**
  It rejects `schema`. Declare a new store (and copy the records across
  in a page script) or a new index name.
- **Storing the only copy of something important.** Browsers may
  delete site data, visitors clear it, and it never leaves the device.
  Call `persist()`, and keep a server copy of anything that matters.
- **Declaring a database inside a function.** `localdb.New` belongs at
  package level; a second call with the same name panics, and a
  declaration made after the first page render is missing from that
  page's manifest.
