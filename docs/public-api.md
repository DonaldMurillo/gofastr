# Public API and support tiers

This page says which parts of GoFastr are safe to depend on and what changing
each part costs the maintainer. It is the human half of the classification in
`stability/stability.go`; the upgrade mechanics live in
[stability.md](../framework/docs/content/stability.md).

Every package in the module is assigned one tier. A test
(`TestEveryPackageIsClassified`) fails the build if a package has no tier, so a
newly added top-level tree cannot ship until someone classifies it on purpose.

## The tiers

| Tier | Promise | Depend on it if… |
|---|---|---|
| **Stable** | Frozen. After v1.0.0, a breaking change requires a new major version. | You want a fixed contract. **No package is Stable yet**: see below. |
| **Provisional** | Supported and documented. May change in a minor release before v1.0.0, and every break ships with a **BREAKING** changelog entry and a `gofastr upgrade` note that finds the affected code ([stability.md](../framework/docs/content/stability.md)). | You are building on GoFastr today and can absorb a documented migration at a minor bump. |
| **Experimental** | May change or be removed with no changelog entry or upgrade note. | You accept that and pin an exact version. |
| **Internal** | Not a contract even though the symbols are exported. Moves freely. | Don't. Import at your own risk. |
| **Excluded** | Not part of the shipped library (examples, benchmarks, evals, build output). | N/A. |

## Why nothing is Stable before v1.0.0

Marking a package Stable freezes it. That is the v1 decision, and it is made per
package in the release that promotes it, not by leaving a package unmarked. The
supported framework surface (`framework/`, `core/`, `core-ui/`, `battery/`,
`sqlite/`, the `gofastr` CLI) is **Provisional** until then: you can build on it,
and breaks come with a migration, but the contract is not yet frozen.
`TestNoStableBeforeV1` enforces this; it is deleted in the same change that marks
the first package Stable.

## Current classification

Read `stability/stability.go` for the exact rules (longest matching path prefix
wins). The seed:

- **Provisional**: `framework/` (except `framework/experimental/`), `core/`,
  `core-ui/`, `battery/` (except `battery/desktop` and `battery/service`),
  `sqlite/`, `cmd/`.
- **Experimental**: `framework/experimental/*`, `kiln/`, `codegen/`,
  `battery/desktop`, `battery/service`.
- **Internal**: anything under an `internal/` directory, the `stability`
  gate package, and `assets/` (the project's own brand files).
- **Excluded**: `examples/`, `benchmarks/`, `evals/`, `dist/`.

## Changing a classification

Promoting a package (Provisional → Stable) or demoting one is an edit to the
manifest in `stability/stability.go`, recorded in the commit message and
`CHANGELOG.md`. Promotion to Stable is a freeze: do it only when the package's
exported surface is one you are willing to carry across a major version.

## Presentation-host seams

[Presentation-host extension seams](proposals/presentation-host-seams.md)
proposed two seams for external presentation packages. Both are now in the
tree, in tiers the list above already assigns:

- **Application hosts** (`framework/`, Provisional): `App.Run(ctx, host)`, the
  `framework.Host` interface, the opaque `framework.Session`,
  `framework.ServicesHost()`, `App.ServicesReady()` and
  `framework.ErrAlreadyStarted`. `Start` and `Serve` are `Run` with the
  built-in HTTP host. See
  [Application hosts](../framework/docs/content/application-hosts.md).
- **Navigation guards** (`core-ui/runtime`, Provisional): the `navguard`
  module's `__gofastr.guardNavigation`, and core's `__gofastr.replaceURL`
  for rewriting the current entry's URL without departing. Both are for
  renderer libraries, not application code: an app authors its screens
  in Go and writes no JavaScript to use them. Where the browser has no
  Navigation API, the guard undoes and replays a Back or Forward by the
  distance between the tags core writes on every history entry, and with
  no known distance it pushes no entry. See the
  [runtime contract](../framework/docs/content/runtime-contract.md).

Underscore-prefixed runtime properties such as `_pushURL` and
`_interceptPopstate` are internal hooks, not a contract in any tier. The
proposal tracks what is still open: acceptance of the guard on WebKit
(its browser tests run Chromium only, with and without the Navigation
API) and the consumer's migration.
