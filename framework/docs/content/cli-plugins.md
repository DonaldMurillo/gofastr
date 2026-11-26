# Extending gofastr with plugins

`gofastr` is extensible through external CLI executables registered under a
namespaced command (`gofastr <plugin> ...`) and backed by the public Go seam
package `core/cliplugin`. Companion modules such as `gofastr-native` stay in
their own repositories while presenting one CLI to the developer:

```bash
gofastr native doctor
gofastr native build --platform ios-device --pkg ./app
gofastr native run --platform android-emu --pkg ./app
```

CLI plugins are distinct from [codegen extensions](codegen.md): codegen
extensions speak a non-interactive file-generation JSON protocol over stdin and
stdout, whereas CLI plugins stream interactive terminal I/O, forward OS
interrupts, and preserve exact exit codes.

## Registering a global plugin

Install a plugin binary that supports `core/cliplugin` protocol v1 (for
`gofastr-native`, use a release containing the protocol v1 adapter) and register
it once in your user registry (`~/.config/gofastr/plugins.yml`, honoring
`XDG_CONFIG_HOME` and `GOFASTR_PLUGIN_CONFIG`):

```bash
go install github.com/DonaldMurillo/gofastr-native/cmd/gofastr-native@latest
gofastr plugin add native gofastr-native
```

Registration probes the binary with `--gofastr-plugin-manifest`, checks the
protocol version (`1`) and GoFastr CLI version bounds, refuses any name that
collides with a built-in command or alias, and atomically writes an owner-only
(`0600`) record to `plugins.yml` under an exclusive file lock. `LoadUserConfig`
and `UpdateUserConfig` refuse symlinks and group/world-writable registry files
or directories.

Manage installed plugins with:

```bash
gofastr plugin list [--json]
gofastr plugin doctor [name] [--json]
gofastr plugin remove <name>
```

Without a plugin registered, the core `gofastr` CLI works unchanged. Invoking
`gofastr native ...` before registering prints install and registration
instructions instead of failing silently.

## Project-scoped plugins in `gofastr.settings.yml`

A repository can declare expected global plugins or repo-local executables
under the `plugins:` section of [`gofastr.settings.yml`](settings.md):

```yaml
version: 1
plugins:
  # Declaration-only: documents an expected global plugin and its install hint.
  - name: native
    description: "Build and run iOS/Android apps"
    install: "go install github.com/DonaldMurillo/gofastr-native/cmd/gofastr-native@latest"

  # Repo-local binary: never executes silently until explicitly trusted.
  - name: release-helper
    description: "Repository release checks"
    command: ["./tools/release-helper"]
    allow_env: ["RELEASE_CHANNEL"]
    hooks: ["pre:release"]
```

Repo-declared commands are **fail-closed**:

- A project `command` must be a **single direct executable file path** inside
  the project root (no bare `PATH` launchers such as `sh` or `go`, no extra
  mutable script arguments, and no traversal or symlink escape outside the
  project root) so the SHA-256 trust pin covers the exact executable artifact.
- GoFastr refuses to execute a repo-declared command until you inspect and
  approve it with `gofastr plugin trust <name>`.
- Every capability requested in `gofastr.settings.yml` (`allow_env`,
  `allow_env_flags`, `hooks`, `overrides`) must be passed explicitly on the
  `gofastr plugin trust` command line (`--allow-env`, `--allow-env-flag`,
  `--allow-hook`, `--allow-override`) before GoFastr probes or trusts the
  binary.
- Trust records live in your user registry (`plugins.yml`), pinned to the
  canonical project directory, resolved command path, executable SHA-256
  digest, and approved grants.
- Before probing or running a trusted project binary, GoFastr copies it into a
  private `0700` temporary snapshot outside the checkout while verifying its
  SHA-256 digest in a single pass, preventing TOCTOU swaps between verification,
  probing, and execution.
- If a `git pull` modifies the binary, changes the command, or adds an
  unapproved capability, execution is refused until you re-run
  `gofastr plugin trust <name>`. Revoke trust anytime with
  `gofastr plugin untrust <name>` from the project directory or a subdirectory,
  even if the `plugins:` section fails to decode.
- A conflicting or malformed project `gofastr.settings.yml` never disables or
  bypasses your globally registered hooks or overrides.

## Hooks and built-in overrides (opt-in seam)

By default, built-in commands can never be shadowed, wrapped, or hooked:
registering a plugin with the name or alias of a built-in command (`build`,
`dev`, `m`, `gen`, `g`, `plugin`, `help`, `version`, etc.) is always rejected.

A plugin under its own namespace may declare optional `hooks` (`pre:<cmd>`,
`post:<cmd>`) or `overrides` (`<cmd>`) in its manifest. Declared hooks and
overrides remain **inactive** until you explicitly opt in:

```bash
gofastr plugin add acme ./acme --allow-hook pre:build --allow-hook post:build
gofastr plugin add acme ./acme --allow-override deploy
```

Rules for hooks and overrides:

- Protected meta-commands (`help`, `version`, `plugin`) can never be hooked or
  overridden, so you always retain access to plugin management and help.
- At most one plugin may override a given built-in command; conflicting
  overrides fail closed.
- A `pre:<cmd>` hook exiting non-zero aborts the built-in command with that exit
  code. `post:<cmd>` hooks run only when the built-in command completes
  successfully (exit `0`).
- On `--json` built-ins (`generate`, `semantic`, `verify`), hooks write to
  stderr so they never corrupt stdout JSON, while an override owns stdout
  directly.
- An override plugin can **wrap** a built-in command by calling back into
  `cliplugin.BuiltinCommand(ctx, "validate", args...)` (which runs
  `gofastr --builtin validate ...` with `GOFASTR_NO_PLUGINS=1`), avoiding
  infinite recursion. Note that `BuiltinCommand` runs with the plugin's
  filtered environment (without blocked secrets such as `DATABASE_URL` or
  `GOFASTR_SECRET`); for commands that rely on ambient `DATABASE_URL` (such as
  `migrate`), use `pre:migrate` / `post:migrate` hooks instead so the host runs
  `migrate` with its original environment. `--builtin migrate` refuses to run
  when the caller's `DATABASE_URL` was stripped by plugin isolation rather than
  silently falling back to `.env`.
- Pass `gofastr --builtin <command> ...` or set `GOFASTR_NO_PLUGINS=1` to bypass
  all hooks and overrides for any single run.

## Authoring a plugin in Go (`core/cliplugin`)

External Go CLIs stay standalone and opt into the GoFastr CLI contract in a few
lines using `core/cliplugin`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/DonaldMurillo/gofastr/core/cliplugin"
)

var manifest = cliplugin.Manifest{
	ProtocolVersion: cliplugin.ProtocolVersion,
	Name:            "native",
	Version:         "v0.1.0",
	Summary:         "Build and run GoFastr iOS/Android apps",
	MinCLIVersion:   "v0.98.0",
	Subcommands: []cliplugin.CommandInfo{
		{Name: "doctor", Summary: "Check mobile toolchains"},
		{Name: "build", Summary: "Build iOS/Android artifacts"},
		{Name: "run", Summary: "Run on simulator, emulator, or device"},
	},
	EnvPassthrough: []string{"ANDROID_*", "JAVA_HOME", "DEVELOPER_DIR", "GOFASTR_BUNDLETOOL"},
	EnvFromFlags:   []string{"--keystore-pass-env", "--key-pass-env"},
}

func main() {
	if handled, err := cliplugin.MaybeServeManifest(os.Args[1:], os.Stdout, manifest); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	inv := cliplugin.InvocationFromEnv()
	if inv.Mode == cliplugin.ModeOverride {
		cmd, err := cliplugin.BuiltinCommand(context.Background(), append([]string{inv.TargetCommand}, os.Args[1:]...)...)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
				os.Exit(exitErr.ExitCode())
			}
			os.Exit(1)
		}
		return
	}

	// Normal standalone or `gofastr native ...` dispatch.
}
```

## Wire contract & environment rules

### Manifest schema (protocol v1)

When invoked with `--gofastr-plugin-manifest` (and `GOFASTR_PLUGIN_MANIFEST=1`),
the binary prints one JSON object on stdout within the probe timeout (`5s`) and
size cap (`256 KiB`). Unknown JSON fields and trailing content are rejected.

| Field | Required | Description |
| --- | --- | --- |
| `protocol_version` | Yes | Must be `1`. Mismatches fail with an upgrade diagnostic. |
| `name` | Yes | Default namespace (`^[a-z][a-z0-9-]{0,31}$`). |
| `version` | Yes | Plugin version string (single-line, e.g. `v0.1.0`). |
| `summary` | Yes | One-line description shown in `gofastr help` and `gofastr plugin list`. |
| `min_gofastr_version` | No | Minimum semver (`vX.Y.Z`) required of the host CLI. |
| `max_gofastr_version` | No | Maximum semver (`vX.Y.Z`) supported by the host CLI. |
| `subcommands` | No | List of `{name, summary}` objects for `doctor` and discovery. |
| `supports_json` | No | Whether the plugin supports machine-readable JSON output. |
| `env_passthrough` | No | Exact env keys (`JAVA_HOME`) or `PREFIX_*` wildcards (`ANDROID_*`). |
| `env_from_flags` | No | Flag names (`--keystore-pass-env`) whose argument names an env var to forward. |
| `hooks` | No | Optional `{phase: "pre"|"post", command, summary}` hooks (opt-in only). |
| `overrides` | No | Optional built-in command names the plugin can override (opt-in only). |

### Environment forwarding & secret protection

Plugins never inherit the full parent environment:

- **Manifest probe allowlist:** Probes receive only minimal OS runtime vars
  (`PATH`, `HOME`, `TMPDIR`, locale, XDG dirs, Windows system vars), excluding
  Go toolchain vars such as `GOPROXY` that may carry credentials.
- **Execution base allowlist:** Normal runs add terminal color hints and Go
  toolchain variables. Credential-bearing `GOPROXY` URLs (containing userinfo)
  are excluded by default unless explicitly approved via `--allow-env GOPROXY`.
- **Approved passthrough:** Global `add` approves manifest `env_passthrough` and
  `env_from_flags` (disable with `--no-manifest-env`, or add patterns with
  `--allow-env` / `--allow-env-flag`). Project `trust` requires explicit flags
  for every env variable or flag.
- **Hard-blocked secrets:** `GOFASTR_SECRET`, `DATABASE_URL`,
  `GOFASTR_HARNESS_*`, `GOFASTR_AUDIT_PASSWORD`, `AWS_SECRET_ACCESS_KEY`,
  `AWS_SESSION_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN`, and common LLM API keys are
  never forwarded implicitly, never matched by wildcards (`GOFASTR_*` is
  refused), and never accepted through `env_from_flags`.
- **Protocol context variables:** GoFastr injects `GOFASTR_CLI_PROTOCOL`,
  `GOFASTR_CLI_VERSION`, `GOFASTR_CLI_BIN`, `GOFASTR_PLUGIN_NAME`,
  `GOFASTR_PROJECT_ROOT`, plus `GOFASTR_HOOK_PHASE` / `GOFASTR_HOOK_COMMAND` or
  `GOFASTR_OVERRIDE_COMMAND` when active.

### Upgrading and compatibility

Every plugin invocation re-probes the binary's manifest so an in-place upgrade
(for example via `go install`) is checked immediately:

- If a plugin moves to a newer `protocol_version`, GoFastr fails cleanly and
  reports the expected vs. actual protocol version.
- If a plugin declares `min_gofastr_version` or `max_gofastr_version` outside
  the installed `gofastr` release (non-`dev` builds), GoFastr refuses to run it
  and reports the required version.
- Existing standalone invocations (such as `gofastr-native doctor`) keep working
  unchanged because `MaybeServeManifest` only triggers on
  `--gofastr-plugin-manifest`.

## Common mistakes

- **Using an interpreter launcher in `gofastr.settings.yml` (such as
  `["sh", "./tools/plugin.sh"]`).** Project commands must point to a single
  direct executable inside the project root so the SHA-256 pin covers the exact
  artifact.
- **Naming a plugin after a built-in command or alias (such as `build` or `m`).**
  Namespace collisions with built-ins are always rejected. Register the plugin
  under its own namespace and opt into `--allow-override build` or
  `--allow-hook pre:build` if you want to extend a built-in.
- **Using an override to wrap `migrate` with an exported `DATABASE_URL`.**
  Overrides run with a filtered environment; use `pre:migrate` / `post:migrate`
  hooks instead so the host executes `migrate` with its original environment.
- **Expecting a `gofastr.settings.yml` plugin command to run immediately after
  `git clone`.** Repo-declared commands never execute silently; run
  `gofastr plugin trust <name>` with explicit capability flags after inspecting
  the binary.
