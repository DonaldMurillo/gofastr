# Custom endpoints

An entity's CRUD routes cover reads and writes of its rows. An operation
that is neither, such as refunding an order or featuring a post, is a
custom endpoint: a route under the entity with a Go handler you write.

Declare it under `endpoints:` in [`gofastr.settings.yml`](settings.md) and
link the handler. `gofastr generate` reads the handler's input and output
types from the Go source, so the YAML holds only what the code cannot say:

```yaml
endpoints:
  - name: refund_order
    entity: orders
    method: POST
    path: "{id}/refund"
    handler: entities/refunds.go#RefundOrder
    description: Refund an order, in full or in part.
    mcp: true
```

```go
package entities

type RefundOrderInput struct {
    ID     string  `path:"id" json:"-"`
    Amount *int    `json:"amount"`
    Reason string  `json:"reason"`
}

func RefundOrder(ctx context.Context, in RefundOrderInput) (Orders, error) {
    // ...
}
```

From those two pieces you get:

- `entities/endpoints.gen.go`, which registers the route
  `POST /orders/{id}/refund` with typed input and output schemas.
- `c.RefundOrder(ctx, id, client.RefundOrderRequest{...})` in the Go client
  (`entities/client`) and the Go SDK.
- `api.orders.refundOrder(id, {...})` in the JS SDK, typed in `client.d.ts`.
- `shop orders refund-order <id> --reason ... [--amount 5]` in the
  generated CLI.
- An OpenAPI operation, and an MCP tool named `refund_order` when
  `mcp: true`.

The SDK schema hash covers endpoints, so a changed input type shows up as
drift like any entity change.

## Keys

| Key | Required | Meaning |
| --- | --- | --- |
| `name` | yes | snake_case. Names the calls: `RefundOrder` in Go, `refundOrder` in JS, `refund-order` in the CLI, and the MCP tool. |
| `entity` | yes | The entity declared in `entities/` that the route hangs under. |
| `method` | yes | `GET`, `POST`, `PUT`, `PATCH` or `DELETE`. |
| `path` | yes | Relative to the entity's route: `"{id}/refund"` on `orders` serves `/orders/{id}/refund`. Quote a path that starts with `{`. |
| `handler` | yes | `entities/<file>.go#<Name>`: a file in the entities package and the func or var in it. |
| `description` | no | One line for docs, OpenAPI, the MCP tool and the CLI help. |
| `mcp` | no | Also register the endpoint as an MCP tool. |
| `public` | no | Serve callers with no session, over HTTP and MCP. |

## The handler

The linked name is either a func or a typed var:

```go
func RefundOrder(ctx context.Context, in RefundOrderInput) (Orders, error)

var SearchOrders handler.Handler[SearchOrdersInput, []Orders] = func(ctx context.Context, in SearchOrdersInput) ([]Orders, error) {
    // ...
}
```

The output is an entity type (`Orders`, `*Orders`), a slice of one
(`[]Orders`), or `struct{}` for an endpoint that returns nothing. The input
is a named struct in the entities package. Each field says where it travels
with the same tags `handler.Bind` reads:

- `path:"id" json:"-"`: a `{id}` segment of the path. Every `{name}` in the
  path needs one. Never a pointer: a path value is always present.
- `query:"q" json:"-"`: a query-string value. Only `GET` and `DELETE` take
  them, and they take nothing else besides path values. `query:"q,empty"`
  keeps an explicit `?q=` (see below); it is the only option a query tag
  takes, and a path tag takes none.
- `json:"amount"`: a JSON body key, for `POST`, `PUT` and `PATCH`.

A pointer field is optional; any other field is required. The generated CLI
refuses a call that leaves out a required flag before it sends anything.
Path and query values are strings, numbers or bools. Body fields may also be
`time.Time`, `json.RawMessage`, slices, maps, or structs declared in the
entities package. That limit is the generator's: the generated clients and
CLI spell a path or query value as a plain scalar. A hand-registered handler gets all
of `handler.Bind`, which also binds a `time.Time` or any other
`encoding.TextUnmarshaler` from a query, path or header value (see
[core packages](core-packages.md) → handler).

An empty query value (`?q=`) is the same as no value: the field stays at
its zero value and a pointer field stays nil. Before this release `?q=`
bound `""` into a string field, and `?page=` failed an int field with a
400. A field tagged `query:"q,empty"` gets the empty value instead: a
`*string` is a non-nil pointer to `""`, so the handler can tell `?q=` from
no `q`. The generated Go client sends `?q=` when an optional field points
at `""`, and the JS client when the value is `""`.

## Who can call it

An endpoint answers only a signed-in caller unless it sets `public: true`.
Over HTTP a request with no user gets 401, and every gated response carries
`Cache-Control: no-store`. The MCP tool requires an authenticated caller the
same way. A write that a plain HTML form could send from another site (no
`Content-Type`, urlencoded, multipart or `text/plain`) gets 403, public or
not. The generated clients send JSON, so they pass.

These gates apply to every `framework.Endpoint`, including ones registered
by hand in Go. See [entity declarations](entity-declarations.md) → Custom
Endpoints.

## Rebuilding

Plain `gofastr generate` rebuilds `entities/endpoints.gen.go` whenever the
settings file has an `endpoints:` section, and deletes it when the section
goes away. The same run rebuilds `entities/client` and the configured `cli`
and `sdk` generators, which read the endpoints from the settings file.
`gofastr build` and `gofastr generate --watch` do the same.
`gofastr generate endpoints` rebuilds only `endpoints.gen.go`.

## Starting from an app spec

An app spec's top-level `endpoints:` list takes the same entries. `gofastr
generate app` writes them into this settings section, writes each linked
handler as a stub that answers 501 Not Implemented, and writes
`endpoints.gen.go` and the client calls with it. `gofastr pack` reads the
section back. See [App specs](app-spec.md#custom-endpoints-endpoints).

## Common mistakes

- A `name` that an entity resource already uses: `list`, `get`, `create`,
  `update`, `patch`, `remove`, the batch and watch calls, a state move's key,
  or a CLI verb. Generation refuses it and names the clash. The same goes
  for a name the Go client already holds as a field: `token`, `h_t_t_p` or
  `base_u_r_l` would declare a `Client.Token`, `Client.HTTP` or
  `Client.BaseURL` method beside the field of that name.
- Two names that differ only before a digit, such as `cache_bust1` and
  `cache_bust_1`. Both become `CacheBust1`, so generation refuses the
  pair, across entities too: the Go client's methods share one namespace.
- A body field on a `GET` or `DELETE`, or a query field on a `POST`. Tag the
  field for where the method carries input.
- A path field without `json:"-"`. A body key of the same name would
  overwrite the path value, so generation refuses it.
- A handler that returns an entity the SDK or CLI selection leaves out.
  The generated client could not name the type, so generation refuses it.
  Add the entity to the selection or return `struct{}`.
- Expecting a hand-written `app.Router().Handle` route to get these gates.
  Only `Endpoint` routes do.
