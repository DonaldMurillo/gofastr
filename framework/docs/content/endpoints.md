# Custom endpoints

CRUD routes cover standard record operations, and plain `app.Router()` handlers cover arbitrary HTTP routes. When you want a custom action that also appears in OpenAPI and optionally publishes an MCP tool, declare an `Endpoint` (either on an entity or in `gofastr.yml`).

## Declaring custom endpoints in Go

Attach `Endpoints` to an entity in `EntityConfig` (or register standalone routes directly on `app`):

```go
app.Entity("reports", framework.EntityConfig{
	Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
	},
	Endpoints: []entity.Endpoint{
		{
			Name:        "regenerate",
			Method:      "POST",
			Path:        "{id}/regenerate",
			Description: "Rebuild the report snapshot from current data.",
			MCP:         true,
			Handler:     regenerateReportHandler,
			MCPHandler:  regenerateReportMCPHandler,
		},
	},
})
```

Key behaviors:
- **Path mounting:** a relative `Path` (no leading `/`, such as `"{id}/regenerate"`) joins the entity's route prefix (e.g., `/api/reports/{id}/regenerate`). A `Path` starting with `/` mounts at that literal absolute path instead.
- **`MCPHandler` required when `MCP: true`:** enabling `MCP: true` without setting `MCPHandler` fails registration.
- **Independent of `Exposure.CRUD`:** setting `CRUD: false` disables only generated CRUD routes; declared `Endpoints` still mount, and an endpoint with `MCP: true` still registers its MCP tool regardless of `Exposure.MCP`.
- **Reserved EntityUI paths:** if you use `framework/entityui`, custom entity endpoints cannot claim `POST _bulk`, `GET _export.csv`, or `POST _pick` — registration rejects collisions in either order.

## Auth and `MCPGate`

Both the HTTP route and its MCP tool require a signed-in caller by default unless `Public: true` is set. The catch is that arbitrary HTTP route middleware (such as custom role or rate-limit wrappers) does **not** run on the MCP path—set `Endpoint.MCPGate` (or check permissions inside `MCPHandler`) whenever an endpoint needs authorization beyond being signed in.

## MCP tool hints

In Go, set `MCPAnnotations` (`*mcp.Annotations`) on `entity.Endpoint` so agent clients know whether the action is read-only, destructive, idempotent, or open-world. In `gofastr.settings.yml` (or a top-level `endpoints:` section in `gofastr.yml`), configure the same metadata under `hints:` with required `entity`, `handler`, and relative `path`:

```yaml
endpoints:
  - name: archive_old_reports
    entity: reports
    method: POST
    path: archive-old
    handler: entities/reports.go#ArchiveOldReports
    mcp: true
    hints:
      destructive: true
      idempotent: true
```

In `gofastr.yml`, declaring top-level `endpoints:` wires routes into `gofastr.settings.yml` and codegen. See [App specs](app-spec.md#custom-endpoints-endpoints).

## Common mistakes

- **Adding a leading `/` to an entity endpoint `Path`:** `Path: "/{id}/regenerate"` mounts at the root instead of `/api/reports/{id}/regenerate`. Omit the leading slash for entity-relative paths.
- **Setting `MCP: true` without `MCPHandler`:** registration rejects an MCP-enabled endpoint that has no `MCPHandler`.
- **Assuming HTTP route middleware protects the MCP tool:** both paths require login unless `Public: true`, but extra route middleware does not run on MCP calls—set `MCPGate` for role/permission checks.
- **Assuming `CRUD: false` also disables custom `Endpoints`:** `CRUD: false` turns off only generated CRUD routes; remove or gate custom endpoints separately.
- **Colliding with EntityUI reserved paths:** `POST _bulk`, `GET _export.csv`, and `POST _pick` belong to `framework/entityui`.
- **Omitting `MCPAnnotations` / `hints:` on destructive MCP endpoints:** mark mutating or irreversible tools destructive so agent harnesses prompt before calling them.
