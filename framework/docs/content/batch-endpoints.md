# Batch endpoints

Every CRUD-enabled entity mounts batch routes at `/<table>/_batch` so callers can create, update, or delete multiple rows in a single atomic transaction.

| Method & path | Action | Request body |
| --- | --- | --- |
| `POST /<table>/_batch` | Bulk create | `{"items": [ {...}, ... ]}` |
| `PATCH /<table>/_batch` | Bulk update | `{"items": [ {"id": ..., ...}, ... ]}` |
| `DELETE /<table>/_batch` | Bulk delete | `{"ids": [ ... ]}` |

## Transaction and validation semantics

- **All-or-nothing:** batch writes execute inside one database transaction. If any item fails validation, access control, a `Before*` hook, or a constraint check, the entire batch rolls back and no rows change.
- **Same security pipeline:** every item goes through the entity's `Exposure.Access`, per-row `Decider`, `Scope.OwnerField` (auto-stamped on create, scoped on update/delete), `Scope.MultiTenant`, `Scope.SoftDelete`, and hooks.
- **Wire names:** item payloads use the mount's configured JSON casing (`WireName` or default camelCase).
- **Read-only mounts:** entities registered via `App.View` or `CrudRouteOptions{ReadOnly: true}` do not mount `_batch` routes.

## Limits

Batch size is bounded to protect memory and lock duration (default max 100 items per request; requests exceeding the cap or sending an empty array return `400`). Larger imports should chunk client-side or run as a background job (see [Queue](queue.md)).

## Common mistakes

- **Expecting partial success:** a single invalid item rolls back the entire batch. Validate payloads or split independent items when partial progress is desired.
- **Sending a top-level JSON array instead of `{"items": [...]}` / `{"ids": [...]}`:** batch endpoints expect the envelope object.
- **Trying to set `OwnerField` or tenant columns in batch creates:** those columns are stamped from the authenticated caller just like single-row creates.
- **Sending thousands of rows in one request:** keep batches at or below the per-request cap and chunk larger imports.
