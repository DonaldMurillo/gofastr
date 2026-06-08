# Codegen

GoFastr code generation is driven by a general YAML configuration surface.
Generators are project extensions that consume structured sources and emit any
generated code files under a safe output root. This is distinct from the
[blueprint](blueprints.md) pipeline (`gofastr generate --from=gofastr.yml`),
which generates the framework's own entity/screen Go from a `gofastr.yml`.

## Config discovery

`gofastr generate` resolves configuration in this order:

1. `--config=<path>` when supplied.
2. `gofastr.codegen.yml`
3. `gofastr.codegen.yaml`
4. `gofastr.yml` / `gofastr.yaml` only when the file has a top-level `codegen:`
   section.

With no codegen config and no `--from` blueprint, `gofastr generate` has
nothing to generate and exits with guidance.

CLI flags override config values where they overlap:

- `--out=<dir>` overrides `codegen.output`.
- `--clean` and `--no-clean` override `codegen.clean`.

`--from=<path>` selects blueprint mode instead. Blueprint generation is
separate from this general codegen config — see [Blueprints](blueprints.md).

## Config shape

```yaml
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

Each generator names an `extension`; the CLI does not ship built-in
in-process generators. Use optional `id` when running the same generator more
than once:

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

The old built-in `gofastr generate ts` / `gofastr generate typescript` command
has been removed from this surface. Projects that need TypeScript or frontend
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

## Common mistakes

- Using `--config=/path/to/gofastr.codegen.yml` and expecting sources to be
  relative to the shell directory. Config paths define the project root.
- Pointing `--out` at `.` or `..`. Codegen refuses to write or clean outside a
  project subdirectory.
- Returning undocumented fields from an extension response. The protocol is
  strict so typos fail instead of being ignored.
