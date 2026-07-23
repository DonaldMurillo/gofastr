# API stability and deprecation policy

GoFastr is pre-v1, but public API changes still follow a predictable migration
path. This policy applies to exported APIs under `framework/`, `core/`,
`core-ui/`, and `battery/`, plus blueprint keys and documented runtime
attributes.

## Before v1.0.0

- Additive changes may ship in any minor release.
- A public API scheduled for removal is first marked deprecated in Go docs and
  called out in `CHANGELOG.md`, release notes, and the `gofastr upgrade`
  registry.
- The deprecated shape remains supported for at least one complete minor
  release. A deprecation introduced in v0.N cannot be removed before v0.(N+1).
- When practical, loaders and generators accept both shapes during that window
  and warn when they encounter the old form.
- Packages under a path containing `/experimental/` are exempt. Their docs must
  identify them as experimental and consumers should pin a version.

Security fixes may require an accelerated change when compatibility would leave
users exposed. The release notes must name the risk, affected versions, and
migration path.

## At and after v1.0.0

GoFastr follows semantic versioning. Breaking public API changes require a new
major version. Deprecations remain available through at least the next minor
release and include a concrete replacement.

## Maintainer checklist

For any breaking or migration-relevant change:

1. Add the replacement before deprecating the old API where possible.
2. Add a `Deprecated:` Go doc comment and compatibility test.
3. Update the relevant embedded guide and `CHANGELOG.md`.
4. Add a migration entry so `gofastr upgrade` can identify affected code.
5. Keep the old path green for the promised compatibility window.
6. Remove it only in an eligible release, with a test proving the old and new
   release boundary is represented by the upgrade guidance.

## Common mistakes

- Treating “pre-v1” as permission for silent breakage.
- Deprecating without naming the replacement or earliest removal release.
- Removing a compatibility path before `gofastr upgrade` can guide consumers.
- Assuming experimental APIs carry the same stability promise as public APIs.
