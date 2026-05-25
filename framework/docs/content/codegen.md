# Codegen

GoFastr code generation is driven by a general YAML configuration surface. Entity
JSON generation is one built-in generator, not the architecture boundary:
generators can consume different structured sources and emit any generated code
files under a safe output root.

## Config discovery

`gofastr generate` resolves configuration in this order:

1. `--config=<path>` when supplied.
2. `gofastr.codegen.yml`
3. `gofastr.codegen.yaml`
4. `gofastr.yml` / `gofastr.yaml` only when the file has a top-level `codegen:`
   section.
5. No config: legacy entity generation from `entities/*.json` into
   `.gofastr/entities`.

CLI flags override config values where they overlap:

- `--out=<dir>` overrides `codegen.output`.
- `--entities=<dir>` overrides `json_dir` sources used by built-in entity
  generators.
- `--clean` and `--no-clean` override `codegen.clean`.

`--from=<path>` remains blueprint mode. Blueprint generation is separate from
general codegen config, though blueprint entity output reuses the same Go
entity renderer where practical.

## Config shape

```yaml
version: 1
codegen:
  output: .gofastr
  clean: true
  generators:
    - name: go/entities
      source:
        type: json_dir
        path: entities
      output: entities

    - name: go/client
      source:
        type: json_dir
        path: entities
      output: entities/client
```

`codegen.output` is the root for all generated paths. A generator's `output`
is a subdirectory under that root. Generated paths must be relative and cannot
contain parent traversal.

Use optional `id` when running the same generator more than once:

```yaml
generators:
  - id: public-models
    name: go/entities
    source:
      type: json_dir
      path: public/entities
    output: public/entities
```

## Built-in generators

- `go/entities` reads an entity declaration directory and emits
  `register.go`, `models.go`, `columns.go`, `repo.go`, and `events.go`.
- `go/client` reads the same entity declaration format and emits a standalone
  Go HTTP client file.

Without config, `gofastr generate` uses both generators to preserve the
existing `.gofastr/entities` output.

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
  output: .gofastr
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

The legacy no-config entity path still performs its historical guarded clean of
`.gofastr/entities` and does not add a manifest file. Configured codegen writes
the manifest.

## Common mistakes

- Using `--config=/path/to/gofastr.codegen.yml` and expecting sources to be
  relative to the shell directory. Config paths define the project root.
- Pointing `--out` at `.` or `..`. Codegen refuses to write or clean outside a
  project subdirectory.
- Returning undocumented fields from an extension response. The protocol is
  strict so typos fail instead of being ignored.
