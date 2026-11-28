---
name: cloud-review
description: Adversarial review of a GoFastr pull request or branch from a Claude cloud session. Use when asked for a cloud review, an adversarial review, or to review a PR or branch before merge.
---

# Cloud review (gofastr, cloud sessions only)

Reviewers try to break the change, not confirm it. Findings stay in this session:
write no review file and post nothing to GitHub unless the user asks.

## 1. Scope
- A PR number: read it with the GitHub MCP tools (pull_request_read: diff, files).
- A branch: `git fetch origin main` and `git diff origin/main...HEAD`.
- For each touched path run `go run ./cmd/primitives for <path>`; note its invariants.

## 2. Reviewers
- Split the diff into 2 to 4 areas by package or primitive.
- Spawn one fresh general-purpose Agent per area, all in one message, in the background.
  Never a fork, and never tell them what you think is wrong: they get the code, not your view.
- Each brief: the diff range, the area's files, AGENTS.md and docs/contributing.md to read,
  and the order to break it. Hunt for: behaviour bugs and races, callers the change breaks
  (CSS, JS, tests, examples, generator), accessibility, security (HTML sinks outside the
  Trusted Types policy, unsanitised input, scope and ownership), vacuous tests,
  and docs or registry text that the code does not match.
- Reviewers edit nothing in the repo. Probes and throwaway tests go in the scratchpad.
- Each finding: severity (blocker, major, minor, nit), file:line, what breaks,
  how it was verified (test, probe, or unverified), a suggested fix.

## 3. Verify
- Reproduce every finding yourself before you accept it: a test or probe that fails.
- Mark each one CONFIRMED, PLAUSIBLE or REJECTED, with the reason.
- A test that passes with its guard removed is a finding too: break the guard, run it.

## 4. Report in the session
- A table ranked by severity: finding, file:line, evidence, verdict.
- Then the areas the reviewers checked and found sound.
- Write no review file. Findings and their triage belong on the PR, not in the
  repo (contributing rule 13), and are posted there only when the user asks.

## 5. Fixing, only when asked
- Failing test first, then the fix, then prove the guard: break it, watch the test fail,
  put it back (Hard rule 11).
- Run the affected tests: `go run ./cmd/affected -- go test -timeout 40m`.
