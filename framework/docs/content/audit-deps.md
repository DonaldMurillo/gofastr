# Auditing global registration call sites

`gofastr audit deps [root]` scans Go source in the selected directory
tree, defaulting to the current directory. It reports candidate calls
to framework-wide registration APIs and groups them by package label.
Each label uses the selected root's module path joined with the
directory's path relative to that root. Without a module path, labels
use relative paths. A nested module still uses the selected root's prefix.

The scan follows linked package directories and symlinked roots while
visiting each resolved directory once, so cycles cannot repeat findings.
Findings retain the linked package's path under the selected root.
The scan skips build outputs and hidden/underscore descendants.
Imported modules in the Go module cache are outside the scan unless
you select or link their source tree.

## Why this exists

Imports can run Go code at process startup, including calls that
mutate global registries. Use the audit to locate registration calls
for review. It matches import paths and call syntax anywhere in a
file, including ordinary functions. Inspect each call site to
determine when or whether it runs.

The audit looks for these registration APIs.

| Kind                       | What it does                                                   |
|----------------------------|----------------------------------------------------------------|
| `style.Contribute`         | Adds a CSS fragment to the host's theme stylesheet             |
| `registry.RegisterStyle`   | Registers a named, lazy-loaded per-component CSS sheet         |
| `render.RegisterComponent` | Replaces / extends a named render component                    |
| `render.RegisterLayout`    | Replaces / extends a named layout                              |
| `render.RegisterFunc`      | Adds or replaces a named template function                            |

## Usage

```sh
# audit from the current directory
gofastr audit deps

# audit the selected project source tree
gofastr audit deps /path/to/project

# audit an explicitly selected dependency checkout
gofastr audit deps /path/to/checkouts/gofastr
```

Example output for the dependency checkout:

```
Candidate global registration call sites

github.com/DonaldMurillo/gofastr/framework/ui
  registry.RegisterStyle ×38
    framework/ui/styles_primitives.go:15
    framework/ui/styles_primitives.go:16
    …
```

## What's blocked by CSP (and what isn't)

For `style.Contribute` specifically: a malicious dep CAN add a rule
like `body { display: none }` or override a Cancel button's
`content`. It CANNOT exfiltrate data via `background: url(//evil/...)`,
`@import`, `@font-face`, or `cursor: url(...)`. GoFastr's default
strict CSP (`default-src 'self'; img-src 'self' data:`) blocks every
external-URL load from CSS.

So the realistic supply-chain risk via `style.Contribute` is **UX
vandalism**, not credential theft. Vetting your dependencies (read the
audit output before each major upgrade) is the defence.

## What's NOT in the audit (yet)

The scanner matches calls through imports of the framework's tracked
packages. It skips descendant `vendor/`, `node_modules/`, `dist/`, `bin/`,
`build/`, `tmp/`, and hidden/underscore directories, plus `_test.go` files.
An explicitly selected root is scanned even if its name is normally excluded.
It does not detect:

- `unsafe`-backed jumps to the registries (vanishingly rare)
- Indirect registrations via reflection / `init()` chains that don't
  use the named package alias
- Non-framework init-time side effects (raw `init()` with arbitrary Go)

For those, read the dependency's source. The audit is a fast first
pass, not a substitute for review.

## Exit status

`gofastr audit deps` exits 1 on scan I/O failures, including an unreadable
root or source file, or if the unmounted-package settings cannot be read
or parsed. The scan skips Go files with syntax errors. Findings alone
leave the exit status at 0. To flag new registrations in CI, compare
the output with a checked-in baseline.

## Common mistakes

- **Expecting a non-zero exit on findings.** Add an output comparison
  against a checked-in baseline to your CI gate.
- **Assuming `vendor/` and test files are covered.** The walker skips
  descendant `vendor/`, `node_modules/`, `dist/`, `bin/`, `build/`, `tmp/`,
  hidden/underscore directories, and every `_test.go` file.
  A registration living in vendored code will not
  appear in the report.
- **Treating an empty report as a vetted supply chain.** The scanner
  only follows aliased imports of the tracked framework registries.
  Raw `init()` side effects (`os.Exec`, `net.Dial`, file writes),
  reflection-driven registrations, and `unsafe` tricks are invisible
  to it. It's a fast first pass, not a dependency review.
