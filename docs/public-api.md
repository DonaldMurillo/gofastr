# Public API and support tiers

This page says which parts of GoFastr are safe to depend on and what changing
each part costs the maintainer. It is the human half of the classification in
`stability/stability.go`; the deprecation mechanics live in
[stability.md](../framework/docs/content/stability.md).

Every package in the module is assigned one tier. A test
(`TestEveryPackageIsClassified`) fails the build if a package has no tier, so a
newly added top-level tree cannot ship until someone classifies it on purpose.

## The tiers

| Tier | Promise | Depend on it if… |
|---|---|---|
| **Stable** | Frozen. After v1.0.0, a breaking change requires a new major version. | You want a fixed contract. **No package is Stable yet** — see below. |
| **Provisional** | Supported and documented. May change before v1.0.0, but only through the deprecation window in [stability.md](../framework/docs/content/stability.md): deprecate first, keep the old shape for at least one minor, then remove. | You are building on GoFastr today and can absorb a documented migration at a minor bump. |
| **Experimental** | May change or be removed with no deprecation window. | You accept that and pin an exact version. |
| **Internal** | Not a contract even though the symbols are exported. Moves freely. | Don't — import at your own risk. |
| **Excluded** | Not part of the shipped library (examples, benchmarks, evals, build output). | N/A. |

## Why nothing is Stable before v1.0.0

Marking a package Stable freezes it. That is the v1 decision, and it is made per
package in the release that promotes it — not by leaving a package unmarked. The
supported framework surface (`framework/`, `core/`, `core-ui/`, `battery/`,
`sqlite/`, the `gofastr` CLI) is **Provisional** until then: you can build on it,
and breaks come with a migration, but the contract is not yet frozen.
`TestNoStableBeforeV1` enforces this; it is deleted in the same change that marks
the first package Stable.

## Current classification

Read `stability/stability.go` for the exact rules (longest matching path prefix
wins). The seed:

- **Provisional** — `framework/` (except `framework/experimental/`), `core/`,
  `core-ui/`, `battery/`, `sqlite/`, `cmd/`.
- **Experimental** — `framework/experimental/*`, `kiln/`, `codegen/`.
- **Internal** — anything under an `internal/` directory, and the `stability`
  gate package.
- **Excluded** — `examples/`, `benchmarks/`, `evals/`, `dist/`.

## Changing a classification

Promoting a package (Provisional → Stable) or demoting one is an edit to the
manifest in `stability/stability.go`, recorded in the commit message and
`CHANGELOG.md`. Promotion to Stable is a freeze: do it only when the package's
exported surface is one you are willing to carry across a major version.
