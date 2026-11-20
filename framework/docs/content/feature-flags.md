# Feature flags (`core/featureflag`)

`core/featureflag` is an in-process feature-flag evaluator supporting global kill switches, percentage rollouts, per-user and per-tenant allowlists, and environment restrictions.

## Quickstart

```go
import flag "github.com/DonaldMurillo/gofastr/core/featureflag"

store := flag.NewMemoryStore() // or flag.NewSQLStore(db) for durable/admin-editable flags
_ = store.Set(flag.Flag{Key: "new-checkout", Enabled: true, Rollout: 25})
app.SetFlagStore(store)
```

Evaluate inside a handler:

```go
ctx := flag.WithContext(r.Context(), flag.EvalContext{
	UserID:   sess.UserID,
	TenantID: tenant.ID,
	Env:      "production",
})

if app.IsEnabled(ctx, "new-checkout") { // or flag.Bool(ctx, "new-checkout")
	return newCheckout(w, r)
}
return oldCheckout(w, r)
```

Default middleware attaches the app's evaluator to every request context (`featureflag.WithEvaluator`). In background jobs or cron runs outside HTTP requests, pass `app.Flags()` explicitly rather than relying on `featureflag.Default()`.

## Flag rules and evaluation order

<!-- gofastr:compile
-->
```go
type Flag struct {
	Key     string
	Enabled bool     // global kill switch
	Rollout int      // 0..100; stable FNV-1a hash of (key, subject)
	Users   []string // allowlist: "tenant:user" (via flag.UserEntry) or bare id
	Tenants []string // allowlist of tenant ids
	Envs    []string // environment allowlist (empty = any)
}
```

Evaluation order:
1. `Enabled == false` → off.
2. `Envs` non-empty and `EvalContext.Env` not listed → off (outermost guard so staging flags never leak to prod testers).
3. User matches `Users` (`flag.UserEntry(tenantID, userID)` formats multi-tenant entries safely) → on.
4. Tenant matches `Tenants` (falls back to `handler.GetTenantID` when `EvalContext.TenantID` is empty) → on.
5. `Rollout` percentage of stable FNV-1a hash → on or off (anonymous empty subjects stay off for partial 1–99% rollouts).

## Stores and GDPR erasure

- `NewMemoryStore()` — in-process concurrent map.
- `NewSQLStore(db, opts...)` — SQLite/Postgres table (`feature_flags`), implements `MutableStore` for `battery/admin`, and registers automatically with `App.ExportData` and `App.EraseUserData` (scrubbing erased user IDs from `users` allowlists — see [Data export](data-export.md#registering-an-eraser)).

## Common mistakes

- **Using feature flags as access control:** flags are for rollouts, not security boundaries (FNV-1a is not adversary-resistant). Use `framework/access` for permissions.
- **Using `IsEnabled` as a kill switch without a safe missing-key default:** a typo or store outage returns `false`. Use `app.Flags().BoolDefault(ctx, key, safeDefault)` when the default must be fail-closed.
- **Evaluating flags inside tight loops:** hoist the boolean once per request.
- **Mixing multi-tenant and bare user IDs:** build `Users` entries with `flag.UserEntry(tenantID, userID)`.
