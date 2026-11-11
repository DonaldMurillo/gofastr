# ShopFront: the declaration-driven flagship

This is the largest example in the repo and the proof of the framework's
thesis: a complete storefront described **once** in
[`gofastr.yml`](gofastr.yml): 5 related entities (categories, products,
orders, order_items, reviews), a themed 8-screen UI, custom endpoints,
seed data, middleware, and a plugin, all emitted as runnable Go by a
single command. The owned `app/entities/order_totals_hooks.go` adds the
transactional catalog-pricing and order-total derivations to that scaffold.

## What it proves

One app spec fans out into every surface: auto-migrated SQL schema,
REST CRUD with validation and cursor pagination, OpenAPI, 25 MCP tools
for agents, and a server-rendered themed storefront. All of it is
asserted live by [`flagship_test.go`](flagship_test.go), which
regenerates `app/` from the app spec, builds the binary, boots it, and
hits each surface, so the proof never depends on a stale checkout.

## Secure by default

The app generator ships with auth enabled (`app.auth.enabled: true`) and the
customer-PII entities, `orders` and `order_items`, owner-scoped via
`owner_field: user_id`. The framework stamps the owner on create and
scopes every list/get/update/delete (REST *and* MCP) to the requesting
user, failing closed with 401 when no owner can be derived.
`TestOrdersOwnerScoped` pins this. (The first cut shipped insecure;
git history has the full story of the fix.)

Line prices and names are server-owned: `order_items.unit_price`,
`total_price` and `product_name` are read-only in the app spec and generated
request types. After each item create/update, the owned lifecycle hook reads
the stored product and quantity, copies the product's current name and price,
multiplies the price using exact decimal
arithmetic at four decimal places, and then recomputes the parent order.
Pricing and totals share the CRUD transaction, so failures roll back the write.
Order subtotals and totals are summed in Go with exact decimal arithmetic at the
columns' four-decimal scale, including tax and shipping; SQLite's floating-point
SUM never computes money. On SQLite, a derived line price, subtotal or total
beyond the decimal exactness limit (15 significant digits) is refused before
persistence, and the write and its parent-total changes roll back.
Clients can omit both price fields; supplied values cannot set a price. The
order's `subtotal` and `total` are read-only the same way: a create need not
send them, and a value sent is not stored.
Quantity-only edits and product changes reprice against the current catalog.
Only an active product can be ordered: a line that names a draft, archived or
deleted product is refused, on create and on any later edit. The demo keeps no
stock count.
Once an order ships, is cancelled or is refunded, its items, tax and shipping
cost are settled: adding, editing, moving or deleting a line, or editing either
charge, is refused, while other order fields stay editable.
Keep the owned hook file when scaffolding; generated hook stubs are not its
implementation. `TestOrderPricesComeFromProducts` exercises this through REST.

Maintainer note: read-only fields are omitted from ordinary CRUD SQL even if
a before-hook fills them. Their zero defaults only bridge the insert; the
after-hook writes authoritative prices inside the same transaction, before
totals, events, and responses. Do not move pricing into a before-hook alone.
This demo has no upgrade/backfill migration: existing line items are repriced
when written, not merely when a newer executable starts.

## Regenerate and run

```sh
cd examples/ecommerce
gofastr generate app gofastr.yml --force   # re-emits app/ from the app spec (output_dir: app)
gofastr dev --dir app                         # hot-reload dev server on localhost:8080
```

`app/` is committed, owned Go. `output_dir: app` in the app spec puts the
app in a subpackage so the example also hosts its own test files.

## Read next

- [`flagship_test.go`](flagship_test.go): the end-to-end proof.
- [`gofastr.yml`](gofastr.yml): the entire input.
