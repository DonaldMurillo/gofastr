---
name: kiln
description: Build a GoFastr web app live by calling Kiln over HTTP. Use when the user wants to scaffold an app, add entities/fields, build UI pages, or compose hooks/routes. Triggers on $KILN_URL env var being set, or the user mentions Kiln, GoFastr, or asks to "build me an app", "add an entity", "wire a CRUD", "make a hook", etc.
---

# Kiln

Kiln is the in-app build-mode runtime for GoFastr. You compose web apps by calling tools that mutate a single in-memory IR ("the world"). The runtime journals every edit, auto-migrates SQLite, re-renders the live preview, and broadcasts SSE so the panel updates in real time.

**You never write Go, JS, or SQL inside Kiln.** All behavior is expressed declaratively through the tool surface. Custom logic uses a tiny built-in expression language; the engine evaluates it.

The user is watching the app live at the page URL itself — typically `$KILN_URL/` for the home page, `$KILN_URL/about` for an about page, etc. The floating chat widget rides along on every page; there is **no separate `/kiln/chat` URL to point users at**. When you finish, just confirm what landed and they'll see it on the page they're already on.

## How to call tools

Kiln exposes its full tool surface as **HTTP REST** at `$KILN_URL/kiln/tool/{name}`. Use bash + curl. This is the canonical transport — preferred over MCP because it has no startup race and works in every harness.

Every call follows the same shape:

```bash
curl -s -X POST "$KILN_URL/kiln/tool/<tool_name>" \
  -H 'Content-Type: application/json' \
  -d '<json args>'
```

Response is always:

```json
{ "ok": true,  "result": { ... } }
{ "ok": false, "error": "...", "kind": "validation|conflict|not_found|needs_plan", "hint": "..." }
```

If `$KILN_URL` isn't set, default to `http://localhost:8765`. The first `kiln serve` started in cwd is what you're talking to.

## Operating rules

- For small additive asks ("add a `priority` field"), call the right tool directly. Don't narrate.
- For any destructive op (`delete_entity`, `delete_field`, `delete_page`, `delete_hook`, `delete_route`), you MUST:
  1. Call `propose_plan` with `targets` listing every destructive op you intend to perform. Example: `targets: [{op:"delete_entity", name:"posts"}]`.
  2. Wait for the user to click Approve in the panel (which calls `approve_plan`).
  3. Call the destructive tool with `plan_id` set to your plan's id. The protocol enforces this — calling without an approved plan returns `{"ok":false,"kind":"needs_plan",...}`. Each (plan, target) is single-use; reuse requires a new plan.
- For large additive asks (>3 tool calls), `propose_plan` is recommended for visibility but not required.
- When `ok=false`, read `kind` and `hint` and self-correct. Don't repeat the same call.
- When unsure of state, GET `$KILN_URL/kiln/world` (or call the `world_get` tool with a path) before acting.

## Tool surface

### World inspection
- `world_get(path?)` — read the world IR. Path examples: `""` (full world), `entities.posts`, `pages./dashboard`, `_chat`, `_plans`.
  - Or just: `curl -s $KILN_URL/kiln/world` for everything.

### Entities (CRUD, OpenAPI, MCP all auto-generate from these)
- `add_entity(entity)` — declare a new entity with fields/relations/CRUD/MCP/soft-delete/multi-tenant flags.
- `update_entity(entity)` — replace an entity in full (prefer `add_field` for additive changes).
- `delete_entity(name, plan_id)` — drop an entity. Destructive: requires approved plan with target `{op:"delete_entity",name}`.
- `add_field(entity, field)` — append a field. Auto-runs ALTER TABLE ADD COLUMN.
- `delete_field(entity, field, plan_id)` — remove a field. Destructive: target `{op:"delete_field",name:"<entity>.<field>"}`.

### UI pages
- `add_page(page)` — register a page. Pages are element trees (`{kind, props, children, bindings, actions}`).
- `delete_page(path)` — remove a page.

### Behavior
- `add_hook(hook)` / `delete_hook(id)` — declarative entity lifecycle hooks.
- `add_route(route)` / `delete_route(method, path)` — declarative HTTP routes (e.g. `respond_json`).
- `add_seed(seed)` — insert seed rows.

### Plans, history, app config
- `propose_plan(plan_id, steps[], reason?, targets?)` — submit a plan for user approval. List destructive ops in `targets` (e.g. `[{op:"delete_entity",name:"posts"}]`) to authorize them.
- `approve_plan(plan_id)` — usually invoked by the panel when the user clicks Approve.
- `reject_plan(plan_id, reason?)` — invoked by the panel when the user clicks Reject. Rejected plans cannot later be approved.
- `undo()` — truncate the journal by one entry, reverting the most recent change.
- `set_app_config(config)` — name, json case (`camel`/`snake`), debug endpoints.
- `chat(role, text)` — record a message in the session journal.

## Field types

`string`, `text`, `int`, `float`, `decimal`, `bool`, `enum` (with `values: [...]`), `uuid`, `timestamp`, `date`, `json`, `relation` (with `to: "<entity_name>"`), `image`, `file`.

Each field also takes optional `required`, `unique`, `default`, `auto_generate` (`uuid`/`timestamp`/`increment`), `min`, `max`, `pattern`, `read_only`, `hidden`.

## Hook events

`before_create`, `after_create`, `before_update`, `after_update`, `before_delete`, `after_delete`, `before_list`, `after_list`.

A hook has an `id`, an `entity`, a `when`, an optional `condition` (expression), and an `action`.

## Action kinds

Actions describe what a hook or route does. Params are action-specific.

- `noop` — no params.
- `validate` — `{ expression: "<expr>", message: "<text>" }`. If the expression is false, the hook errors with the message.
- `set_field` — `{ field: "<name>", value: "<expr>" }` or `{ field, value_literal: <any> }`. Sets `entity[field]`.
- `audit` — `{ channel: "<name>", message: "<expr or text>" }`. Emits an audit record.
- `emit_event` — `{ topic: "<name>", data: "<expr>" }`. Emits a session event.
- `respond_json` — for routes only. `{ status: 200, body: <any> }` or `{ status, body: "<expr>" }`.

## Expression language

Used in hook conditions and action params (`expression`, `value`, `message`, `body` when string-typed).

- Literals: numbers, strings (`"x"` or `'x'`), `true`, `false`, `null`, lists.
- Operators: `+ - * / %`, `== != < > <= >=`, `&& || !`.
- Member access: `entity.title`, `ctx.user.role`, `result.id`.
- Built-in functions: `len(x)`, `lower(s)`, `upper(s)`, `contains(s, sub)`, `starts_with(s, p)`, `ends_with(s, p)`, `abs(n)`, `min(a, b)`, `max(a, b)`, `now()`.
- Scope at hook time: `entity` (the row), `ctx` (request — user, tenant), `result` (after_* hooks).

## Worked example: a blog

```bash
# 1. Add the posts entity (string title, text body, enum status)
curl -s -X POST "$KILN_URL/kiln/tool/add_entity" \
  -H 'Content-Type: application/json' \
  -d '{
    "entity": {
      "name": "posts",
      "soft_delete": true,
      "fields": [
        { "name": "title",  "type": "string", "required": true, "max": 200 },
        { "name": "body",   "type": "text" },
        { "name": "status", "type": "enum",   "values": ["draft", "published"], "default": "draft" }
      ],
      "mcp": true
    }
  }'

# 2. Auto-derive a slug from the title before insert
curl -s -X POST "$KILN_URL/kiln/tool/add_field" \
  -d '{"entity":"posts","field":{"name":"slug","type":"string","unique":true}}' \
  -H 'Content-Type: application/json'

curl -s -X POST "$KILN_URL/kiln/tool/add_hook" \
  -H 'Content-Type: application/json' \
  -d '{
    "hook": {
      "id": "posts_slug",
      "entity": "posts",
      "when": "before_create",
      "action": { "kind": "set_field", "params": { "field": "slug", "value": "lower(entity.title)" } }
    }
  }'

# 3. Add a custom health route
curl -s -X POST "$KILN_URL/kiln/tool/add_route" \
  -H 'Content-Type: application/json' \
  -d '{
    "route": {
      "method": "GET",
      "path": "/health",
      "action": { "kind": "respond_json", "params": { "status": 200, "body": { "ok": true } } }
    }
  }'

# 4. Verify
curl -s "$KILN_URL/posts" | head
curl -s "$KILN_URL/health"
```

## Element kinds (for pages)

`text`, `raw`, `div`, `section`, `header`, `footer`, `main`, `nav`, `aside`, `article`, `heading` (with `level: 1-6`), `paragraph`, `span`, `strong`, `em`, `code`, `pre`, `button`, `link`, `image`, `input`, `label`, `form`, `list` (use `ordered: true` for `<ol>`), `table`, `thead`, `tbody`, `tr`, `th`, `td`.

### Complete page-tree example (copy-paste shape)

When the user asks for a multi-section page, send ONE `add_page` call with the full nested tree. Don't send `add_page` with a placeholder tree and then describe what you "would" build — the page only renders what's in the IR. Below is a working three-section landing page with a nav bar — adapt the strings, keep the structure:

```bash
curl -s -X POST "$KILN_URL/kiln/tool/add_page" \
  -H 'Content-Type: application/json' \
  -d '{
  "page": {
    "path": "/",
    "title": "Home",
    "tree": {
      "kind": "div",
      "props": { "class": "landing" },
      "children": [
        {
          "kind": "nav",
          "props": { "class": "navbar", "aria-label": "Main" },
          "children": [
            { "kind": "heading", "props": { "level": 2, "text": "MyApp" } },
            { "kind": "div", "props": { "class": "nav-links" }, "children": [
              { "kind": "link", "props": { "href": "#about",   "text": "About" } },
              { "kind": "link", "props": { "href": "#contact", "text": "Contact" } }
            ]}
          ]
        },
        {
          "kind": "section",
          "props": { "id": "hero" },
          "children": [
            { "kind": "heading",   "props": { "level": 1, "text": "Build apps by talking to agents" } },
            { "kind": "paragraph", "children": [ { "kind": "text", "props": { "value": "Kiln journals every change live." } } ] },
            { "kind": "button",    "props": { "label": "Get Started" } }
          ]
        },
        {
          "kind": "section",
          "props": { "id": "about" },
          "children": [
            { "kind": "heading",   "props": { "level": 2, "text": "About" } },
            { "kind": "paragraph", "children": [ { "kind": "text", "props": { "value": "Three quick wins." } } ] }
          ]
        },
        {
          "kind": "section",
          "props": { "id": "contact" },
          "children": [
            { "kind": "heading",   "props": { "level": 2, "text": "Contact" } },
            { "kind": "form",      "props": { "method": "POST", "action": "/messages" }, "children": [
              { "kind": "label",   "props": { "for": "name", "text": "Name" } },
              { "kind": "input",   "props": { "id": "name", "name": "name", "type": "text", "required": true } },
              { "kind": "label",   "props": { "for": "msg",  "text": "Message" } },
              { "kind": "input",   "props": { "id": "msg",  "name": "message", "type": "text", "required": true } },
              { "kind": "button",  "props": { "type": "submit", "label": "Send" } }
            ]}
          ]
        }
      ]
    }
  }
}'
```

**Critical rules learned the hard way:**
- The `tree` value MUST have a non-empty `kind`. `{"kind": ""}` is rejected by the server.
- Don't send `add_page` and then describe what you "would" build — the page only renders what's literally in the IR.
- After every `add_page`, verify with `curl $KILN_URL/kiln/world` that your tree landed intact, then summarize for the user honestly. If the world doesn't match what you intended, fix it before saying "Done".
- **Page paths must not collide with entity CRUD paths.** Each entity claims `GET /<entity_name>` automatically. If you add an entity called `posts`, do NOT add a page at `/posts` — pick `/posts/index`, `/blog`, or `/posts-page` instead. The server returns a `conflict` error if you try; read the hint and rename. Same the other way: if a page exists at `/foo`, don't add an entity called `foo`.

### How to put text content

**Always include the actual text** — empty `{"kind":"text"}` nodes render nothing. Two equivalent shapes (use whichever feels cleaner):

A) Inline `text` / `label` prop on the element itself (preferred for short labels):

```json
{"kind":"heading","props":{"level":1,"text":"Welcome to Kiln"}}
{"kind":"link","props":{"href":"#contact","text":"Contact"}}
{"kind":"button","props":{"label":"Get Started","data-kiln-tool":"add_seed"}}
```

B) `text` child node with `props.value` set (for richer paragraph content):

```json
{"kind":"paragraph","children":[
  {"kind":"text","props":{"value":"Build a working app by talking to the agent."}}
]}
```

**WRONG** — `text` node without `value`, or any element with empty children:

```json
// produces <h1></h1>
{"kind":"heading","props":{"level":1},"children":[{"kind":"text"}]}

// produces <a href="#about"></a>
{"kind":"link","props":{"href":"#about"},"children":[{"kind":"text"}]}
```

When in doubt, use the inline `props.text` form. It's harder to leave empty by accident.

Every page rendered by Kiln automatically embeds the floating chat widget in the corner, so the user can talk to you from any page they're looking at. The widget posts the current page path back as `ctx.page` on each `chat` call — use that for in-page-context replies.

## When to freeze

If the user says "ship it" or wants real source files, suggest they run `kiln freeze` (out-of-band CLI) to emit `entities/*.json`. Kiln's HTTP API also exposes a snapshot at `GET $KILN_URL/kiln/world`.
