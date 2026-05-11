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

Nested includes (`author.profile`) require **both** sides registered
with the framework's `Registry`. Top-level includes work as long as
the parent's relation declaration names a real table.

## Filter scope

Scoped filters use the same suffix operators as top-level entity
filters:

| Suffix   | Operator        |
|----------|-----------------|
| `_gt`    | `>`             |
| `_gte`   | `>=`            |
| `_lt`    | `<`             |
| `_lte`   | `<=`            |
| `_like`  | SQL `LIKE`      |
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

## Errors

- `unknown include "x"` — the named relation does not exist on the
  entity at that depth.
- `target entity "y" not registered (required for nested includes)`
  — a path of length > 1 hit an unregistered target.
- `scoped field "x" not on target entity` — the filter referenced a
  field that does not exist on the target's schema.

## Common mistakes

- **Forgetting parentheses for filters.** `comments(status=draft)` is
  scoped; `comments,status=draft` is two unrelated query parameters.
- **Filtering with the wrong field name.** Scoped filters validate
  against the target, not the parent. Use the target's column names.
- **Nesting through unregistered entities.** Register every entity in
  the registry; otherwise nested includes fail at parse time.
- **Expecting `?include=` to control SELECT projection.** It does
  not — use field projections separately. Includes only attach
  related data.
