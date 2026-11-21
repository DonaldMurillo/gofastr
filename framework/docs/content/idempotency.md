# Idempotency keys (`core/middleware`)

`framework.WithIdempotency` adds `Idempotency-Key` protection to unsafe HTTP writes (`POST`, `PUT`, `PATCH`, `DELETE`) so clients retrying over flaky networks receive the original `2xx` response instead of duplicating orders or charges.

## Quickstart

<!-- gofastr:compile
stmt: _ = app
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/core/middleware"
-->
```go
app := framework.NewApp(framework.WithIdempotency(middleware.IdempotencyConfig{}))
```

`WithIdempotency` mounts innermost via `router.UseInner`, after auth, tenant resolution, and route-group middleware, so it automatically keys by `(principal, tenant, held scopes, Idempotency-Key)`:
- **Principal:** resolved user ID (falling back to hashed credential headers, or `"anon"`). When mounting `middleware.Idempotency` manually without `WithIdempotency`, **always set `Principal`** — an unset `Principal` disables replay caching to prevent cross-user response leaks.
- **Tenant & scopes:** changing active tenant or calling with a narrower scoped API token never replays a response from another tenant or broader token.
- **Credential headers stripped:** `Set-Cookie`, `Cookie`, `Authorization`, `Proxy-Authorization`, and `WWW-Authenticate` are never stored or replayed.

## Request/response semantics

| Situation | Outcome |
| --- | --- |
| `GET` / `HEAD` / `OPTIONS` or missing header (`Required: false`) | Pass through, uncached |
| Missing header (`Required: true`), >255 chars, or non-printable ASCII | `400 Bad Request` |
| Duplicate key + same request fingerprint (cached `2xx`) | Replayed with `Idempotent-Replay: true` |
| Duplicate key + different method/path/query/body | `422 Unprocessable Entity` |
| Duplicate key while first request is still running | `409 Conflict` + `Retry-After: 1` |
| Non-`2xx`, panic, or cancelled without writing | Claim released; retry runs handler |
| Body > `MaxBodyBytes` (default 1 MiB) | Pass through with `Idempotent-Bypass: body-too-large` |
| Store failure (`FailOpen: false`, default) | `503 Service Unavailable` (fail closed) |

## Stores (single-node vs. multi-replica)

- `NewMemoryIdempotencyStore(ttl)` — default bounded single-process store (100,000 entries, 1 MiB max per cached response, 24h TTL). Warns if paired with `WithFanout`.
- `NewSQLIdempotencyStore(db, opts...)` — SQLite/Postgres (`idempotency_keys`), safe across replicas using atomic `INSERT ... ON CONFLICT DO NOTHING`, and integrated with `App.ExportData` / `App.EraseUserData`. Because its default in-flight claim TTL is `30s` — equal to the framework's default `30s` `RequestTimeout` — **always raise the in-flight TTL strictly above `RequestTimeout`** (or boot fails):

```go
store, err := middleware.NewSQLIdempotencyStore(db,
	middleware.WithSQLIdempotencyInFlightTTL(45*time.Second), // strictly > 30s RequestTimeout
)
if err != nil {
	return err
}
app := framework.NewApp(
	framework.WithDB(db),
	framework.WithIdempotency(middleware.IdempotencyConfig{Store: store}),
)
```

- **In-flight TTL vs. request timeout:** `InFlightTTL` must be strictly longer than your `RequestTimeout` (GoFastr enforces this at boot for any store implementing `middleware.InFlightTTLStore`, and logs a warning if a custom store omits that interface). Custom `IdempotencyStore` implementations must scope `Finish` updates/deletes by both `key` **and** `fingerprint`.

## Common mistakes

- **Using the default memory store across multiple replicas:** retries hitting another replica execute twice. Use `NewSQLIdempotencyStore` with `WithSQLIdempotencyInFlightTTL`.
- **Calling `NewSQLIdempotencyStore(db)` with default 30s `RequestTimeout`:** both default to 30s, so the boot check refuses startup until you pass `WithSQLIdempotencyInFlightTTL` > 30s (or lower `RequestTimeout`).
- **Mounting `middleware.Idempotency` manually without `Principal`:** silently degrades to pass-through with a warning to avoid cross-user leaks.
- **Ignoring `fingerprint` in a custom `IdempotencyStore.Finish`:** a late finish after claim expiry can overwrite another caller's row.
- **Setting `Required: true` globally instead of on payment/order groups:** breaks non-idempotent POST endpoints like login or form submissions.
- **Switching to `FailOpen: true` casually:** lets duplicate writes through during store outages.
