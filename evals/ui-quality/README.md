# GoFastr natural UI-quality evaluation

This evaluation asks a fresh coding agent to build the same product against one
or more GoFastr framework snapshots. It measures what the framework, scaffold,
embedded docs, component catalog, and normal generated agent guidance produce
naturally.

It does **not** install an evaluator-authored design prompt in the candidate.
The task brief describes product requirements only. Each builder receives the
ordinary output of that snapshot's `gofastr init`, including `DESIGN.md`,
`AGENTS.md`, `CLAUDE.md`, `agents/`, and the normal host skill. The evaluator
expects the builder to complete the app-owned `DESIGN.md`, but fails the
candidate if it mutates framework-owned `AGENTS.md`, `CLAUDE.md`, `agents/`, or
the generated host skill.

The question is:

> Does this GoFastr snapshot naturally lead an ordinary agent to produce a
> refined, product-specific, mobile-credible application without app-owned CSS
> or hand-rolled structural UI?

## What varies

Each suite variant names a `framework_root`. That root supplies all of the
following as one treatment:

- the `gofastr init` scaffold;
- generated agent onboarding;
- embedded framework documentation;
- `framework/ui` and `core-ui` behavior;
- the Go module used to build the candidate.

Use two or more eligible framework snapshots for an actual comparison. A suite
with only `working-tree` is a quality certification run and cannot name a
competitive winner.

The old `instructions/composition-v1.md` through `composition-v5.md` files are
retained only to explain historical artifacts. The active suite does not load
them, copy them into candidates, or score them as framework variants.

## Agent backends

Builders can run with:

- Codex: `--builder-agent codex`
- OMP / GLM-5.2: `--builder-agent omp`
- Claude Code / Opus: `--builder-agent claude`

Visual judges can run with Codex or Claude Code. OMP is intentionally not a
judge backend because the locally available GLM-5.2 catalog entry does not
accept image evidence. A common mixed run uses OMP for implementation and Codex
or Claude for independent visual judging.

Model and CLI versions are recorded in `manifest.json` and candidate provenance
for historical reproducibility. They are not promoted into the visible quality
result or used as variant names.

## Integrity model

- Every builder and judge is a fresh, non-resumed process.
- Builders have a neutral 25-minute implementation budget and at most two
  bounded visual-review passes; the main suite enforces a 30-minute ceiling.
  The instruction limits runtime, not design.
- Timeout cancellation and normal process exit terminate the agent's complete
  process tree on Windows and Unix, so temporary browser helpers and `go run`
  servers cannot survive into another cell.
- Codex builders keep normal repository rules enabled. Claude loads generated
  project settings. OMP runs in the generated project without disabling its
  rules.
- Agent processes receive only their own backend credential (when the CLI needs
  one); unrelated cloud, repository, package, and signing credentials are
  stripped. Generated-app tests, builds, and runtime use a smaller allowlisted
  environment with an isolated home directory and no agent credential.
- Candidates are isolated Git workspaces containing the normal scaffold plus a
  neutral `EVAL_TASK.md`.
- The builder prompt does not prescribe layouts, aesthetics, component choices,
  or exceptions to GoFastr's ownership rules.
- The generated onboarding fingerprint is captured before the builder and
  checked afterward. Mutation is contamination and fails the candidate.
- The framework snapshot and built CLI are fingerprinted before the matrix and
  checked after every builder and candidate. Any mutation aborts the run before
  a contaminated framework can reach another cell.
- Each candidate's module points at its declared framework snapshot.
- The harness supplies judges only opaque screenshot evidence, the scoring
  schema, and the rubric; source, framework variant names, builder output, and
  other judges' results are omitted from their workspaces and prompts.
- Screenshot pixels and text are explicitly treated as untrusted candidate
  output. UI-borne requests to change scores or ignore the rubric are a severe
  product defect, never judge instructions.
- Holistic and mobile panels are independent. Mobile quality cannot be averaged
  away by desktop quality.
- Judge output is schema- and semantics-validated and retried once if invalid.

This is a reproducible local evaluation protocol, not an OS security boundary.
Do not place secrets in candidate or judge workspaces.

On a host with TLS interception (corporate proxy or AV web shield), export
`NODE_EXTRA_CA_CERTS` with the interception root before running: it passes
through to agent processes untouched. The runner deliberately never probes
for machine-specific certificate paths itself.

## Gates

Before visual judging, every candidate must pass:

- `go test ./...` and `go build .`;
- `/healthz` and every required route;
- desktop and real mobile-emulation captures in light and dark themes;
- exact viewport and screenshot-size checks;
- CSP-safe animation/transition freezing before each forced color scheme,
  synchronized HTML/native-UA scheme state, plus font/image settling;
- document overflow and visible-bounds audits;
- interactive-label contrast checks.

The configured quality bar is a weighted score of 8.5 or higher in both panels,
every dimension at least 7.5, strict-majority quality verdicts in both panels,
and all technical gates passing. It is an internal calibration target, not an
official third-party certification.

## Run it

From the repository root:

```powershell
# Validate the protocol and candidate matrix without agent calls.
go run ./evals/ui-quality/cmd/gofastr-ui-eval --dry-run

# Natural GoFastr build with OMP / GLM-5.2 and the default Claude judges.
go run ./evals/ui-quality/cmd/gofastr-ui-eval --smoke `
  --builder-agent omp

# Claude Code / Opus for implementation and both visual panels.
go run ./evals/ui-quality/cmd/gofastr-ui-eval --smoke `
  --builder-agent claude `
  --judge-agent claude `
  --mobile-judge-agent claude

# Default Claude Code / Opus configuration from suite.json.
go run ./evals/ui-quality/cmd/gofastr-ui-eval --smoke

# Re-run gates, captures, and judges without rebuilding an unchanged candidate.
go run ./evals/ui-quality/cmd/gofastr-ui-eval --smoke `
  --run-id <existing-run-id> --reuse-workspaces
```

To compare snapshots, copy `suite.json` and declare two roots:

```json
"variants": [
  {"id": "before", "framework_root": "C:/path/to/clean-before"},
  {"id": "working-tree", "framework_root": "../.."}
]
```

Then run with `--suite <copy.json>`. Keep scenario briefs and the evaluator
source identical across the comparison.

Role-specific `--builder-bin`, `--judge-bin`, `--mobile-judge-bin`, model, and
repeatable `--*-prefix-arg` flags support wrappers and alternate installations.

## Artifacts

Runs are written under `dist/ui-eval/<run-id>/`:

- `protocol/` — effective suite, rubric, schema, and evaluator fingerprint;
- `manifest.json` — private candidate-to-framework mapping and provenance;
- `workspaces/` — generated candidate source trees;
- `blind/` — viewport and full-page evidence;
- `judge-workspaces/` and `judge-artifacts/` — source-free judge inputs/results;
- `results/` — builder logs, gates, and per-candidate results;
- `summary.json` and `leaderboard.md` — aggregate machine/human output.

Workspace reuse is accepted only when the framework snapshot, neutral task,
builder prompt/backend, scaffold, generated guidance, and produced source still
match their stored fingerprints.

The CLI supplies source-free judge workspaces and fresh agent processes, but it
does not create a separate OS user or VM. Filesystem blinding therefore assumes
the configured coding agents follow the evaluation instruction not to traverse
outside their workspace. Use an external container/VM boundary or human
confirmation when evaluating deliberately adversarial agents rather than normal
framework-guided generation.

Historical prompt-treatment scores and why they were invalidated are documented
in [RESULTS.md](RESULTS.md).
