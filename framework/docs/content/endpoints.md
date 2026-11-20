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
			Path:        "/{id}/regenerate",
			Description: "Rebuild the report snapshot from current data.",
			MCP:         true,
			Handler:     regenerateReportHandler,
		},
	},
})
```

Key behaviors:
- **Path mounting:** entity endpoints mount relative to the entity's route prefix (for example `/api/reports/{id}/regenerate`).
- **Independent of `Exposure.CRUD`:** setting `CRUD: false` disables only generated CRUD routes; declared `Endpoints` still mount, and an endpoint with `MCP: true` still registers its MCP tool regardless of `Exposure.MCP`.
- **Reserved EntityUI paths:** if you use `framework/entityui`, custom entity endpoints cannot claim `POST _bulk`, `GET _export.csv`, or `POST _pick` — registration rejects collisions in either order.

## Auth and `MCPGate`

HTTP requests to a custom endpoint run through the route's middleware chain (auth, roles, rate limits). **Its MCP twin does not automatically inherit arbitrary HTTP middleware** — if you enable `MCP: true`, protect the MCP invocation explicitly with `Endpoint.MCPGate` (and/or `Access` / role checks inside the handler) so agents cannot bypass HTTP route guards.

## MCP tool hints

When `MCP: true` is set on an endpoint, provide MCP annotations in `Hints` (`ReadOnly`, `Destructive`, `Idempotent`, `OpenWorld`) so agent clients know whether the action is safe to auto-run or requires confirmation:

```yaml
endpoints:
  - name: archive_old_reports
    method: POST
    path: /api/reports/archive-old
    mcp: true
    hints:
      destructive: true
      idempotent: true
```

In `gofastr.yml`, declaring top-level or entity `endpoints:` generates typed request/response structs and handler stubs you implement in owned Go. See [App specs](app-spec.md#custom-endpoints-endpoints).

## Common mistakes

- **Enabling `MCP: true` without setting `MCPGate` or handler-level auth:** HTTP route middleware does not automatically wrap direct MCP tool calls.
- **Assuming `CRUD: false` also disables custom `Endpoints`:** `CRUD: false` turns off only generated CRUD routes; remove or gate custom endpoints separately.
- **Colliding with EntityUI reserved paths:** `POST _bulk`, `GET _export.csv`, and `POST _pick` belong to `framework/entityui`.
- **Omitting `Hints` on destructive MCP endpoints:** mark mutating or irreversible tools `Destructive: true` so agent harnesses prompt before calling them.
