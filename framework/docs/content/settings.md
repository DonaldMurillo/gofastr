# Project settings

`gofastr.settings.yml` is the one committed file that configures the
`gofastr` tools for a project. It sits at the project root, beside `go.mod`,
and holds one top-level section per reader:

```yaml
version: 1

codegen:        # gofastr generate and gofastr build
  output: gen
  generators:
    - name: sdk
      output: sdk

contracts:      # gofastr verify
  performance: warn
  rules:
    GOFASTR1003:
      exempt: ["cmd/**"]

isolation:      # the running app and gofastr dev
  enabled: true
  mode: worktree

unmounted:      # written by gofastr unmount and gofastr remount
  - source: "github.com/DonaldMurillo/gofastr/battery/auth"
    version: "v0.92.0"
    path: "unmounted/battery/auth"
    date: "2026-10-19"
```

| Section | Read by | Reference |
| --- | --- | --- |
| `codegen` | `gofastr generate`, `gofastr build`, `gofastr generate --watch` | [Codegen](codegen.md) |
| `contracts` | `gofastr verify`, `gofastr build`, `gofastr dev`, the MCP contracts tools | [Contracts](contracts.md) |
| `isolation` | `framework/isolation`: `App.Start`, `gofastr dev`, generated `main.go` | [Worktree isolation](isolation.md) |
| `unmounted` | `gofastr unmount`, `gofastr remount`, `gofastr verify`, `gofastr upgrade`, `gofastr audit deps` | [Unmounting a package](unmount.md) |

Every section is optional. `gofastr init` writes the file with an
`isolation` section.

## How the file is found

Each reader looks for `gofastr.settings.yml` in the directory it starts
from and in each parent up to the project root: the first directory that
holds `go.work`, `go.mod` or `.git`. It never looks past that root. A
settings file can name a command that `gofastr generate` runs, so one
planted in a shared parent (a workspace directory, `/tmp`, `$HOME`) must
never apply to the project below it. With no root marker anywhere above
the start directory, only the start directory is searched.

`gofastr generate --config=<file>` and `gofastr verify --config <file>`
name a settings file directly instead.

## What is an error

- A top-level key other than `version`, `codegen`, `contracts`,
  `isolation` or `unmounted`.
- A `version` other than `1`.
- A file the settings file replaced, found where the reader looks for
  settings: `gofastr.codegen.yml`, `gofastr.contracts.yml` or
  `gofastr.isolation.yml` (or their `.yaml` spellings). The reader of that
  section refuses to run and names the section to move the content into,
  so a relaxation or a disabled setting never drops away unnoticed.

The app spec (`gofastr.yml`) describes the app, not the tools. A `codegen:`,
`contracts:` or `isolation:` key in it is an error that points here.

## The unmounted section

`gofastr unmount` and `gofastr remount` own this section and rewrite it
whole; the rest of the file keeps its bytes and comments. Each entry has
exactly four string fields:

- `source`: the import path the copy came from.
- `version`: the gofastr release copied, or `dev` for a copy of a local
  checkout. Letters, digits, `.`, `-` and `+` only.
- `path`: the copy's directory, slash-separated and relative to the
  settings file. It may not climb out with `..` or sit inside a `vendor`
  directory.
- `date`: the day of the copy, `YYYY-MM-DD`.

An unknown key, a missing field, a value outside those shapes or a path
listed twice is an error. One source may have several entries, one per
copy (`generate package` copies a chrome package once per binary).
Removing the last entry removes the section, and the file too when
nothing else is left in it. The section's key must be a plain
`unmounted:` at the start of a line, and the file a real file, not a
symlink: the commands refuse to rewrite anything else.

## Moving from the separate files

Each old file already had the section shape, so the move is a copy:

1. Copy the `codegen:` block of `gofastr.codegen.yml`, the `contracts:` block
   of `gofastr.contracts.yml` and the `isolation:` block of
   `gofastr.isolation.yml` into `gofastr.settings.yml`. A contracts file
   written without the `contracts:` wrapper needs one added and its body
   indented under it.
2. Move any `codegen:`, `contracts:` or `isolation:` section out of
   `gofastr.yml`.
3. Delete the old files.

`gofastr upgrade` reports each old file it finds.

## Common mistakes

- Putting `gofastr.settings.yml` in a subdirectory of a module and running
  the tool from the module root. The lookup walks up from the start
  directory, never down: keep the file beside `go.mod`.
- Copying a contracts file that had no `contracts:` wrapper (its keys,
  such as `rules:`, at the top level) straight into the settings file.
  Those keys are unknown sections there; indent them under `contracts:`.
- Keeping a `codegen:` or `isolation:` section in `gofastr.yml`. The app
  spec refuses it; move it into the settings file.
