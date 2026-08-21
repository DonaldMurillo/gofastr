# Includes & eager loading

The `?include=` query parameter eager-loads related entities in a
single response, avoiding N+1 queries. The framework runs one
follow-up query per relation per nesting level — never one per parent
row.

## Quickstart

```bash
# Single relation:
curl 'http://localhost:8080/posts?include=author'

# Multiple relations:
curl 'http://localhost:8080/posts?include=author,comments'

# Nested:
curl 'http://localhost:8080/posts?include=author.profile,comments.replies'

# Scoped — only published comments:
curl 'http://localhost:8080/posts?include=comments(status=published)'

# Scoped with operators:
curl 'http://localhost:8080/posts?include=comments(created_at_gte=2026-01-01,score_gt=5)'
```

The response embeds the loaded relations inline under each parent row:

```json
{
  "id": "p1", "title": "First",
  "author": { "id": "u1", "name": "Carol" },
  "comments": [
    { "id": "c1", "body": "…", "score": 12 }
  ]
}
```

## Path syntax

```
includes := path ("," path)*
path     := segment ("." segment)*
segment  := name [ "(" filter ("," filter)* ")" ]
filter   := field ("_gt"|"_gte"|"_lt"|"_lte"|"_like"|"_in")? "=" value
```

- Top-level commas separate sibling includes.
- Dots descend into nested relations on the previously-named target.
- Parentheses scope filters to the include's target entity.
- `field_in=a|b|c` becomes `field IN (a, b, c)` (pipe-separated).

## Supported relations

`include` understands every relation declared on the entity:

- `HasOne` / `BelongsTo` — attaches a single object (`null` if missing).
- `HasMany` — attaches an array (`[]` if empty).
- `ManyToMany` — attaches an array via the join table declared in the
  relation.

Every relation named in an `?include=` — nested or top-level — must
resolve to an entity registered with the framework's `Registry`. The
target's declaration is what drives the Hidden-column scrub, owner and
tenant scoping, the soft-delete filter, and the scoped-filter field
allow-list, so a target the registry cannot resolve is refused with
**400** rather than loaded without those guards.

`Relation.Entity` is the target's **entity name** (the registry key),
which is not necessarily its table name. An entity declared with
`Name` different from `Table` is reached by its `Name`; the eager-load
query targets its `Table`.

## Filter scope

Scoped filters use the same suffix operators as top-level entity
filters:

| Suffix   | Operator        |
|----------|-----------------|
| `_gt`    | `>`             |
| `_gte`   | `>=`            |
| `_lt`    | `<`             |
| `_lte`   | `<=`            |
| `_like`  | literal `contains` — `LIKE '%value%' ESCAPE '\'` with the caller's `%`/`_`/`\` escaped (matches the substring literally, not as a wildcard pattern; mirrors the DSL `contains` operator). **Identical at every depth**: top-level, `?rel.field_like=`, and `include=rel(field_like=…)` all mean literal substring. Nested filters used to pass the value through as a raw pattern, so the same parameter meant two different things depending on whether a dot appeared in it |
| `_in`    | `IN (...)` (pipe-separated values)  |

Filters validate against the **target** entity's fields, not the
parent's. `include=comments(post_id=x)` validates `post_id` on
`comments`, not on `posts`.

## Behaviour & guarantees

- Each unique relation runs one SQL query regardless of parent count.
  Loading `comments` for 50 posts is 1 query, not 50.
- Soft-deleted rows in the related entity are excluded (the include
  honours the target entity's `SoftDelete` config).
- Multi-tenant scoping is applied to includes — if both parent and
  child are tenant-scoped, the child query filters on the same tenant.
- Result key casing matches the entity's `JSONCase` setting
  (`camel` or `snake`); nested rows are deep-converted.
- Includes are bounded. A single path may take at most **4** relation
  hops, and one request's includes may materialise at most **20,000**
  related-row references in total. Either limit is a **400** — narrow
  the include or reduce the page size. Without them a short path over a
  self-referencing relation multiplies at every hop.
- Nullable foreign keys (optional relations) are supported: a parent
  whose FK column is `NULL` comes back with that relation **absent**
  (`null`), not an error. `BelongsTo`/`HasOne` relations over columns
  like `milestone_id` or `assignee_id` load normally for rows that
  reference a target and are simply omitted for rows that don't.

> **Low-level helper:** the HTTP `?include=` path scrubs soft-deleted
> rows and Hidden columns and scopes related rows to the ctx owner and
> tenant automatically. The exported `EagerLoad` helper
> (`framework.EagerLoad`) only does so when you pass the optional
> `entity.Registry` argument — `EagerLoad(ctx, db, ent, rels, ids, registry)`
> — which lets it resolve each relation's target to apply the
> `deleted_at IS NULL` filter, exclude Hidden fields, and AND in the
> owner (`OwnerField`) and tenant (`MultiTenant`) predicates from ctx,
> exactly as the include path does. With no user or tenant in ctx those
> predicates match nothing — fail closed. Always pass the registry when
> loading relations whose targets are soft-deletable, carry Hidden
> columns, are owner-scoped, or are multi-tenant; without it the helper
> returns unscrubbed, unscoped rows.

## Not supported with streaming

The streaming list path (`?stream=true`) skips include resolution to
keep memory bounded. Combining `?stream=true` with `?include=` is
refused with **400** rather than silently returning rows without their
relations. Drop one of the two. (When a list auto-streams because the
requested `limit` is very large, the framework instead falls back to
the buffered path so includes still resolve — only the explicit
`?stream=true` opt-in 400s.)

## Errors

- `unknown include "x"` — the named relation does not exist on the
  entity at that depth.
- `streaming list does not support include` — `?stream=true` was
  combined with `?include=`.
- `relation "y" targets entity "z", which is not registered` — a
  segment named a relation whose target is not in the registry.
- `include "x" requires an entity registry` — the handler has no
  `Registry` set, so no target can be resolved. Framework apps wire
  this automatically; a hand-built `crud.NewCrudHandler` must set
  `.Registry`.
- `include "x" is N relations deep; the maximum is 4` — the path
  exceeded the depth cap.
- `include exceeds the maximum number of related rows` — the assembled
  response would carry more than 20,000 related-row references.
- `scoped field "x" not on target entity` — the filter referenced a
  field that does not exist on the target's schema.

## Common mistakes

- **Forgetting parentheses for filters.** `comments(status=draft)` is
  scoped; `comments,status=draft` is two unrelated query parameters.
- **Filtering with the wrong field name.** Scoped filters validate
  against the target, not the parent. Use the target's column names.
- **Including through unregistered entities.** Register every entity
  the relations point at — including tables a battery self-migrates,
  such as the auth battery's user table. An unregistered target is
  refused, not loaded unscrubbed.
- **Expecting `?include=` to control SELECT projection.** It does
  not — use field projections separately. Includes only attach
  related data.


## Authorization

An include is a read of the RELATED entity's rows, so that entity's own posture
governs it — not the entity in the path.

`?include=rel` answers **403** when the caller may not read the target, naming
the entity that failed:

```json
{"code":403,"error":"access denied: include targets entity users, which you may not read","success":false}
```

The check runs at every depth, so `?include=comments.author` cannot reach a
gated `author` through a readable `comments`, and it runs before any rows are
loaded — including before the shortcut for an empty parent, so the answer never
depends on whether the parent table happens to have rows.

Owner and tenant scoping are applied differently: the eager loaders scope the
related rows per node, so an include of an owner-scoped entity returns the rows
the caller owns rather than a refusal. An anonymous caller owns nothing, so the
same include returns an EMPTY relation rather than an error.

The two predicates lift independently, each by its own grant. A caller marked
cross-owner, or holding the entity's `CrossOwnerRead` permission, sees every
owner's rows; a caller marked cross-tenant sees every tenant's. Neither lifts
the other, so a target carrying both scopes needs both grants to see every row.
Soft-deleted rows are hidden regardless.

This applies to requests arriving over HTTP. In-process callers — the
programmatic API, typed repos, seeds, jobs — are trusted server-side code and
skip these AUTHORIZATION checks, exactly as the baseline session requirement is
an HTTP concern.

What they do not skip is data-layer scoping. `EagerLoad(..., registry)` still
applies the soft-delete, owner and tenant predicates described above, so an
in-process eager load is scoped even though no posture was consulted. Only
`Exposure.Access` and the relation-disclosure refusal are HTTP-only.

## Filtering across a relation (`?rel.field=`)

`?author.email=someone@example.com` filters the parent by a field on a related
entity. It is subject to the same posture check, and to one additional rule.

Filtering across a relation whose target declares `Scope.OwnerField` or
`Scope.MultiTenant` narrows the subquery to your own rows. The filter compiles
to an `EXISTS` clause that counts rows without selecting them, so it cannot
narrow them by selecting; instead it carries the same owner and tenant
predicates the include and eager paths apply. The result is that
`?comments.body=x` counts exactly the comments you could list yourself:

```
GET /api/posts?comments.body=my+note      → your posts with that comment
GET /api/posts?comments.body=their+note   → 0 rows, identical to a value
                                            that exists nowhere
```

A guess that matches another owner's row is indistinguishable from a guess that
matches nothing, so the row count is not an oracle. A caller holding a
cross-owner or cross-tenant grant gets no predicate on that axis, because they
can already list the target wholesale — and the axes are independent, so a
cross-owner grant does not lift the tenant narrowing.

Earlier releases refused this shape outright for any scoped target, which closed
the oracle but also refused an owner filtering their own rows.

Soft-deleted rows never match a nested filter, matching every other read
surface.
