# Browser-local databases (`core-ui/localdb`)

`core-ui/localdb` declares IndexedDB databases and indexes in Go and gives page scripts a clean promise API with transactions, index queries, and cross-tab change notifications — working on both live servers and static exports.

For standard forms and lists without writing JS, see [`framework/localentity`](local-entities.md), which builds on this package.

## Declare a database in Go

Declare at package level before the UI host serves its first page (`uihost` emits the schema automatically in `<script type="application/json" id="gofastr-localdb">`):

<!-- gofastr:compile
-->
```go
package main

import "github.com/DonaldMurillo/gofastr/core-ui/localdb"

var (
	Pokedex = localdb.New("pokedex")

	Members = Pokedex.Store("members",
		localdb.AutoKey(),
		localdb.Index("by_level", "level"),
		localdb.Index("by_team_slot", "team", "slot"),
		localdb.MultiEntryIndex("by_type", "types"),
	)

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

## Use from a page script

```js
await __gofastr.loadModule('localdb');
const db = await __gofastr.localdb.open('pokedex');

const id = await db.put('members', { name: 'Pikachu', level: 30, types: ['electric'] });
const top = await db.list('members', { index: 'by_level', direction: 'prev', limit: 6 });

// Atomic multi-store transaction (await ONLY t.* methods inside tx):
await db.tx(['members', 'tags'], 'readwrite', async (t) => {
  await t.put('members', { name: 'Eevee', level: 5 });
  await t.put('tags', { slug: 'normal', label: 'Normal' });
});

// Watch local and other-tab changes (BroadcastChannel):
const stop = db.watch('members', (e) => refreshTeam());
```

Methods: `get`, `put`, `add`, `delete`, `clear`, `count`, `list` (`index`, `only`/`lower`/`upper`, `direction`, `offset`, `limit`, `keys`), `tx`, `watch`, plus `__gofastr.localdb.persist()` and `estimate()`. Rejections are `LocalDBError` with a closed `code` (`unsupported`, `unknown-db`, `invalid`, `constraint`, `quota`, `schema`, `aborted`, `failed`).

## Schema evolution

Additive stores and indexes upgrade automatically without manual version numbers; removed stores/indexes stay untouched so older open tabs never break. Changing an existing store's key path or index definition in place rejects with `schema` — declare a new store or index name instead.

## Common mistakes

- **Awaiting `fetch` or timers inside `db.tx(...)`:** IndexedDB auto-commits as soon as no DB request is pending, aborting later `t.put` calls. Do network/async work outside `tx`.
- **Trusting records read from IndexedDB:** visitors can edit IndexedDB in DevTools. Always insert via `textContent` and validate shapes.
- **Declaring `localdb.New` inside a handler:** declare at package level so it appears in the page manifest and doesn't panic on duplicate registration.
- **Using `localdb` as the sole store for critical user data:** call `persist()`, and back important data on the server.
