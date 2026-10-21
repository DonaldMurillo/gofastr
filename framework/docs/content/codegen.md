# Codegen

Every generator runs as `gofastr generate <name>`. Generators come in two
kinds, and the kind decides who runs them and whether you may edit what they
write.

**Scaffold** generators write code into your repo once. From then on you own
it and edit it like any other source file. A second run refuses to overwrite
existing files unless you pass `--force`.

- `app [spec]`: the whole app from an [app spec](app-spec.md)
  (default `gofastr.yml`; a file or a directory). `--add` merges new
  entities and screens into an existing app.
- `entity <name>` and `screen <name>`: an editable stub in a generated app.
- `package [<name>]`: a copy of a canonical chrome package (siteheader,
  sitefooter, docpage), recorded as an [unmounted](unmount.md) copy. No
  name lists them.

**Derived** generators keep a type-safe layer in sync with a source. Their
files say DO NOT EDIT, and a rerun replaces them.

- `client`: `entities/client/client.go`, the app's typed Go client, from
  `entities/*.go`.
- `cli`: a customer-facing CLI for the app API. Its `*.gen.go` files are
  rebuilt; `custom.go` is created once and stays yours. See
  [Ship your API as a CLI](app-cli.md).
- `sdk`: Go and JavaScript/TypeScript API clients. See [SDK](sdk.md).
- `styles [patterns]`: typed Go for app tokens and owned style sheets.

Plain `gofastr generate` runs the codegen config below. `gofastr build`
and `gofastr generate --watch` call it, so the config lists derived
generators and project extensions only. A scaffold generator in the config
is an error: a build would re-run it on code you own.

`gofastr generate --help` prints the same list, grouped by kind.

## Config discovery

The codegen config is the `codegen:` section of
[`gofastr.settings.yml`](settings.md). `gofastr generate` reads it from:

1. `--config=<path>` when supplied: a settings file.
2. Otherwise `gofastr.settings.yml`, looked up from the current directory to
   the project root.

Whether or not a config is found, `gofastr generate` then rebuilds
`entities/client/client.go` when that file exists; the `client` generator
needs no config entry. With neither a config nor a client it has nothing to
generate and exits with guidance. To generate an app, run `gofastr generate
app gofastr.yml`.

CLI flags override config values where they overlap:

- `--out=<dir>` overrides `codegen.output`.
- `--clean` and `--no-clean` override `codegen.clean`.

Plain `gofastr generate` also accepts `--dry-run`, `--json` and `--watch`.
Any other argument is an error.

## Config shape

```yaml
# gofastr.settings.yml
version: 1
codegen:
  output: gen
  clean: true
  generators:
    - name: custom/reports
      extension: report-generator
      source:
        type: json_file
        path: reports.codegen.json
      output: reports
  extensions:
    - name: report-generator
      command: ["./tools/report-generator"]
```

`codegen.output` is the root for all generated paths. A generator's `output`
is a subdirectory under that root. Generated paths must be relative and cannot
contain parent traversal.

Each generator names an `extension`, with two built-in exceptions. An entry
named `sdk` (no `extension`) runs the built-in SDK generator, the same
output as `gofastr generate sdk`; see the [sdk](sdk.md) doc. An entry named
`styles` runs `gofastr generate styles` after the other generators. It
writes beside each sheet rather than under `codegen.output`, so it takes no
`output` or `source`; its only config key is `patterns`, a list of Go
package patterns (`./...` when absent). `--dry-run` skips it, and
`--dry-run --json` refuses a config that lists it.

An entry named `cli` runs `gofastr generate cli` after the engine. It writes
to its own `output` (default `cmd/<binary>`), not under `codegen.output`,
takes no `source`, and its `config` keys mirror the flags as strings:
`binary`, `api_prefix`, `only`, `exclude`, `verbs`, and `from_openapi` (a
file in the project, never a URL). `--json` refuses a config that lists it,
because the CLI run would print its own report into that document. See [Keeping it current](app-cli.md#keeping-it-current).

```yaml
generators:
  - name: sdk
  - name: cli
    config:
      binary: shop
  - name: styles
    config:
      patterns: ["./ui/..."]
```

Use optional `id` when running the same generator more than once:

```yaml
generators:
  - id: public-reports
    name: custom/reports
    extension: report-generator
    source:
      type: json_dir
      path: public/reports
    output: public/reports
```

Go embedders that link `github.com/DonaldMurillo/gofastr/codegen` directly can
also register in-process generators with `Registry.RegisterGenerator`.

## Sources

Supported source types:

- `json_dir`: reads every `*.json` file in a directory in sorted order.
- `json_file`: reads one JSON file.

Source paths are project-relative. Absolute paths and `..` traversal are
rejected. Future source types can be added without changing the generator and
extension protocol.

## Extensions

External extensions let projects add arbitrary code generation without linking
that code into the `gofastr` CLI.

A command extension is an arbitrary binary named by whichever
`gofastr.settings.yml` is in the project, so it runs with a deliberately narrow
contract:

- **The environment is an allowlist, not an inheritance.** The child gets what a
  build-time tool needs to run and nothing else: `PATH`, `HOME`, a temp dir,
  locale, and the Go toolchain variables. It does not see `GOFASTR_SECRET`,
  `DATABASE_URL`, or cloud credentials. Pass project-specific values under the
  extension's `config:` key, which arrives on stdin with the rest of the
  request; do not expect an ambient environment variable to reach it.
- **Output is scrubbed of terminal control bytes.** An extension's stderr is
  replayed to your terminal, so escape sequences are stripped from it (and from
  diagnostic messages) before they are written. Newlines and tabs survive.
- **Output is bounded and the wait is bounded.** stdout and stderr are capped,
  and an extension that stops producing output cannot hang the generate
  indefinitely.
- **A `severity: error` diagnostic fails the run.** Returning one is how an
  extension rejects the input it was given.

The old built-in `gofastr generate ts` / `gofastr generate typescript` command
has been removed. Projects that need TypeScript or frontend
artifacts should configure a project extension and decide their own output
shape rather than relying on a maintained built-in TypeScript target.

```yaml
version: 1
codegen:
  output: gen
  generators:
    - name: custom/reports
      extension: report-generator
      source:
        type: json_file
        path: reports.codegen.json
      output: reports
  extensions:
    - name: report-generator
      command: ["./tools/report-generator"]
      config:
        package: reports
```

The CLI invokes each extension phase with JSON on stdin:

```json
{
  "protocol_version": 1,
  "phase": "render",
  "project_dir": ".",
  "generator": {
    "name": "custom/reports",
    "extension": "report-generator",
    "source": { "type": "json_file", "path": "reports.codegen.json" },
    "output": "reports"
  },
  "extension": {
    "name": "report-generator",
    "command": ["./tools/report-generator"],
    "config": { "package": "reports" }
  },
  "source": { "name": "reports" },
  "files": []
}
```

The extension must print protocol JSON on stdout:

```json
{
  "protocol_version": 1,
  "diagnostics": [
    { "severity": "info", "message": "generated reports" }
  ],
  "files": [
    { "path": "report.go", "content": "package reports\n" }
  ],
  "deletes": ["stale_report.go"]
}
```

The CLI runs phases in this order: `load`, `validate`, `render`, `finalize`.
Extensions may return files or diagnostics from any phase. Stderr is forwarded
as extension log output. A non-zero exit, malformed response, unsafe generated
path, or file collision fails the generation run.

`files` and `deletes` are relative to the generator's `output` directory.
Deletes can only remove pending files owned by the same extension, so an
extension cannot silently remove another generator's output.

Response JSON is decoded strictly. Unknown response fields and unsupported
`protocol_version` values fail generation.

Go embedders can register in-process generators and extensions through
`github.com/DonaldMurillo/gofastr/codegen`.

## Cleaning and manifest

Configured codegen writes `.codegen-manifest.json` in the output root. Clean
mode removes only paths listed in that manifest before writing new output.
Unowned files are left alone.

The manifest also records a sum of every file it lists. Before a run
replaces a listed file whose content no longer matches its sum, it warns
that the file was edited and the edit is about to be lost. A clean run
checks every listed file; an unclean run checks only the files it writes
again.

## Provenance markers

The built-in generators stamp a marker at the top of each file they write
in a format with comments; the table below lists them.
It says which generator made the file, its kind, and a sum of the rest of
the file:

```go
//gofastr:generated generator=app kind=scaffold version=v0.92.0 sum=3f9a0c51d2e87b14

// Package main is the app.
package main
```

- `generator` is the `gofastr generate` subcommand that wrote the file,
  or `unmount` for a copy `gofastr unmount` or `gofastr generate package`
  made.
- `kind` is `scaffold` or `derived`, as above.
- `version` is the gofastr release that scaffolded the file, or `dev` for
  a CLI built from a checkout. Derived files carry no version: any CLI
  rebuilds them, and a version there would rewrite every derived file on
  each release with no change to its code.
- `source` appears on an unmounted copy only: the import path the copy
  came from. See [Unmounting a package](unmount.md).
- `sum` is the first 16 hex digits of the SHA-256 of the file without the
  marker line. It detects edits; it is not a signature.

The marker uses the file's own comment syntax and sits in its first eight
lines, below anything that has to come first:

| File | Marker |
|------|--------|
| `.go`, `go.mod`, `.js`, `.mjs`, `.cjs`, `.ts`, `.mts`, `.cts` | `//gofastr:generated …` and a blank line, so gofmt never folds it into a doc comment |
| `.css` | `/* gofastr:generated … */` |
| `.md`, `.html` | `<!-- gofastr:generated … -->`, after a doctype |
| `.yml`, `.yaml`, `.toml`, `.sh`, `Makefile`, `.gitignore`, `.dockerignore`, `.env*` | `# gofastr:generated …`, after a shebang |
| `Dockerfile` | `# gofastr:generated …`, after `# syntax=`, `# escape=` and `# check=` directives |
| `.sql` | `-- gofastr:generated …` |

JSON, `go.sum` and other files with no comment syntax carry no marker.
An owned style sheet's marker is left out of what `gofastr gen styles`
embeds and of its `Source hash`, so stamping a sheet does not make its
generated Go stale.

Each generator reads the markers before it writes:

- A scaffold generator that finds files in its way says what is there
  before it refuses: how many each generator and version wrote, how many
  have no marker, and how many marked files were edited since.
- `--force` names every target whose content no longer matches its sum.
  A marked file you never touched is not listed, even when today's
  template differs. See [App specs](app-spec.md).
- A derived generator that is about to rebuild an edited file warns that
  the edit is lost and names the source to change instead: the style
  sheet for `styles`, the entity declarations for `sdk`, `client` and
  `cli` (or the OpenAPI document a `cli --from-openapi` run read).

Editing a scaffold file is expected: the marker stays and its sum stops
matching, which only tells the next `--force` that the file holds your
work. Delete the marker line if you would rather the file carry no trace
of the generator.

`gofastr verify` counts the marked files by kind, generator and version,
and warns about each derived file edited since it was generated:

```
Generated files: derived styles: 4, scaffold app v0.92.0: 31; 6 scaffold file(s) edited since, which is expected: they are yours
```

## Common mistakes

- Using `--config=/path/to/gofastr.settings.yml` and expecting sources to be
  relative to the shell directory. Config paths define the project root.
- Pointing `--out` at `.` or `..`. Codegen refuses to write or clean outside a
  project subdirectory.
- Returning undocumented fields from an extension response. The protocol is
  strict so typos fail instead of being ignored.
