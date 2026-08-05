# Contracts — `gofastr verify`

`go build` answers "does this compile". `go vet` answers "is anything
obviously wrong". Neither answers the question you actually have before
merging:

> Is this still a good GoFastr application?

`gofastr verify` answers that one. It discovers your route table, entity
declarations, permission strings, and rendering surface, then reports
where they no longer hold to the framework's contract — with the reason
and the fix attached to every finding.

```
gofastr verify
```

## What it checks

Rules are grouped into capabilities, and each capability is also a filter:
`gofastr verify routing security` runs two of them.

| Capability | Catches |
|---|---|
| `routing` | duplicate routes, `:id` where ServeMux wants `{id}`, lowercase methods, in-page state modelled as navigation, routes no test mentions |
| `permissions` | per-user data exposed with no owner field, mutating routes outside any guarded group, auth configured but never mounted |
| `security` | SQL built by concatenation, POST forms with no CSRF input, `render.HTML` on a concatenation, cookies missing HttpOnly/Secure/SameSite, committed credentials |
| `data` | writes whose result and error are both discarded |
| `entities` | MCP tools on an entity with CRUD disabled, entities opted into anonymous access, a CRUD entity exposed with no auth wired (every operation 401s) |
| `architecture` | imports that point up the layer stack, explicitly forbidden edges |
| `rendering` | CSS outside the design system, `location.href` used as navigation, bespoke `EventSource`, inline `style=`, `<script>` with an inline body |
| `accessibility` | the static WCAG floor — missing alt text, unnamed controls and landmarks, incomplete form controls, implicit heading levels, elements missing required metadata |
| `performance` | regexps compiled per call, N+1 queries, reflection on the request path |
| `testing` | routes, permissions, roles, entity operations, lifecycle hooks, and event subscribers no test exercised; disabled tests; a line-coverage floor; an unreadable coverage manifest |
| `ai` | hand-rolled CRUD for a declared entity, subsystems a battery already provides, raw SQL bypassing entity scoping |

`gofastr verify --list` prints the whole catalog. `gofastr verify
--explain GOFASTR1002` prints one rule in full.

## Strict by default, relaxed on purpose

Every rule in the catalog is enforced at its declared severity. There is
no opt-in, because a rule you have to switch on is a rule nobody switches
on. There are exactly two ways to say no, and both leave a written trace.

Configuration moves severity in either direction — nothing is quieter than
declared unless you write it down, but a team that wants
`routing/untested-route` to be an *error* rather than a warning is making
a real choice and can. Either way it appears in the report footer.

**One instance**, at the site:

```go
//gofastr:allow(GOFASTR1902) public webhook receiver; signature is verified in the handler
r.Handle("POST", "/hooks/stripe", stripeWebhook)
```

The reason is required — a directive without one is itself a finding
(`GOFASTR0001`). So is a directive naming a rule that does not exist
(`GOFASTR0003`), or one that stops matching anything (`GOFASTR0002`), so
suppressions cannot quietly accumulate. `//gofastr:allow(all)` is
rejected outright: it would absorb every rule added afterwards.

Put the directive at the end of the offending line, or on its own line
directly above it. `//gofastr:allow-file(RULE) reason` covers a whole
file.

**A whole surface**, in `gofastr.contracts.yml`:

```yaml
contracts:
  exempt:
    - "**/testdata/**"

  performance: warn          # a capability, downgraded

  rules:
    GOFASTR1003:             # a single rule
      severity: warn
      exempt: ["cmd/**"]

  coverage:
    minimum: 90
    routes: true
    permissions: true
    entities: true
```

Path globs are slash-separated (`cmd/**`, `**/testdata/**`); a backslash
is accepted as a separator too, so a path typed on Windows works rather
than silently matching nothing.

A `contracts:` block in `gofastr.yml` works too, so a small project needs
one file rather than two. Every relaxation is printed in the report
footer — a run that passes because half of it was switched off says so.

## Adopting it on an existing codebase

Strict-by-default and adoption pull against each other. Switch `verify` on
in a mature app and you get hundreds of findings at once; nobody fixes
hundreds of findings at once, so the realistic outcomes are "turn the tool
off" or "downgrade every rule to warn". Both end with nothing enforced.

A baseline is the third option — accept what is there, fail on what is
added:

```
gofastr verify --strict --baseline-write
```

That records every current finding into
`.gofastr-contracts-baseline.json` and commits it. It refuses to run
with `--rule` or a capability filter: a baseline recorded from a
narrowed run would erase every other rule's accepted debt, and the next
full `verify` or `build` would fail on findings the team had already
signed off. From then on the same
`gofastr verify --strict` passes on the debt it recorded and fails on
anything new. The file lives in the repository, not under `.gofastr/`:
unlike the coverage manifest this is a reviewed decision about what debt
is accepted, and it belongs in the diff where a reader can watch it
shrink.

The conventional path is the contract. The build gate, the dev loop, and
MCP `contracts_verify` all read `.gofastr-contracts-baseline.json` from
the analysed root, and none of them can be pointed elsewhere.
`verify --baseline <file>` overrides the path for that one invocation —
useful for trying a candidate baseline before adopting it — but a
baseline only `verify` can see is one every other gate will ignore, so
the recorded file belongs at the conventional path.

Counts are keyed by **(rule, file)**, not by line. Line numbers churn on
every edit, and a baseline that goes stale on a reformat is a baseline
people delete. Moving a finding within a file keeps it accepted; adding
one more of the same rule to that file does not.

As you pay debt down, the report says so:

```
· 12 pre-existing findings absorbed by the baseline
· 3 baseline entries are now over-accepting — re-record with gofastr verify --baseline-write
```

That nudge matters. A fixed finding keeps its allowance until someone
re-records, and that slack is exactly where a new finding could hide.
Re-recording is what makes the baseline a ratchet rather than a mute
button.

Suppressing a baselined finding with `//gofastr:allow` does not free its
slot: the finding still exists, so it keeps occupying its allowance, and
a new finding of the same rule in that file still fails. The entry shows
up in the over-accepting nudge instead — a re-recorded baseline, which
also runs after suppressions are consumed, would no longer include it.

Only **gating** findings are recorded. Anything below the run's fail-on
severity is left out on purpose: a baseline exists to unblock a gate, and
an entry for a finding that cannot fail the run would absorb it on every
later run — silencing an informational signal you deliberately kept
visible.

That interacts with the semantic-coverage rules specifically. They record
*which tests ran*, which differs by environment: a CI job that excludes
some packages, or a machine with Docker down, produces different findings
for the same commit. Gating a shared CI on a recorded baseline of those is
flaky by construction — a baseline recorded in one lane fails in the
other. The usual answer is to downgrade them to `info` in the config, so
they stay visible on every run and never gate; the filter above is what
stops `--baseline-write` from then silencing them anyway. This repository
does exactly that — see its `gofastr.contracts.yml`.

A baseline is not a substitute for the config. Use `gofastr.contracts.yml`
when a rule's premise genuinely does not hold for a surface — that is
permanent and explained. Use a baseline for debt you intend to pay.

## Checking only what you changed

`gofastr verify` reports the whole tree, which is the wrong shape for the
three moments you most want it: a pre-commit hook, a dev-loop rebuild, and
a PR review. All three ask the narrower question — *what did this change
break* — and answering it by reading a 200-line whole-repo report is
answering a different one.

```
gofastr verify --changed           # uncommitted work, including new files
gofastr verify --changed=main      # everything since this branch forked
```

The **analysis still runs over the whole tree**, and must: the route
table, the entity list, and the coverage manifest are only meaningful
whole. Only the reporting narrows. A duplicate route introduced by editing
one file is still found, because the other half of the pair was analysed
too.

`--changed=<ref>` compares against the **fork point** with that ref, not
its tip — otherwise a long-lived branch would report every finding in
everyone else's commits as something it changed. Untracked files count as
changed; a brand-new file full of findings is exactly what a pre-commit
check should catch, and `git diff` alone never lists it.

A narrowed run says what it withheld:

```
· 41 findings outside the changed files — run without --changed for the full picture
```

Outside a git repository the flag warns and reports everything, rather
than silently reporting nothing.

`--changed` narrows the **fixes** too, not just the report. `gofastr
verify --changed --fix` in a pre-commit hook rewrites only the files the
change touched — editing an unrelated file there would land an unreviewed
edit in someone's commit, and the narrowed report they were reading would
never mention it. Without `--changed`, `--fix` still applies everywhere.

## In the dev loop

`gofastr dev` reports contract findings after each reload, scoped to the
files you changed:

```
✓ Reloaded!

⚠ contracts — findings in what you changed:
    GOFASTR1002  main.go:14:2  route DELETE /wipe/:id uses `:id` — ServeMux matches that literally…
    GOFASTR1902  main.go:14:2  DELETE /wipe/:id is registered outside any group…
    explain any of these: gofastr verify --explain <rule>
```

Three deliberate properties:

- **After the reload, never before it.** Analysis takes about a second on
  a large tree, and a second added to every save is the difference between
  a loop people use and one they turn off. The server restarts first; this
  runs behind it and prints when ready.
- **Compact.** Rule ID, location, one line. The reasoning, the example,
  and the fix are what `gofastr verify --explain` is for — repeating them
  on every save would bury the loop. Long reports are capped, and say how
  many they hid.
- **Quiet when nothing changed.** An unchanged report is not reprinted;
  fixing the last finding prints `contracts: clean` once. A loop that
  repeats itself is a loop you stop reading.

The baseline applies here too, so an existing project sees only what it
is adding. And failures of the analysis itself are said out loud, once —
a config that will not parse, a tree that cannot be scanned, a corrupt
baseline. In a loop, silence reads as "clean", so nothing is allowed to
fail silently: even a changed-set that cannot be computed (a repository
with no commits yet, say) falls back to whole-tree reporting *and says
so* rather than silently widening.

## Semantic coverage

Line coverage answers "did this statement run". It cannot answer:

- Did a request ever reach this route through the real router, the real
  middleware chain, and the real auth check?
- Was this permission ever evaluated?
- Did anything ever call this entity's delete endpoint?
- Did this `OnBeforeCreate` hook ever actually fire?
- Did anything ever publish the event this handler subscribes to?
- Has a request ever arrived holding this role?

A handler can sit at 100% line coverage because a unit test called the
function directly, while no request has ever reached it through the
router. `framework/semcov` records what a test run genuinely exercised
into `.gofastr/semantic-coverage.json`, and the `testing` rules diff that
against the discovered surface.

Six dimensions are recorded, each through a hook the runtime packages
expose rather than by importing the recorder (the router's
`SetServeHook` method,
`access.SetObserver`, `hook.SetObserver`, `event.SetObserver`) — so
`core/router`, `framework/access`, `framework/hook`, and `framework/event`
stay dependency-free leaves and a production binary pays one atomic load
per check:

| Dimension | Recorded when | Rule |
|---|---|---|
| Routes | a request matches a registered pattern, after the route gate and before the middleware chain | `GOFASTR1101` |
| Permissions | `access.Can` / `CanResource` / `RequirePermission` evaluates one, **whatever the verdict** | `GOFASTR1102` |
| Entity operations | a CRUD route for a declared entity is served | `GOFASTR1103` |
| Lifecycle hooks | a *registered* hook body actually runs | `GOFASTR1107` |
| Event subscribers | the type a handler subscribes to is published | `GOFASTR1108` |
| Roles | a caller holds the role during a permission check | `GOFASTR1109` |

Two of those distinctions carry the weight. A permission **denial** is
recorded as coverage, because a test asserting a rejection proves the
boundary exists at least as well as one asserting a grant — the failure
worth catching is a check that is never reached at all. And a hook firing
is only recorded when something is registered for that lifecycle point;
`ExecuteHooks` runs on every CRUD operation regardless, so crediting the
call would hand every entity full hook coverage on its first request.

The role check inverts the usual worry. A role granting too *little*
surfaces as a broken feature someone reports; a role granting too *much*
surfaces as nothing at all, until it is used. Recording which roles a
suite actually authenticated as is how the second kind becomes visible.

Hooks and event subscribers are the two surfaces with no callers to
follow. Nothing in the codebase invokes them by name, so a rename on the
emitting side — `order.placed` becoming `order.completed` — leaves the
subscriber compiling, the tests green, and the notification never sent.

`framework.TestHarness` records automatically, so a suite already using it
gets coverage without changing a line. Drive the app in-process some other
way and call it directly:

```go
func TestCheckout(t *testing.T) {
    framework.RecordSemanticCoverage(t, app)
    // …
}
```

A test that builds the binary and drives it over real HTTP is a different
case: the requests happen in a **subprocess** that never touched the
harness, so nothing records and a thorough e2e suite scores zero. Set
`GOFASTR_SEMANTIC_COVERAGE=1` on the *server's* environment and it records
what it serves:

```go
srv.Env = append(os.Environ(), "PORT="+addr, "GOFASTR_SEMANTIC_COVERAGE=1")
```

The e2e test `gofastr generate` emits already does this. The manifest is
written from the request path rather than at shutdown, because a harness
usually SIGKILLs the server — flushing costs one write per newly-seen
route, not one per request.

Set `GOFASTR_NO_SEMANTIC_COVERAGE` to turn in-process recording off for a
suite.

Absence and drift are treated differently, on purpose. **No manifest at
all** — a fresh clone whose tests have never run — reports at info level
and does not fail: walling the first `verify` behind a full test run
teaches people to skip `verify`. **A manifest that exists but misses a
route** is real drift and fails. Run `go test ./...` once and the manifest
exists from then on. The file lives under `.gofastr/` (gitignored, wiped
by `make clean`), so it never ships.

## What this deliberately does not check

Three things are absent on purpose rather than pending.

**Generated files.** Analyzers skip anything carrying a `Code generated …
DO NOT EDIT` header: the developer cannot fix a finding there, only the
generator can, and a finding nobody can act on trains people to stop
reading. This is why `cmd/check-csp` still exists alongside the
rendering rules — it scans generated output at `make build` time, where
the generator's product is exactly what needs checking. The two are a
division of labour, not a duplication: contracts polices what you write,
check-csp polices what the build emits.

**Middleware execution.** Recording which middleware ran needs a wrapper
around every registered middleware, permanently, on the request path — and
the rule it would enable ("you registered middleware that never ran")
almost never fires, because `app.Use` middleware runs on *every* request
by definition. Group-scoped middleware is the interesting case, and route
coverage already tells you whether those routes were reached. A permanent
per-request cost for a rule that cannot realistically fire is the wrong
trade.

**API versions.** `framework/experimental/apiversions` is an experimental
surface, explicitly outside the stability contract
(`framework/ARCHITECTURE.md`). Rules that pin an experimental API's shape
would make it harder to change, which is the opposite of what
"experimental" is for.

If either becomes worth having, the analyzer surface is the place to add
it — `framework/contracts/analyzers`, one file per capability.

## Output for agents

`--json` emits every diagnostic with its complete rule attached — the
reason, the fix, the bad/good example pair, the doc topic. One finding is
a complete work item, so an agent acting on it needs no second call:

```
gofastr verify --json | jq '.diagnostics[] | select(.severity=="error")'
```

### The `--json` shape

`schema` is the first field so a reader can branch on it before parsing
anything else. It is bumped when the shape changes in a way a consumer
must notice.

```json
{
  "schema": 1,
  "tool": "gofastr verify",
  "root": "/abs/path",
  "config": "gofastr.contracts.yml",
  "passed": false,
  "failOn": "error",
  "vet":   { "ran": true, "passed": true },
  "counts": { "errors": 1, "warnings": 3, "infos": 1, "total": 5 },
  "summary": [ { "capability": "routing", "errors": 1, "warnings": 2, "infos": 0 } ],
  "diagnostics": [ … ],
  "suppressed": 2,
  "baselined": 65,
  "baselineFixed": 3,
  "unparsed": 1,
  "outsideChange": 41,
  "relaxations": [ "rule GOFASTR1101 → info" ],
  "analyzerErrors": [],
  "timings": [ { "name": "rendering", "ms": 656.3, "diagnostics": 180 } ],
  "durationMs": 705.3
}
```

`capabilities` appears when the run was scoped (`gofastr verify routing`),
`config` when a config file was found, and `baselined`,
`baselineFixed`, `outsideChange`, `analyzerErrors` and `timings` are
omitted when empty — so absence means zero, never unknown. A skipped vet
stage omits `passed` entirely: a stage that never ran has no verdict,
and serialising `false` there would read "did not check" as "failed".

`unparsed` counts files the parser rejected. They produced no findings
from any analyzer, so without the count a tree mid-edit reads as clean for
exactly the files nobody could read. A tree that does not parse is normal
in the dev loop and reported rather than fatal — `go vet` runs first and
is where a genuine syntax error stops the run.

`passed` is the field to gate on — it already accounts for a failed vet
stage and for analyzer errors, either of which means "could not look"
rather than "nothing found". `baselined`, `baselineFixed`, and
`outsideChange` report what the run did NOT show: without them a narrowed
or baselined run is indistinguishable from a clean whole-tree one.

Each diagnostic carries its complete rule under `ruleDoc`, so one finding
is a self-contained work item:

```json
{
  "rule": "GOFASTR1003",
  "slug": "routing/untested-route",
  "capability": "routing",
  "severity": "warn",
  "file": "app.go",
  "line": 294,
  "column": 2,
  "message": "no test file mentions /api/tables/customers",
  "snippet": "fwApp.Router().HandleFunc(\"GET\", \"/api/tables/customers\", …)",
  "suggestion": "add a test that requests /api/tables/customers — …",
  "evidence": { "method": "GET", "pattern": "/api/tables/customers" },
  "ruleDoc": { "id": …, "title": …, "summary": …, "why": …, "fix": …,
               "examples": [ { "bad": …, "good": … } ],
               "doc": "routing", "severity": …, "autofix": false }
}
```

`file` is relative to `root`. `snippet` is omitted for a redacted finding
— the hardcoded-secret rule never echoes what it matched, over any
output. `evidence` is per-rule structured detail; treat unknown keys as
additive.

`--sarif verify.sarif` writes SARIF 2.1.0 for GitHub code scanning and
IDE inline diagnostics. Artifact URIs are relative to the analysed root,
and the run declares that root in `originalUriBaseIds` — so annotations
map correctly whether you ran verify from the repository root or pointed
it at a subdirectory.

Every report carries the `vet` stage that ran before the analyzers:

```json
"vet": { "ran": true, "passed": true }
```

`go vet` is a precondition, not a check — half the analyzers cannot read
what they need from a tree the compiler rejects, so a failing vet stops
the run and the document says so (`passed: false`, no diagnostics, and
vet's own output attached). `--no-vet` records `{"ran": false, "skipped":
"--no-vet"}` rather than omitting the field, because "we did not check"
must never read as "it was fine".

A running app built with `framework.WithMCPIntrospection()` also serves
the catalog over MCP — `contracts_list`, `contracts_explain`,
`contracts_capabilities` — so an agent can read what the framework expects
*before* writing code rather than after a build rejects it.

Under `gofastr dev`, two more tools turn the catalog from reference into
a working loop: `contracts_verify` runs the analyzers over the app's own
source tree and returns the findings as structured diagnostics, and
`contracts_fix` applies a single rule's autofixes and reports which files
changed. An agent can then verify → explain → fix without shelling out.

`contracts_verify` honours the project's baseline, so an agent's view of
the tree agrees with `gofastr verify`, `gofastr build`, and the dev
watcher — and reports the count it absorbed, so a clean result never
hides accepted debt. It also admits what it could not check: `unparsed`
counts files the parser rejected, and `analyzerErrors` lists checks that
errored instead of completing — so `passed: false` with zero findings
always comes with its reason. The one deliberate difference from
`gofastr verify` is the missing `go vet` stage: the dev loop's rebuild
already reports compile errors, and running vet again on every tool call
would only add latency. `contracts_fix` deliberately does not honour the
baseline: it records debt the team agreed to carry, and paying it down
is the point.

Both read local source, and `contracts_fix` writes it, so neither is
registered outside the dev loop — a deployed `/mcp` does not gate them,
it does not have them. `contracts_fix` takes one rule at a time on
purpose: a single rule's edits are reviewable in a way a whole-tree
rewrite is not. Rules with no autofix are refused with the reason,
rather than silently doing nothing.

## Fixing

`gofastr verify --fix` applies the mechanical fixes and re-verifies.
Only unambiguous rewrites carry one; anything needing a judgment call is
left to you with the fix written out in prose. Today that is `GOFASTR1005`
(uppercase a lowercase method), `GOFASTR1404` (add the missing cookie
attributes), and `GOFASTR0002` (delete a suppression that matches nothing).

The last is the one worth running regularly: suppression debt accumulates
quietly, and a directive left on a moved line silently covers whatever
code slid into its place. `gofastr verify --rule GOFASTR0002 --fix` clears
the dead ones — the whole line when the directive sits on its own, only
the comment when it trails a statement, never the code behind it.

`--rule <id>` narrows a run to one rule (or several, comma-separated, as
IDs or slugs). Paired with `--fix` it applies exactly that rule's edits
and leaves everything else alone, which keeps a fix commit reviewable:

```
gofastr verify --rule GOFASTR1005 --fix
```

An unrecognised rule name is an error rather than an empty run, so a typo
cannot read as a clean bill of health.

Applied fixes are gofmt-ed, so an edit only has to be syntactically
correct rather than match the surrounding indentation. Overlapping edits
are refused outright, and every edit records the text it expects to
replace — a file edited since analysis is caught by content, not just by
offsets that happen to still be in range. A corrupted source file is a
far worse outcome than "run verify again".

`GOFASTR1002` deliberately has **no** autofix even though the rewrite is
obvious. Changing `/users/:id` to `/users/{id}` on its own turns a loud
404 into a route whose handler still reads the parameter the old way and
fails quietly; the handler has to change in the same edit, and only you
know what it should read.

`gofastr build` runs the analyzers too, but fails only on error-severity
findings — a build that fails on "this route has no test" is a build
people learn to bypass. `--no-contracts` skips it.

The build gate reads the same baseline `verify` does. Recording one has
to fix both, or adopting a baseline leaves `build` permanently red and
the only exit a user finds is `--no-contracts`, which turns every check
off. New findings the baseline never recorded still stop the build.

Every rule carries at least one bad/good example pair — required, not
encouraged: `RegisterRules` rejects a rule without one at init, the same
place a malformed or duplicate rule already fails. A rule the reader
cannot see is a rule they will not apply.

The catalog is checked against the analyzers, not just against itself: no
rule may fire on its own documented *good* example, and a rule's *bad*
example has to produce it. A rule and its documentation cannot drift apart
without a test failing. Twenty rules whose examples need context one file
cannot express — a coverage manifest, a multi-package layout, a `_test.go`
— are listed with the reason, and that list is guarded too: if such an
example starts firing, the entry is stale and the test says so.

## Adding a rule

Rules live in `framework/contracts/catalog.go` as data; the detectors live
in `framework/contracts/analyzers/`. That split is deliberate: the catalog
has to be readable and serveable without a Go parser, and a detector has to
be replaceable without touching the documentation contract it satisfies.

`Why` and `Fix` are mandatory and validated at init. If you cannot write
the fix, the finding is an opinion and does not belong in the catalog.

## Your own rules

The pipeline is a library, and the catalog is open to registration: a
project can add rules the framework could never know about — "order
writes go through the audit trail", "handlers in `internal/api` never
import `internal/billing` directly" — and they ride everything the
built-ins ride: config severity, `off`, exemptions, `//gofastr:allow`
with a mandatory reason, the baseline ratchet, the JSON/SARIF output,
and `contracts_list`/`contracts_explain` over MCP in any app process
that imports them.

A custom rule picks its **own uppercase ID prefix** (`ACME101`, not
`GOFASTR9901`) — the `GOFASTR` namespace and its per-capability number
blocks belong to the built-in catalog, which is what keeps a suppression
written today from colliding with a future release. The slug's prefix
must name one of the existing capabilities, which decide where the rule
appears in reports.

Register the rule and its detector from an `init` (or before the first
run), then drive the pipeline from a small command of your own:

```go
package main

import (
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	// The built-in analyzers register themselves on import.
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

func init() {
	contracts.RegisterRules(contracts.Rule{
		ID: "ACME101", Slug: "data/orders-are-audited",
		Title:      "Order writes bypass the audit trail",
		Capability: contracts.CapData, Severity: contracts.SeverityError,
		Summary: "A write to orders does not go through audit.Record.",
		Why:     "Regulated flows must leave a trail; a silent write is an audit failure.",
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
			// p.Files, p.AST, p.Lines — the same pass the built-ins read.
			return nil, nil
		},
	})
}

func main() {
	cfg, err := contracts.LoadConfig(".", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	pass, err := contracts.NewPass(".", cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Print(contracts.FormatText(report, contracts.TextOptions{}))
	os.Exit(report.ExitCode())
}
```

`go run ./cmd/contracts-verify` is then your project's gate — wire it
into the same pre-commit hook or CI step you would wire `gofastr verify`
into. The stock `gofastr` binary only ever knows the built-in catalog:
custom rules live in your module, so they run through your command,
where the compiler can see them.

## Common mistakes

- **Suppressing instead of fixing.** `--explain` exists because most
  findings have a one-line remedy. Reach for `//gofastr:allow` when the
  rule's premise genuinely does not hold here, not when the fix is
  inconvenient — and write the reason for a reader, not for the linter.
- **Expecting `:id` to work in an API route.** It works in *screens*,
  which core-ui routes itself, and silently 404s in `Router().Handle`,
  which goes to ServeMux. GOFASTR1002 only fires on the second.
- **Reading a clean `testing` report as proof.** With no manifest, those
  checks did not run — that is what the `GOFASTR1106` info line is telling
  you. Run the tests first.
- **Configuring a coverage floor with no profile.** `coverage.minimum`
  needs `go test -coverprofile=coverage.out` to have produced something to
  read; verify reports the missing profile rather than quietly passing.
- **Putting an architecture layering in the config and expecting the
  order not to matter.** Layers are ordered top-first, and a package may
  import its own layer and anything *below* it. Reversing the list inverts
  every rule.
