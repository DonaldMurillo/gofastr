---
name: chaos-test
description: Live-agent chaos testing — spawn diverse persona agents against the running dev server, each with playwright browser tools and a distinct exploration mandate. Use when the user says "chaos test", "stress test the UI", "find bugs by exploring", "monkey test the site", or invokes `/chaos-test`. Personas are non-deterministic explorers, NOT chromedp scripts — they make judgment calls, follow surprising paths, and report what they tried. Triggers on phrases like "chaos monkey", "exploratory testing", "stress the UI", "agentic testing".
---

# Chaos-test — live exploratory testing via diverse persona agents

When this skill is invoked, you are the **orchestrator**. The skill replaces
deterministic chromedp tests with live agents who actually drive a browser
and make judgment calls.

## When to use

Invoke this skill when:
- The user wants stress / chaos / exploratory testing of a running UI
- Deterministic tests miss real-world weirdness (rage clicks, weird inputs,
  edge-case keyboard flows, slow networks)
- You've shipped a UI change and want to surface what a curious user finds

Do NOT use this skill for:
- Adding regression tests (use chromedp in `examples/site/e2e_*.go`)
- Code review (use Agent with `general-purpose`, or a UX-review agent if your environment provides one)
- Static analysis of the codebase

## Pre-flight

Before spawning personas, confirm:

1. **The dev server is running** at the target URL (default `http://localhost:8082`).
   Check with `curl -s -o /dev/null -w "%{http_code}\n" <url>`. If not 2xx, ask
   the user to run `./scripts/dev-watch.sh` (or override target via args).
2. **A run directory exists**. Create `/tmp/gofastr-chaos/<ISO-timestamp>/`.
   Each persona writes a `<persona>.md` report there.

## Personas (7)

Spawn in parallel via Agent tool with `subagent_type=chaos-<persona-name>`:

| Persona | Mandate |
|---|---|
| `chaos-rage-clicker` | Spam clicks, double-clicks, drags. Surface race conditions and stuck UI states. |
| `chaos-keyboard-sherlock` | Tab/Shift-Tab/Enter/Esc only. Find focus traps, broken focus order, invisible focus. |
| `chaos-mobile-thumb` | 320/375/414 viewports. Hamburger, horizontal scroll, tap-target overlap, touch gestures. |
| `chaos-form-vandal` | Submit forms with emoji, RTL, 10MB strings, `<script>`, control chars, malformed dates. |
| `chaos-console-auditor` | Visit every reachable route. Capture console.error/warn, uncaught errors, CSP violations, 4xx/5xx network. |
| `chaos-network-pessimist` | Throttle network/CPU, drop requests, go offline. Check graceful degradation. |
| `chaos-screen-reader` | Use accessibility tree. Validate aria-live announcements, landmarks, focus announcements. |

Each agent has playwright browser tools and writes its findings to
`/tmp/gofastr-chaos/<run>/<persona>.md` in the structured format defined
in its own agent file.

## Spawning protocol

1. Confirm dev server is reachable.
2. Make the run directory.
3. Spawn all 7 personas in **parallel** via a single message with 7 Agent
   tool-use blocks. The dev server handles the concurrency. Each agent gets:
   - The target URL
   - The path to its report file
   - A 10-minute time budget (the agent decides when to stop)
4. Wait for all 7 to return.
5. Read every report file. Collate into a single triage doc at
   `/tmp/gofastr-chaos/<run>/summary.md` with sections:
   - **Critical findings** — bugs that crash, lose data, fail WCAG-A, or render
     content unreadable. Cite which persona found each + reproduction steps.
   - **Quality findings** — UX rough edges, missing affordances, perf glitches.
   - **What worked** — paths every persona reported as smooth (lets you see
     where the code is genuinely solid, not just where bugs are).
   - **Inter-persona corroboration** — findings reported by ≥2 personas get
     promoted (high-signal, multi-angle confirmation).
6. Present the summary to the user with a recommendation: fix all,
   triage tier-1 only, or accept as-is. The user picks.

## Pi delegation

If you have `mcp__pi__*` available and the orchestration phase needs:
- Cross-report deduplication (5 personas all flagged a contrast issue)
- Severity classification of a long list
- Summarization of repetitive findings

…delegate via `mcp__pi__pi_delegate` or `mcp__pi__pi_swarm`. The
browser-driving work stays with Claude Agents (playwright is a Claude MCP).

## Cost note

Seven parallel playwright sessions for ~5–10 min each = serious token spend.
Tell the user up front: "Round 7 of chaos-test fires 7 agents in parallel —
this'll take ~10 min and burn tokens. Proceed?" Default to yes if the user
already invoked the skill.

## Args

- `--target <url>` — override the default `http://localhost:8082`
- `--persona <name>` — run just one persona instead of all seven
- `--budget <min>` — per-agent time budget in minutes (default 10)

## What success looks like

A `summary.md` that:
1. Is short enough to read in 5 minutes (cap each tier at 7 items)
2. Names specific reproduction steps for every critical finding
3. Cites which persona surfaced each issue (so you can ask follow-ups)
4. Distinguishes "X is broken" from "X is a UX rough edge"
5. Names the things that worked, not just the bugs

Then leave the run directory in place — it's a paper trail.
