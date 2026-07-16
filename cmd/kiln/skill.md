---
name: kiln
description: Build a GoFastr web app live by calling Kiln over HTTP. Use ONLY on explicit Kiln signals — $KILN_URL env var set, the user names Kiln ("kiln serve", "the kiln world", "kiln freeze"), or IR-mutation phrasing against a running Kiln. Do NOT trigger on "GoFastr" alone or on generic "build me an app" requests: a user building with the framework directly writes Go against `framework/` and this skill would mis-route them into HTTP IR mutations.
---

You are driving a live GoFastr Kiln server. Mutate the application only through
Kiln tools at `$KILN_URL`; do not edit the repository or start another server.

## Workflow

1. Call `world_get` before changing anything.
2. Make the smallest coherent tool calls that satisfy the user's request.
3. Read the returned structured result. On a conflict or validation error,
   follow its hint and retry; never claim success after a failed call.
4. Read `world_get` again and verify the exact entity/page/config state.
5. For a destructive delete, propose a plan with the exact target and wait for
   approval before calling the delete tool with that `plan_id`.

The HTTP form is:

```bash
curl -sS -X POST "$KILN_URL/kiln/tool/add_entity" \
  -H 'Content-Type: application/json' \
  -d '{"entity":{"name":"notes","fields":[
    {"name":"text","type":"string","required":true}
  ]}}'
```

Prefer native tool calling when available; HTTP and MCP reach the same typed
dispatcher.

## Current routing contract

Entity CRUD defaults to `/api/<entity>`; HTML screens use bare paths. An entity
`posts` and a screen `/posts` intentionally coexist. Forms targeting the entity
must post to `/api/posts`, not `/posts`.

`set_app_config` replaces app config. Its HTTP payload must wrap every field
under `config`; a flat payload is invalid:

```bash
curl -sS -X POST "$KILN_URL/kiln/tool/set_app_config" \
  -H 'Content-Type: application/json' \
  -d '{"config":{"name":"my-app","module":"example.com/my-app","db_driver":"sqlite","db_url":"app.db","api_prefix":"api"}}'
```

Include a name and module before freeze. An omitted or empty `api_prefix`
inside `config` resolves to `api`.

## Entities

Use the current declaration fields: `fields`, `relations`, `endpoints`,
`soft_delete`, `owner_field`, `cross_owner_read`, `search_fields`, `access`,
`timestamps`, `crud`, `mcp`, `cursor_field`/`cursor_fields`, `indices`, and
`properties`.

For per-user data, set `owner_field` to the user foreign-key column. Do not set
`multi_tenant: true`: Kiln cannot choose a tenant resolver and will reject it.
That integration belongs in owned Go after freeze.

## Screens

Send a complete, non-empty tree in `add_page`. Use design-system kinds for
layout and styling:

- `page_header`, `hero`, `section`, `card`
- `stack`, `cluster`, `grid`, `stat_row`, `stat_grid`, `stat_card`
- `link_button`, `callout`, `divider`

Use semantic leaf kinds only for their HTML meaning: `heading`, `paragraph`,
`text`, `link`, `form`, `label`, `input`, `button`, list and table elements.
Never send `class`, `style`, or `on*` props. They are rejected because styling
and interaction belong to GoFastr's shared component/runtime surfaces.

Example:

```json
{
  "page": {
    "path": "/",
    "name": "home",
    "title": "Home",
    "layout": {"name": "marketing"},
    "tree": {
      "kind": "stack",
      "props": {"gap": "xl"},
      "children": [
        {"kind": "hero", "props": {
          "eyebrow": "GoFastr",
          "title": "Build the app you can own",
          "subtitle": "Live first, plain Go when you freeze."
        }},
        {"kind": "section", "props": {
          "heading": "What ships", "description": "One current blueprint"
        }, "children": [
          {"kind": "grid", "props": {"min": "16rem", "gap": "lg"},
           "children": [
            {"kind": "card", "props": {"heading": "REST + OpenAPI"}},
            {"kind": "card", "props": {"heading": "SSR + hydration"}}
          ]}
        ]}
      ]
    }
  }
}
```

Kiln assigns `_id` values and a page `version`. Use `update_page_element` for
later surgical edits; pass the current version as `if_match` when possible.

## Scaffold surfaces

Use `set_scaffold` to replace navigation and owned-Go stubs:

```json
{
  "nav": [{"label":"Dashboard","href":"/dashboard"}],
  "endpoints": [{"name":"health","method":"GET","path":"/healthz"}],
  "middleware": [{"name":"request_logger"}],
  "plugins": [{"name":"metrics"}],
  "helpers": [{"name":"slug"}]
}
```

Navigation renders live. Endpoint/middleware/plugin/helper declarations become
editable Go stubs in the generated scaffold.

## Behavior: hooks, routes, seeds

Declarative behavior uses these tools (same dispatcher as everything else):

- `add_hook(hook)` / `delete_hook(id)` — entity lifecycle hooks. `when` is one
  of `before_create`, `after_create`, `before_update`, `after_update`,
  `before_delete`, `after_delete`, `before_list`, `after_list`. A hook has an
  `id`, an `entity`, a `when`, an optional `condition` (expression), and an
  `action`.
- `add_route(route)` / `delete_route(method, path)` — declarative HTTP routes.
- `add_seed(seed)` — insert seed rows: `{"entity":"posts","rows":[{...}]}`.
- `undo` — revert the most recent journal entry.

Action kinds (params are action-specific): `noop`; `validate`
`{ expression, message }` — errors with the message when false; `set_field`
`{ field, value }` (expression) or `{ field, value_literal }`; `audit`
`{ channel, message }`; `emit_event` `{ topic, data }`; `respond_json`
(routes only) `{ status, body }`.

The expression language covers `condition`, `value`, `message`, and
string-typed `body`: literals (numbers, strings, `true`/`false`/`null`,
lists), operators `+ - * / %`, `== != < > <= >=`, `&& || !`, member access
(`entity.title`, `ctx.user.role`, `result.id`), and built-ins `len`, `lower`,
`upper`, `contains`, `starts_with`, `ends_with`, `abs`, `min`, `max`, `now()`.
Hook scope: `entity` (the row), `ctx` (request), `result` (`after_*` only).

```bash
curl -sS -X POST "$KILN_URL/kiln/tool/add_hook" \
  -H 'Content-Type: application/json' \
  -d '{"hook":{"id":"posts-slug","entity":"posts","when":"before_create",
    "action":{"kind":"set_field","params":{"field":"slug","value":"lower(entity.title)"}}}}'
```

## Theme

`set_theme` accepts semantic light color tokens such as `primary`,
`primary-fg`, `background`, `surface`, `surface-soft`, `text`, `text-muted`,
`border`, `accent`, `success`, `warning`, `danger`, and `info`. Prefer the
default adaptive theme unless the user requested a brand palette. Dark tokens
are part of `set_app_config.config.theme_dark`.

## Freeze boundary

When the user wants source, tell them to run:

```bash
kiln freeze --diff
kiln freeze --dir build
gofastr validate build/gofastr.yml
gofastr generate --from=build/gofastr.yml --out=app
```

Freeze emits `gofastr.yml` and the lossless `world.json`; it does not emit the
removed `entities/*.json` format. Declarative behaviors that need a Go function
graduate as named owned-Go stubs, with the exact IR retained in `world.json`.
