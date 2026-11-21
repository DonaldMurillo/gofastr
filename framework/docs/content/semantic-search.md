# Semantic search (`battery/semantic`)

`battery/semantic` adds vector/embedding-based semantic search to your app and CLI — chunking documents, calling a pluggable embedding provider, storing vectors in SQLite or PostgreSQL, and serving authenticated search routes.

## Quickstart

```go
import (
	"os"

	"github.com/DonaldMurillo/gofastr/battery/semantic"
)

app.RegisterPlugin(semantic.NewPlugin(idx).
	WithPrefix("/semantic").
	WithAuthToken(os.Getenv("SEMANTIC_TOKEN")))
```

Or mount the bare `http.Handler` directly onto a `core/router.Router` or stdlib `http.ServeMux`:

```go
mux.Handle("/semantic/", http.StripPrefix("/semantic",
	semantic.Handler(idx, semantic.WithAuthToken(os.Getenv("SEMANTIC_TOKEN")))))
```

## HTTP routes and authentication

**Every HTTP route requires a bearer token.** The handler fails closed: if no token is configured, every request returns `401 Unauthorized` so an unconfigured mount can never expose your index.

- Configure a token with `WithAuthToken` (or `Plugin.WithAuthToken`) and send `Authorization: Bearer <token>` (compared in constant time).
- `WithInsecureDisabledAuth()` disables auth for local development only — never use it on an exposed listener.

## CLI integration

Run local indexing or query a remote server through `gofastr semantic`:
- `index`, `watch`, and `clear` operate locally so concurrent writers do not fight over a local index file.
- When `GOFASTR_URL` is set, `query` and `stats` talk to the server's `/semantic/*` endpoints using `GOFASTR_SEMANTIC_TOKEN` as `Authorization: Bearer <token>`. Redirects are refused so the bearer token is never forwarded to another host.

## Metadata filtering (`Filter.MetaMatch`)

Scope semantic search results to a tenant, user, or document type via `Filter.MetaMatch` (`filter.meta_match` over JSON):
- Every key in `MetaMatch` must exist in the chunk's metadata with an equal JSON value (`5` matches `5.0`).
- Always enforce caller tenant/owner filters server-side before returning hits from multi-user indexes.

## Common mistakes

- **Mounting the semantic plugin without `WithAuthToken`:** every request fails closed with `401`. Always pass a secret token from the environment.
- **Using `WithInsecureDisabledAuth()` in production:** exposes the full index and stats to anyone who can reach the server.
- **Pointing `GOFASTR_URL` at a redirecting hostname:** the CLI refuses redirects on bearer-authenticated requests. Point directly at the canonical origin.
- **Skipping metadata scoping in multi-tenant apps:** vector similarity searches the whole index unless filtered by `Filter.MetaMatch`.
