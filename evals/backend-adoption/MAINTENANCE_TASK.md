# Maintenance task: ticket priority

Evolve the working support desk without replacing its architecture.

Add a ticket priority with exactly these values:

- `low`
- `normal`
- `high`

Requirements:

- Existing databases migrate automatically without losing users or tickets.
- Existing tickets become `normal`.
- New tickets default to `normal` when priority is omitted.
- `POST /api/tickets` accepts `priority`; list, get, and patch responses include
  it.
- `PATCH /api/tickets/{id}` can change priority.
- Invalid priority returns 400 and does not modify the ticket.
- `tickets_create`, `tickets_get`, and `tickets_list` expose priority over MCP.
- The OpenAPI document describes priority.
- The server-rendered dashboard displays each ticket's priority.
- Existing authentication, owner isolation, cross-site request protection,
  and restart persistence must continue to work.
- Add or update automated tests, then run `gofmt`, `go test ./...`, and
  `go build ./...`.

