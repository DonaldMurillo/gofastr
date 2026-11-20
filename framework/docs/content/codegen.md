# Codegen

Every generator runs via `gofastr generate <name>` and falls into one of two categories:

- **Scaffold generators (one-shot, you own the files):** `app [spec]`, `entity <name>`, `screen <name>`, `package [<name>]`. Refuse to overwrite existing files without `--force` (`app --add` writes only new files).
- **Derived generators (regenerated from source, `DO NOT EDIT`):** `client`, `endpoints`, `cli` (rebuilds `*.gen.go`, leaves `custom.go` untouched), `sdk`, `styles [patterns]`.

Plain `gofastr generate` (also run by `gofastr build` and `gofastr generate --watch`) runs derived generators and project extensions configured in [`gofastr.settings.yml`](settings.md), plus automatically refreshes `entities/endpoints.gen.go` (when `endpoints:` is present) and `entities/client/client.go` (when it exists). Scaffold generators are refused inside `codegen:` so builds can never clobber owned code.

## Configuring `gofastr.settings.yml`

```yaml
version: 1
codegen:
  output: gen
  clean: true
  generators:
    - name: sdk
      output: sdk
    - name: cli
      output: cmd/myapp
      config:
        binary: myapp
        only: [orders, products]
    - name: styles
      config:
        patterns: ["./ui/..."]
```

Flags: `--config=<path>`, `--out=<dir>`, `--clean` / `--no-clean`, `--dry-run`, `--json` (one JSON document per run, or NDJSON stream with `--watch --json`).

## Custom extensions

Add project-specific generators without modifying the `gofastr` CLI by declaring an external command extension in `gofastr.settings.yml`:

```yaml
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
```

Security & protocol rules for command extensions:
- **Explicit opt-in:** auto-discovered settings files do not execute arbitrary commands automatically — pass `--config <path>` or set `GOFASTR_CODEGEN_ALLOW_COMMANDS=1`.
- **Allowlisted env:** child processes receive only `PATH`, `HOME`, temp dir, locale, and Go toolchain vars (never `GOFASTR_SECRET` or `DATABASE_URL`). Pass custom values via `extension.config`.
- **Strict JSON stdin/stdout protocol (`protocol_version: 1`):** runs phases `load` → `validate` → `render` → `finalize`; `severity: error` diagnostics fail generation; `clean: true` removes only files listed in `.codegen-manifest.json`.

## Provenance markers

Built-in generators stamp a single comment header at the top of each generated file (for formats supporting comments) recording the generator name, kind (`scaffold` or `derived`), version, optional `source` (for unmounted packages), and a 16-hex SHA-256 content sum:

```go
//gofastr:generated generator=app kind=scaffold version=v0.92.0 sum=3f9a0c51d2e87b14
```

How GoFastr uses markers:
- Scaffold `--force` warns about every marked file whose sum no longer matches (meaning you edited it).
- Derived generators warn before overwriting an edited derived file and point you to the source to change instead.
- `gofastr verify` summarizes generated files and flags edited derived files.
- Editing scaffold files is expected — delete the marker line if you prefer no trace.

## Common mistakes

- **Listing `app` or `client` inside `codegen.generators`:** `app` is a one-shot scaffold (refused in config) and `client` refreshes automatically when present.
- **Pointing `--out` at `.` or `..`:** codegen requires a project subdirectory so clean mode never touches the module root.
- **Expecting ambient env secrets inside a command extension:** extensions run under a minimal allowlisted environment; pass configuration via `extension.config`.
- **Assuming `--config=/other/dir/gofastr.settings.yml` resolves paths from your shell cwd:** paths resolve relative to the settings file's project root.
