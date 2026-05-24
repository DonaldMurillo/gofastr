# Feature flags

`core/featureflag` is GoFastr's feature-flag primitive — small enough
to stay in-process, expressive enough to gate the cases that come up in
real shipping: global kill switches, percentage rollouts, per-user
overrides for beta testers, per-tenant enablement for design partners,
and per-environment restriction so a staging-only flag never reaches
production.

The package is named `featureflag` to avoid colliding with the standard
library's `flag` package; users who want a shorter local name can alias
on import.

For anything richer (multi-arm experiments, scheduled ramps, dependent
flags) wrap the supplied `Evaluator` rather than extending this
package.

## Wiring

```go
import flag "github.com/DonaldMurillo/gofastr/core/featureflag"

app := framework.NewApp(framework.WithDB(db))

// Default backing store is in-memory; call SetFlagStore for redis/db.
store := flag.NewMemoryStore()
_ = store.Set(flag.Flag{Key: "new-checkout", Enabled: true, Rollout: 25})
app.SetFlagStore(store)
```

The first call to `app.Flags()` (or `app.IsEnabled`, or
`app.SetFlagStore`) lazily wires the evaluator and also installs it as
`featureflag.Default()` so package-level helpers work from anywhere.

## Using a flag

Inside a handler:

```go
ctx := flag.WithContext(r.Context(), flag.EvalContext{
    UserID:   sess.UserID,
    TenantID: tenant.ID,
    Env:      "production",
})

if app.IsEnabled(ctx, "new-checkout") {
    return newCheckout(w, r)
}
return oldCheckout(w, r)
```

Or, equivalently, the package-level form:

```go
if flag.Bool(ctx, "new-checkout") { ... }
```

Both consult the same evaluator.

## Flag rules

```go
type Flag struct {
    Key     string
    Enabled bool      // global kill switch
    Rollout int       // 0..100; stable hash of (key, subject)
    Users   []string  // explicit allow list
    Tenants []string  // explicit allow list
    Envs    []string  // restrict to deployment environments (empty = any)
}
```

Evaluation order:

1. `Enabled` false → always off.
2. `Envs` non-empty AND `EvalContext.Env` not in the list → off.
3. `EvalContext.UserID` matches `Users` (non-empty) → on.
4. `EvalContext.TenantID` matches `Tenants` (non-empty) → on.
5. `Rollout` percentage of the stable hash → on or off.

The hash is FNV-1a over `key + 0x00 + subjectID`. Subject id is the
user id when present, otherwise the tenant id, otherwise empty (which
hashes to a single deterministic bucket per flag key — anonymous traffic
is therefore binary on-or-off per key at any rollout < 100%; choose
allow-list gating for anonymous-sensitive flags).

The `Envs` filter is the **outermost** rule: even a user that appears
in the `Users` allow list does not see the flag if their request's
`Env` is not in `Envs`. This keeps a staging-only feature from leaking
into production for testers whose user-id sits in both environments'
identity provider.

## Stores

`featureflag.Store` is the read interface, `featureflag.MutableStore`
adds writes. The bundled `MemoryStore` implements both, exposes `All()`
for admin listings, and is safe for concurrent use.

Two stores are bundled:

- `NewMemoryStore()` — in-process map.
- `NewSQLStore(db, opts...)` — SQL-backed (sqlite + postgres),
  creates `feature_flags` on first use. Pass `WithSQLTable(...)` to
  override the table name, or `WithSQLDialect("postgres"|"sqlite")` to
  pin the dialect when the runtime probe is unreliable. Also implements
  `MutableStore` and `All(ctx)` for admin tooling.

For Redis or other backends, wrap `featureflag.Store` — only `Get` is
required; add `MutableStore` for admin-edit support.

## Common mistakes

- **Don't read flags inside hot loops without caching the bool.** The
  evaluator is fast but every call hits the store. Hoist the decision.
- **Don't rely on flags for security gating.** Flags are for *rolling
  out features*, not for access control. Use `framework/access` for
  permissions. The rollout hash is FNV-1a and is not adversary-resistant;
  an attacker who can choose their subject id can grind into a 1%
  cohort with ~100 tries.
- **Don't share an EvalContext across requests.** It's per-call; build
  it from the request's authenticated subject.
- **Don't bake user ids into the flag definition long-term.** The
  allow lists are great for beta testers; they're not how you maintain
  a permanent VIP segment.
- **Don't treat a missing key as "off" for a kill switch.** A typo or
  accidental delete returns `false` — for `if !IsEnabled(...) { dangerous() }`
  you'd silently enable the dangerous path. Use the explicit
  `Flags().BoolDefault(ctx, key, defaultIfMissing)` form to set the
  safe default per call site.
