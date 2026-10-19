# Unmounting a package

`gofastr unmount <package>` copies a gofastr package into your app so you
can change its code. It rewires your imports to the copy and records the
copy in `gofastr.settings.yml`. `gofastr remount <package>` undoes it.

```sh
gofastr unmount battery/auth          # copy it to unmounted/battery/auth
gofastr unmount                       # list the copies
gofastr remount battery/auth          # back to the framework's package
```

## When to unmount

Unmount when you need to change code inside a framework package and no
hook, config field or extension point gets you there. Try those first:
a copy is code you maintain from now on.

What you take on:

- Fixes to the framework's package stop reaching your app, security
  fixes included. `gofastr upgrade` never changes a copy. It prints the
  upstream diff between the copied version and the target so you can carry
  changes over by hand.
- The copy is yours to review. `gofastr verify` checks it like the rest of
  your code.

## What unmount does

1. Finds the gofastr module your `go.mod` requires (with `go list -m`) and
   reads the package from it, with the packages under it. Tests, `testdata`,
   `vendor`, hidden and `_`-prefixed directories are left out, except a
   directory a non-test file embeds from (`//go:embed testdata/axe.min.js`).
2. Checks the package can be copied:
   - it imports no gofastr `internal/` package from outside itself, since
     only gofastr may import those;
   - no package outside your module that your app's build compiles (tests
     included) imports it: another gofastr package, or a third-party one
     such as a plugin. The rewire reaches only your module's files, so a
     copy beside such an importer would put two versions of the same
     types in one binary. Unmount refuses and names each importer, for
     example `github.com/DonaldMurillo/gofastr/framework/ui imports
     github.com/DonaldMurillo/gofastr/framework/ui/theme`.
3. Writes the copy to `unmounted/<package>` at the module root, or to
   `--out=<dir>`. The target must be empty and inside the same module.
   The copy's imports of its own packages point at the copy.
4. Rewrites every import of the package in your module to the copy.
5. Adds an `unmounted:` entry to `gofastr.settings.yml` at the module
   root.

`--dry-run` prints the files it would write and the files it would rewire,
and changes nothing.

## How a copy is marked

Every Go file in the copy starts with a provenance marker naming its
source and version:

```go
//gofastr:generated generator=unmount kind=scaffold version=v0.92.0 source=github.com/DonaldMurillo/gofastr/battery/auth sum=53790688b1ff57b4
```

Each package of the copy also gets an `unmounted.gen.go`. Its `init()`
registers the copy with `core/unmounted`, so the running app knows which
copies it was built with. `App.Unmounted()` returns them, and the MCP
tool `app_unmounted` reports them to a connected agent. The root
package's `unmounted.gen.go` also lists every file of the copy with its
sum, which is how remount tells an edited copy from an untouched one.

A copy made from a local checkout (a `replace` to a directory) records
`dev` as its version.

## The settings entry

```yaml
unmounted:
  - source: "github.com/DonaldMurillo/gofastr/battery/auth"
    version: "v0.92.0"
    path: "unmounted/battery/auth"
    date: "2026-10-19"
```

`unmount` and `remount` write this section; leave it to them. The
[settings reference](settings.md) has the field rules.

The CLI reads it:

- `gofastr verify` lists each copy and warns when go.mod requires a newer
  gofastr release than the copy came from, with a link to the upstream
  diff. It also warns about an entry whose directory holds no files, and
  about a file marked as a copy of a package the section does not list.
- `gofastr upgrade` lists the copies with the upstream diff to the target
  release.
- `gofastr audit deps` lists them after the init-time registrations.

## Remount

`gofastr remount <package>` takes the import path, the part after
`github.com/DonaldMurillo/gofastr/`, or the copy's directory. It rewires
your imports back to the framework's package, removes the settings entry
and deletes the copy.

Remount refuses when the copy changed since unmount wrote it and names
each changed file: edited, added or deleted. `--force` deletes the copy
anyway, and your changes with it. It also refuses when the gofastr version
your go.mod requires no longer has the package. A copy you already
deleted holds nothing to lose: remount rewires the imports back and drops
the entry without `--force`.

## Chrome packages

`gofastr generate package <name>` copies a canonical chrome package
(`siteheader`, `sitefooter`, `docpage`) the same way. It marks the files,
writes the hook and records the entry, so verify, upgrade and remount
handle it like any other copy. It copies the package's tests as well, and
its default target is `./<name>`.

One package may have a copy per binary (`alpha/siteheader` and
`beta/siteheader`), each with its own entry. Remount one of them by its
directory: its import path names both, and remount refuses to guess.

## Common mistakes

- Unmounting a package to change one behaviour a hook or config field
  already covers. The copy stops getting fixes; the hook does not.
- Unmounting a package that other framework packages or a plugin use,
  such as `framework/ui/theme`. Unmount refuses: the importers would still use
  the framework's version. Unmount the package you import directly, or
  ask upstream for the extension point.
- Editing the `unmounted:` section by hand. A copy listed nowhere loses
  its upgrade warnings, and an entry with no copy makes verify warn. Use
  `gofastr remount` to drop a copy.
- Running `remount --force` to get past the edit check without reading the
  file list first. The listed changes are deleted.
- Forgetting that a rewire reaches other copies. Unmounting `battery/auth`
  after `battery/admin` rewires the admin copy's imports of auth as well,
  so remounting admin later reports those files as edited.
