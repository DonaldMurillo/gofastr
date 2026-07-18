# UI capability-discovery evaluation

This suite measures whether a cold-start coding agent can begin with a product
problem, discover GoFastr's UI architecture, and produce a working application.
It dispatches fresh Claude Code Opus processes for the builder, holistic visual
panel, and mobile visual panel.

The evaluator does not tell the builder to use the capability map, name a
component, choose an island/store/SSE architecture, or run a particular
command. The candidate receives the snapshot's ordinary `gofastr init` output
and a product brief. That keeps documentation discovery part of the treatment.

## What it measures

- whether the Claude Opus builder invoked `gofastr docs`;
- the topics it opened and `--grep` queries it used;
- whether it opened `ui-capability-map`;
- whether the generated app tests, builds, starts, and serves every required
  route;
- desktop/mobile and light/dark capture gates, overflow, visible bounds, and
  interactive-label contrast;
- independent Claude Opus visual scores for hierarchy, composition,
  typography, product specificity, density, component polish, responsive
  intent, and theme coherence;
- the existing dev-loop and MCP discovery signals.

Runs are written under `dist/ui-capability-eval/<run-id>/`. Each candidate
result records the docs evidence beside build and judge results; the
leaderboard aggregates capability-map discovery and documentation-call rates
per framework snapshot.

## Validate without agents

From the repository root:

```powershell
go run ./evals/ui-quality/cmd/gofastr-ui-eval `
  --suite ./evals/ui-capability-discovery/suite.json `
  --dry-run
```

## Smoke run

The checked-in suite is a one-snapshot certification run:

```powershell
go run ./evals/ui-quality/cmd/gofastr-ui-eval `
  --suite ./evals/ui-capability-discovery/suite.json `
  --smoke
```

That dispatches one Claude Opus builder plus one holistic and one mobile Opus
judge for the first scenario.

## Compare before and after

For causal evidence, create two clean framework worktrees at the commits being
compared, copy `suite.json`, and replace `variants`:

```json
"variants": [
  {"id": "before", "framework_root": "C:/worktrees/gofastr-before"},
  {"id": "capability-map", "framework_root": "C:/worktrees/gofastr-after"}
]
```

Then run the full matrix:

```powershell
go run ./evals/ui-quality/cmd/gofastr-ui-eval `
  --suite C:/path/to/ui-capability-comparison.json `
  --runs 2
```

Use the same suite, scenarios, Claude Opus versions, run counts, and evaluator
source for both snapshots. A one-variant or smoke run is diagnostic and cannot
name a competitive winner.

## Interpreting discovery

Opening `ui-capability-map` is useful funnel evidence, not a quality pass by
itself. An agent can read the right guide and still build a weak product; it
can also reach a sound result through another valid doc path. The outcome is
therefore reported as three layers:

1. discovery (`gofastr docs` calls, searches, and topics);
2. technical product outcome (test/build/boot/routes/captures);
3. visible product outcome (blind holistic and mobile panels).

Do not promote a documentation change from discovery rate alone.
