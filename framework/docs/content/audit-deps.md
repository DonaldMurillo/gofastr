# Auditing init-time global registrations

`gofastr audit deps` scans your project (and its modules) for packages
whose `init()` mutates framework-wide state. The output is a flat list
of every call site, grouped by Go import path, sorted alphabetically.

## Why this exists

A malicious dependency that lands in your `go.mod` can run arbitrary Go
code at process start. That's the same trust model as importing any Go
package — `os.Exec`, `net.Dial`, file writes, and route hijacks are all
available to it. The audit doesn't change that surface; it just makes
the *most invisible* part of it visible: things that happen before
`main()` runs.

In particular, the audit flags packages that contribute to GoFastr's
global registries:

| Kind                       | What it does                                                   |
|----------------------------|----------------------------------------------------------------|
| `style.Contribute`         | Adds a CSS fragment to the host's theme stylesheet at boot     |
| `registry.RegisterStyle`   | Registers a named, lazy-loaded per-component CSS sheet         |
| `render.RegisterComponent` | Replaces / extends a named render component                    |
| `render.RegisterLayout`    | Replaces / extends a named layout                              |
| `render.RegisterFunc`      | Replaces / extends a named template function                   |

If you didn't expect a particular dependency to appear in this list,
that's the signal to investigate it.

## Usage

```sh
# audit the current module
gofastr audit deps

# audit a specific directory
gofastr audit deps /path/to/project
```

Sample output:

```
Init-time global registrations
(packages whose init() can mutate framework-wide state)

example.com/myapp/screens
  style.Contribute ×2
    screens/home.go:14
    screens/about.go:9

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
`@import`, `@font-face`, or `cursor: url(...)` — GoFastr's default
strict CSP (`default-src 'self'; img-src 'self' data:`) blocks every
external-URL load from CSS.

So the realistic supply-chain risk via `style.Contribute` is **UX
vandalism**, not credential theft. Vetting your dependencies (read the
audit output before each major upgrade) is the defence.

## What's NOT in the audit (yet)

The scanner only follows aliased imports of the framework's tracked
packages. It does not look inside `vendor/`, `node_modules/`, or
`_test.go` files. It does not detect:

- `unsafe`-backed jumps to the registries (vanishingly rare)
- Indirect registrations via reflection / `init()` chains that don't
  use the named package alias
- Non-framework init-time side effects (raw `init()` with arbitrary Go)

For those, read the dependency's source. The audit is a fast first
pass, not a substitute for review.

## Exit status

`gofastr audit deps` always exits 0 unless the walk itself fails (bad
path, unreadable file). Findings do not cause a non-zero exit — that
keeps the command composable in dev workflows. For CI failure on new
registrations, diff the output against a checked-in baseline.

## Common mistakes

- **Expecting a non-zero exit on findings.** `gofastr audit deps`
  exits 0 whenever the walk succeeds — findings alone never fail it.
  A CI gate that just runs the command passes forever; diff the output
  against a checked-in baseline instead.
- **Assuming `vendor/` and test files are covered.** The walker skips
  `vendor/`, `node_modules/`, `.git`, hidden directories, and every
  `_test.go` file. A registration living in vendored code will not
  appear in the report.
- **Treating an empty report as a vetted supply chain.** The scanner
  only follows aliased imports of the tracked framework registries.
  Raw `init()` side effects (`os.Exec`, `net.Dial`, file writes),
  reflection-driven registrations, and `unsafe` tricks are invisible
  to it — it's a fast first pass, not a dependency review.
