# Factories / fixtures

`framework/factory` is a tiny Rails-style factory primitive for tests
and dev-time seeders. Each `Factory` binds to one entity and produces
fresh row bodies — typically by layering caller overrides on top of a
base function — so test setup reads "make me a user with admin=true"
instead of "construct a map with every required field by hand."

Factories dispatch through `crud.CrudHandler.CreateOne`, so they run
the entire production pipeline: `BeforeCreate` hooks → INSERT →
`AfterCreate` hooks → event emission, inside the same transaction the
HTTP path uses. Integration tests stay close to real-world behaviour
without rebuilding the wiring.

## Wiring

```go
import "github.com/DonaldMurillo/gofastr/framework/factory"

app := framework.NewApp(framework.WithDB(db)).Entity("users", entity.EntityConfig{...})

userSeq := &factory.Sequence{}
userFactory, err := factory.New(app.Registry, "users", func() map[string]any {
    n := userSeq.Next()
    return map[string]any{
        "email": fmt.Sprintf("user%d@example.com", n),
        "name":  fmt.Sprintf("User %d", n),
        "role":  "member",
    }
})
```

The `BaseFunc` is called for **every** `Create`/`Build`, returning a
fresh map each time. Sharing a map across calls would leak overrides
between tests; the package never holds a reference to it.

## Creating rows

```go
ctx := context.Background()

u, err := userFactory.Create(ctx)
// u["email"] = "user1@example.com" etc.

admin, err := userFactory.Create(ctx, map[string]any{"role": "admin"})
// base values + role override

// Many at once, with per-index variation:
batch, err := userFactory.CreateMany(ctx, 10, func(i int) map[string]any {
    return map[string]any{"score": i * 10}
})
```

`Build(overrides...)` returns the body that `Create` would insert —
useful when a test wants to assert on the request shape without
actually hitting the database. Multiple overrides apply left-to-right;
later overrides win on key conflict.

## Registry

For test suites with many factories, use the `Registry`:

```go
reg := factory.NewRegistry().
    Register("users", userFactory).
    Register("orders", orderFactory)

// Anywhere in your tests:
admin, _ := reg.Create(ctx, "users", map[string]any{"role": "admin"})
```

`MustGet(name)` panics on missing factories — tests are the only
caller, so a typo should fail loudly.

## Sequence helper

`Sequence` is an atomic counter for unique base values inside
`BaseFunc`. Concurrent-test safe — `Next()` never repeats.

```go
seq := &factory.Sequence{}

base := func() map[string]any {
    return map[string]any{
        "email": seq.NextString("user-") + "@example.com",
        "slug":  fmt.Sprintf("entry-%d", seq.Next()),
    }
}
```

## Common mistakes

- **Don't reuse a map across factories.** `BaseFunc` must return a
  fresh map each call; otherwise later overrides leak into earlier
  factory outputs.
- **Don't put `Sequence` values in a struct literal at package level.**
  Tests share package state — use one per `*testing.T` (or a global
  if you accept cross-test sequencing) but never bake a literal
  count into the base map.
- **Don't expect factories to clean up.** They INSERT only. Wrap
  your test in a transaction (and roll back in cleanup), or truncate
  the relevant tables in `t.Cleanup`.
- **Don't bypass the registry pattern just because it's easier.**
  Tests across files share fixtures; centralising the BaseFuncs in a
  Registry keeps your "valid user" definition in one place.
