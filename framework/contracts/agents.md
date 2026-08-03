# framework/contracts — `gofastr verify`

One pipeline that answers "is this still a good GoFastr application?"
before the code merges. It discovers the route table, entity
declarations, permission strings, and rendering surface, then reports
where they no longer hold to the framework's contract — with the reason
and the fix attached to every finding.

**Use this when** the prompt mentions: verify, lint, check my code, "is
this idiomatic", "did I wire this right", pre-merge check, pre-commit
hook, CI gate, code quality, contract, rule, `GOFASTR` followed by a
number, "why is this flagged", suppress a warning, baseline, technical
debt ratchet, semantic coverage, "which routes are untested", "which
permissions are never exercised", architecture layering, forbidden
import, SARIF, code scanning.

**Import:** nothing — this is a CLI surface. `gofastr verify`.

**Shape:**

```
gofastr verify                       # everything, strict
gofastr verify routing security      # one or more capabilities
gofastr verify --changed             # only what this change touched
gofastr verify --explain GOFASTR1002 # one rule in full
gofastr verify --rule GOFASTR1005 --fix
gofastr verify --json                # machine-readable, rule attached
```

Every finding carries a stable ID (`GOFASTR1002`), the capability it
belongs to, why it matters, how to fix it, a bad/good example pair, and
a doc topic. `--json` inlines the whole rule on each diagnostic so one
finding is a complete work item — an agent acting on it needs no second
call.

**Rules are strict by default.** There are exactly two ways to say no,
and both leave a written trace:

```go
//gofastr:allow(GOFASTR1902) public webhook; signature verified in the handler
r.Handle("POST", "/hooks/stripe", stripeWebhook)
```

...or a `gofastr.contracts.yml` entry, which is printed in the report
footer so a run that passes because half of it was switched off says so.
A directive with no reason is itself a finding.

**Adopting on an existing app:** `gofastr verify --strict
--baseline-write` records current findings as accepted debt and fails
only on new ones. Counts are keyed by (rule, file), not line, so a
reformat does not invalidate it.

**Don't reinvent:** do not add a bespoke linter, a `//nolint`-style
comment scheme, or a CI step that greps for banned patterns. A missing
check is a missing *rule* — rules live in `framework/contracts/catalog.go`
as data. An analyzer *declaring* a rule the catalog does not know is an
init-time panic; a finding *emitted* under an undeclared rule is dropped
and reported as an analyzer error, which fails the run; and a built-in
rule with no analyzer is caught by the catalog test suite. There is no
path to a silently undocumented check — but note the middle case is an
error, not a panic, so a custom-rule author must watch the run result,
not wait for a crash. Do not disable a rule to make a build pass; suppress the instance
with a reason, or record a baseline.

**Under `gofastr dev`** the same analysis is reachable over MCP:
`contracts_verify` returns the findings, `contracts_fix` applies one
rule's autofixes, and `contracts_list` / `contracts_explain` /
`contracts_capabilities` expose the catalog. Those two source-touching
tools exist only in the dev loop — a deployed `/mcp` does not have them.

Full reference: `gofastr docs contracts`.
