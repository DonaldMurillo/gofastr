---
name: omp-workers
description: Dispatch implementation work, bug fixes, test suite runs, or repetitive coding tasks to background oh-my-pi (omp) worker agents instead of doing it inline or spawning expensive internal subagents. Use when the user says "send it to omp", "omp workers", "oh my pi agents", "pi agents", "use the cheap agents", or asks to delegate builds/fixes to background workers while Antigravity plans, reviews, and verifies. omp is the user's locally configured multi-provider agent CLI.
---

# omp workers — dispatch background build jobs to oh-my-pi

Division of labor: **Antigravity plans, scopes, and verifies; omp workers type.**
Write each worker a self-contained brief, launch it headless in the background, and adversarially review + re-run the gates yourself when it reports back. Never treat a worker's "all green" as verified.

## The dispatch pattern

1. **Write the brief to a file** (scratchpad, one file per worker):
   Store in `<appDataDir>/brain/<conversation-id>/scratch/brief-<name>.md` or `<workspace>/scratch/brief-<name>.md`.

   The brief must be fully self-contained — omp does not see your conversation. Always include:
   - repo path + branch, and "do NOT commit; leave changes uncommitted"
   - pointer to repo ground rules (`CLAUDE.md`, relevant ARCHITECTURE docs)
   - the task, with file:line anchors you already identified
   - explicit file ownership: which files it owns, which files other concurrent workers own ("do NOT touch X — another agent owns it")
   - the exact verification commands to run (and which test suites must run ISOLATED), plus known gotchas (e.g. `chromedp.Submit` doesn't fire submit — use Click)
   - "You are running headless — work autonomously until done, then print a final report as your last message" and what the report must contain
   - "Halt and end your run instead of guessing if you hit a decision you cannot make from the brief + repo" — headless workers can't ask questions
   - "Do the work yourself in this process; do NOT dispatch omp sub-workers or spawn subagents."
   - for review/critique/assessment briefs: paste the body of `intellectual-honesty/SKILL.md` into the brief. Cheap models are sycophancy-prone — without the rubric they hedge ("both approaches have merit"), bury problems in caveats, and rate work green to please. Workers can't load Antigravity/Claude skills, so the text must travel in the brief itself.

2. **Launch headless, in the background**:
   By default, run commands sandboxed. When dispatching `omp`, external LLM API network calls and writes to `~/.omp/` SQLite databases require unsandboxed execution; only use `BypassSandbox: true` and `--auto-approve` when operating on trusted repositories and tasks where the user has explicitly requested or approved external worker dispatch.

   ```sh
   (
     omp -p --auto-approve --max-time 5400 \
       --cwd /path/to/repo \
       @/path/to/scratch/brief-<name>.md \
       > /path/to/scratch/omp-<name>.log 2>&1
     echo "OMP_EXIT=$?" >> /path/to/scratch/omp-<name>.log
   ) &
   ```

   Flag notes (verified against omp v18.x):
   - `-p` / `--print`: non-interactive, exits when done. The final assistant message is the stdout tail — that's the worker's report.
   - `--auto-approve`: REQUIRED headless; without it tool approvals stall. (`--approval-mode=yolo` is the equivalent long form.)
   - `@file`: includes the brief file as the prompt message without shell-quoting hazards.
   - `--cwd <repo>`: set the working directory explicitly; don't rely on the launch cwd.
   - `--max-time <seconds>`: set a ceiling so a stuck worker can't run forever (3600–7200 for build tasks).
   - `--model <fuzzy>`: override per task tier if needed ("glm", "opus", "gpt-5.2", `provider/id` also works). Omit to use the user's configured default role (`~/.omp/agent/config.yml`).
     - **Sol lives on the OpenAI Codex provider**: use `--model openai-codex/gpt-5.6-sol` (the bare fuzzy name `gpt-5.6-sol` fails with a login error; it is NOT on github-copilot anymore). Codex quota is a 7-day window — check `omp usage` before dispatching a long Sol run.
     - **`zai/glm-5.3` (default)** is configured in `~/.omp/agent/models.db`.
   - `--thinking minimal|low|medium|high|xhigh|max`: raise for gnarly tasks, lower for mechanical sweeps.
   - Sessions are saved by default → keep them (skip `--no-session`) so a failed worker can be resumed: `omp --resume <id-prefix> -p "..."` or `omp -c -p "..."` for the most recent. `omp --export <session.jsonl>` renders a session to HTML.
   - `omp worktree` lists/clears agent-managed worktrees under `~/.omp/wt` if a worker was asked to isolate in one.

3. **Monitor**:
   Antigravity will be reactively notified when the background command finishes.
   Check the log tail: inspect `/path/to/scratch/omp-<name>.log` and grep for the `OMP_EXIT=` line.
   Exit 0 + a report is normal; a truncated log with no report means it hit the time cap or died — resume the session rather than re-dispatching from scratch.

4. **Verify like an adversary** (non-negotiable):
   Diff what the worker actually changed (`git status` / `git diff`), re-run the gates it claims are green yourself (e.g., `make analyze`, `go test`), and review the diff for the failure modes cheap models favor: tests weakened to pass, allowlists/budgets raised instead of causes fixed, sibling cases of the fixed bug left untouched, bespoke workarounds where an upstream fix belongs. Fix-forward small issues yourself; resume the worker's session for large ones.

## Parallelism rules

- **Hard cap: 5 workers running at once.** The provider rate-limits beyond that. Split bigger jobs into more slices if needed, but hold the extras in a queue and launch each one only as a running worker finishes. Never fire a batch larger than 5.
- **The cap counts every omp process, nested ones included — and workers must never nest.** Put this line in every brief: "Do the work yourself in this process; do NOT dispatch omp sub-workers or spawn subagents."
- One worker per disjoint file set. State ownership in every brief when workers run concurrently.
- Workers editing the same working tree must never share files; there is no merge step. If two tasks genuinely overlap, serialize them.
- Don't let two workers (or a worker and yourself) run chromedp/e2e suites at the same time — flaky-under-load suites will lie to both of you. Tell workers to run heavy suites once, at the end.
- Never run `git stash`/checkout/branch switches while workers are live.

## When NOT to dispatch

- Tasks needing judgment calls mid-flight (design decisions, ambiguous contracts) — do those yourself or expect a halt-and-report.
- Tiny edits where the brief would be longer than the diff.
- Anything requiring credentials/tools omp doesn't have.

## Quick smoke test

```sh
omp -p --no-session --max-time 90 --auto-approve \
  "Run 'go version' with your bash tool and tell me the output."
```
