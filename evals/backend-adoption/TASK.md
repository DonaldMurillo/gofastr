# Cold-start task: support desk

Build a production-minded, server-rendered support desk in this repository.
Use the framework named in `FRAMEWORK.md`; do not replace it with a different
web framework. The app must run without external services or API keys.

## Startup contract

The evaluator starts the app with:

```sh
PORT=127.0.0.1:<ephemeral-port> DATABASE_PATH=<absolute-sqlite-path> go run .
```

Honor both environment variables. Create or migrate the SQLite database
automatically. Data must survive a process restart.

## HTTP contract

All errors are JSON with a useful `error` string.

- `GET /healthz` returns 200.
- `POST /auth/register`, form encoded with `email` and `password`, creates a
  user, signs them in, returns 201, and sets a session cookie.
- `POST /auth/login`, using the same fields, signs an existing user in and
  returns 200.
- The session cookie must be `HttpOnly` and `SameSite=Lax` or stricter.
- `GET /api/tickets` returns the signed-in user's tickets as a JSON array.
- `POST /api/tickets` accepts JSON `{"title":"...","body":"..."}` and returns
  the created ticket with a stable string `id`, `status` equal to `open`, and
  status 201.
- `GET /api/tickets/{id}` returns the ticket.
- `PATCH /api/tickets/{id}` accepts JSON `{"status":"closed"}` and returns the
  updated ticket.
- Anonymous API access returns 401.
- A different signed-in user must receive 404 when reading or changing a
  ticket they do not own, and must never see it in their list.
- Cookie-authenticated state changes with an untrusted cross-site `Origin`
  must return 403.

## Human and machine discovery

- `GET /` is a useful server-rendered dashboard. Signed-in users can see their
  own ticket titles there.
- `GET /openapi.json` returns an OpenAPI document describing the ticket API.
- `POST /mcp` implements JSON-RPC MCP `tools/list` and `tools/call`.
- The MCP tools include `tickets_list`, `tickets_get`, and `tickets_create`.
- MCP uses the same session cookie and owner authorization as HTTP. An
  anonymous call must fail without returning ticket data; one user must never
  receive another user's ticket through MCP.

For MCP, accept standard requests shaped like:

```json
{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}
```

and:

```json
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"tickets_list","arguments":{}}}
```

## Engineering expectations

- Store password verifiers, never plaintext passwords.
- Validate required input and use parameterized SQL.
- Add focused automated tests for critical authorization behavior.
- Keep the implementation understandable to a Go maintainer.
- Run `gofmt`, `go test ./...`, and `go build ./...` before finishing.

