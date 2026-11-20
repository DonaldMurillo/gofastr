# Contracts: `gofastr verify`

`go build` checks syntax and types; `go vet` checks Go footguns. `gofastr verify` checks whether your app still holds to GoFastr's architectural, security, UI, and testing contracts—attaching the reason, a bad/good example, and the fix to every finding.

```bash
gofastr verify
```

## Capabilities & CLI flags

Run all rules, or pass one or more capabilities (or short aliases like `a11y`, `sec`, `perf`, `arch`, `test`, `perms`, `entity`, `route`, `ui`, `db`, `guidance`):

```bash
gofastr verify routing security
gofastr verify --list                     # list every rule in the catalog
gofastr verify --explain GOFASTR1002      # print why, fix, and examples for one rule
gofastr verify --changed                  # report only uncommitted + untracked files
gofastr verify --changed=main             # report changes since branch fork point
gofastr verify --rule GOFASTR1005 --fix   # apply safe autofixes for one rule
gofastr verify --json                     # machine-readable report for CI/agents
gofastr verify --sarif verify.sarif       # SARIF 2.1.0 for GitHub code scanning
```

| Capability | What it catches |
|---|---|
| `routing` | Duplicate routes, `:id` instead of ServeMux `{id}`, lowercase HTTP methods, in-page state modeled as navigation, untested routes, unsafe prefix checks |
| `permissions` | Per-user entities missing `OwnerField`, mutating routes outside guarded groups, auth configured but unmounted |
| `security` | SQL string concatenation, POST forms missing CSRF, raw `render.HTML` concatenation, insecure cookies, committed secrets, raw `encoding/json` bypassing `core/handler` strict decode |
| `data` | Discarded DB write errors (`db.Exec(...)`) |
| `entities` | MCP tools on non-CRUD entities, public anonymous entities, exposed CRUD with no auth wired |
| `architecture` | Upward layer imports and forbidden package edges |
| `rendering` | Ownerless CSS files (`GOFASTR1809`), stale `*.style.css` / `*_style.gen.go` pairs (`GOFASTR1814`), unknown or duplicated theme tokens (`GOFASTR1806`–`1825`), `location.href` hard navigation (`GOFASTR1802`), bespoke `EventSource` (`GOFASTR1803`), inline `style=` / `<script>`, physical CSS directions in kit code (`GOFASTR1824`) |
| `accessibility` | Static WCAG floor: missing alt text, unnamed controls/landmarks, broken form wiring, skipped heading levels |
| `performance` | Per-call `regexp.Compile`, N+1 queries, reflection on the request path |
| `testing` | Routes, permissions, roles, entity ops, hooks, and event subscribers unexercised by tests; coverage floor |
| `ai` | Hand-rolled CRUD for declared entities, reimplementing built-in batteries, raw SQL bypassing entity scoping |

`gofastr build` also runs contracts and fails on error-severity findings (`--no-contracts` skips it).

### The `vet` stage

Before running contract analyzers, `gofastr verify` runs `go vet` followed by GoFastr's type-aware vettool analyzers (unbounded body reads, `==` secret compares, world-readable secret files, HTTP clients without timeouts, `discardeddecode`, `textbind`, required JSON arrays, plus extra `x/tools` checks). Use `--no-vet` to skip both passes.

To waive an intentional vettool finding, add a reasoned directive on the line or directly above it:

```go
//gofastr:allow(clienttimeout) health probe against a local port, bounded by ctx
resp, err := http.Get(probeURL)
```

## Suppressions, config & baselines

Every rule runs at its declared severity by default. Relaxations must leave a written trace:

### Per-site suppression

```go
//gofastr:allow(GOFASTR1902) public webhook receiver; signature verified in handler
r.Handle("POST", "/hooks/stripe", stripeWebhook)
```

- A real reason is mandatory (`GOFASTR0001` flags missing reasons, `GOFASTR0003` unknown rules, `GOFASTR0002` stale suppressions that match nothing—clean dead ones with `gofastr verify --rule GOFASTR0002 --fix`).
- `//gofastr:allow-file(RULE) reason` covers a whole file. `//gofastr:allow(all)` is rejected.
- In CSS files (`*.style.css` or `.css`), use `/* gofastr:allow(GOFASTR1809) reason */` (must precede the first rule for `GOFASTR1809`). In JS files, use `// gofastr:allow(GOFASTR1802) reason`.

### Project config (`gofastr.settings.yml`)

Tune severities, exemptions, or coverage thresholds in the `contracts:` block of [`gofastr.settings.yml`](settings.md):

```yaml
contracts:
  exempt:
    - "**/testdata/**"
  performance: warn
  rules:
    GOFASTR1003:
      severity: warn
      exempt: ["cmd/**"]
  coverage:
    minimum: 90
    routes: true
    permissions: true
    entities: true
```

Every active relaxation is printed in the report footer so silent downgrades can't hide.

### Adopting on an existing codebase (baselines)

Record existing debt once so CI fails only on *new* findings:

```bash
gofastr verify --strict --baseline-write
```

This atomically writes `.gofastr-contracts-baseline.json` (keyed by `(rule, file)`, not line numbers, so formatting edits don't churn it). Commit it to the repo—`gofastr verify`, `gofastr build`, `gofastr dev`, and MCP `contracts_verify` all read that conventional path. When you fix baselined debt, verify nudges you to re-record so the ratchet tightens. (`--baseline-write` refuses `--sarif` or filtered runs so you never accidentally drop accepted debt.)

## Owned-style rules (`*.style.css`)

`gofastr verify` shares `core-ui/ownstyle` with `gofastr gen styles` so CSS checks never disagree with code generation:
- **`GOFASTR1809`:** Non-owned `.css` files outside `*.style.css` / `*.tokens.css` (excluding pure custom-property sheets).
- **`GOFASTR1814`:** Missing or out-of-date `<name>_style.gen.go` / `<name>_tokens.gen.go` (run `gofastr gen styles`).
- **`GOFASTR1806`–`1825`:** Unknown `var(--...)` tokens, hardcoded values that match a theme/app token, kit class overrides (`.fui-*`/`.cui-*`/`.hui-*`), `!important`, raw `@media` widths (use `(--above-md)`), missing `@media (--reduced-motion)`, non-placement CSS on kit component roots (`GOFASTR1817`), cross-package style handle leaks (`GOFASTR1818`), and disallowed top-level at-rules (`@import`).

## Dev loop & MCP integration

- **`gofastr dev`:** Runs changed-file contract analysis *after* each reload (coalescing rapid saves) and prints a compact summary only when findings change.
- **MCP tools:** With `framework.WithMCPIntrospection()`, agents can query `contracts_list`, `contracts_explain`, and `contracts_capabilities`. Under `gofastr dev`, `contracts_verify` (honors the baseline; skips `go vet` since the dev loop compiles) and `contracts_fix` (one rule at a time) let agents check and fix without shelling out.
- **`--json` output:** Emits a single schema-versioned JSON object (`schema`, `passed`, `vet`, `counts`, `diagnostics` with embedded `ruleDoc`, `baselined`, `outsideChange`, `unparsed`, `relaxations`). Gate CI on `passed`.

## Semantic coverage

Line coverage doesn't prove a route was reached through your real router and middleware, or that a hook or event subscriber actually fired. `framework/semcov` records six runtime dimensions into `.gofastr/semantic-coverage.json` (routes `GOFASTR1101`, permissions `GOFASTR1102`, entity ops `GOFASTR1103`, registered hooks `GOFASTR1107`, event subscribers `GOFASTR1108`, roles `GOFASTR1109`):

- `framework.TestHarness` records automatically; for custom in-process tests call `framework.RecordSemanticCoverage(t, app)`.
- For subprocess E2E tests, pass `GOFASTR_SEMANTIC_COVERAGE=1` in the server's environment.
- Disable with `GOFASTR_NO_SEMANTIC_COVERAGE=1`; override directory with `GOFASTR_SEMANTIC_COVERAGE_DIR`.
- Missing manifest on a fresh clone reports `info` only; an existing manifest missing a route fails.

## Writing your own rules

`framework/contracts` is an importable library. Register custom org rules under your own uppercase prefix (e.g., `ACME101`, never `GOFASTR*`) and run them from a tiny `cmd/contracts-verify` binary—they inherit config severities, exemptions, `//gofastr:allow`, baselines, JSON/SARIF output, and MCP exposure:

```go
package main

import (
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

func init() {
	contracts.RegisterRules(contracts.Rule{
		ID: "ACME101", Slug: "data/orders-are-audited",
		Title: "Order writes bypass the audit trail",
		Capability: contracts.CapData, Severity: contracts.SeverityError,
		Summary: "A write to orders does not go through audit.Record.",
		Why:     "Regulated flows must leave a trail.",
		Fix:     "Wrap the write in audit.Record(...).",
		Doc:     "entity-declarations",
		Examples: []contracts.Example{{
			Bad:  `db.Exec("UPDATE orders ...")`,
			Good: `audit.Record(ctx, func() { db.Exec("UPDATE orders ...") })`,
		}},
	})
	contracts.Register(&contracts.Analyzer{
		Name: "acme-audit", Doc: "audit-trail discipline for order writes",
		Rules: []string{"ACME101"},
		Run: func(p *contracts.Pass) ([]contracts.Diagnostic, error) {
			return nil, nil
		},
	})
}

func main() {
	cfg, _ := contracts.LoadConfig(".", "")
	pass, _ := contracts.NewPass(".", cfg)
	report, _ := contracts.Run(pass, contracts.RunOptions{})
	fmt.Print(contracts.FormatText(report, contracts.TextOptions{}))
	os.Exit(report.ExitCode())
}
```

## Common mistakes

- **Suppressing instead of fixing.** Run `--explain <rule>` first; reserve `//gofastr:allow` for cases where the rule's premise genuinely doesn't apply.
- **Using `:id` in API routes.** Core-UI screens accept `:id`, while `app.Router()` uses Go `ServeMux`'s `{id}` syntax (`GOFASTR1002` intentionally has no autofix because your handler must update its param lookup too).
- **Treating a clean `testing` pass with no manifest as proof.** Run `go test ./...` first so `.gofastr/semantic-coverage.json` exists.
- **Setting `coverage.minimum` without a coverage profile.** Run `go test -coverprofile=coverage.out` before verifying line coverage.
- **Reversing `architecture` layer order in `gofastr.settings.yml`.** Layers are ordered top-first (a layer may import itself and layers below it).
