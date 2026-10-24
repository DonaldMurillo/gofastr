# API stability and upgrade policy

GoFastr is pre-v1. Public APIs change between minor releases, and each
change arrives with the tools to move an app across it. This policy
applies to exported APIs under `framework/`, `core/`, `core-ui/`, and
`battery/`, plus app spec keys and documented runtime attributes.

## Before v1.0.0

- Additive changes may ship in any minor release.
- A breaking change ships whole in one minor release. The old spelling
  is deleted in the same change and every caller in the repo moves to
  the new one. GoFastr keeps no deprecated aliases, fallback keys, or
  shims beside the new API, so there is one way to write each thing.
- Every breaking change carries a **BREAKING** entry in `CHANGELOG.md`
  naming the old and new spelling, and a note in the `gofastr upgrade`
  registry that finds the affected code in an app and says what to
  write instead. Run `gofastr upgrade` before bumping the version.
- Packages under a path containing `/experimental/` are exempt. Their
  docs must identify them as experimental and consumers should pin a
  version.

Security fixes follow the same rule. When the fix changes behavior an
app relies on, the release notes name the risk, the affected versions,
and the migration path.

## At and after v1.0.0

GoFastr follows semantic versioning. Breaking public API changes require
a new major version, with the same changelog entry and upgrade note.

## Maintainer checklist

For any breaking or migration-relevant change:

1. Replace the old API and migrate every caller in the same change.
2. Update the relevant embedded guide and add a **BREAKING** entry to
   `CHANGELOG.md`.
3. Add a note to the release's `internal/upgrade/releases/` file so
   `gofastr upgrade` finds the old spelling, with a test that the note
   fires on it and stays quiet on the new one.

## Common mistakes

- Treating "pre-v1" as permission for silent breakage: a change with no
  changelog entry or upgrade note is a bug.
- Keeping the old spelling working beside the new one "for a while".
  Two spellings double what every reader, test, and doc has to cover.
- An upgrade note whose find pattern matches nothing an app would
  actually write.
- Assuming experimental APIs carry the same promise as public APIs.
