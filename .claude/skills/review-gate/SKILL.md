---
name: review-gate
description: Review a change before it is committed or pushed with three independent reviews that block only when all three request changes. Use when asked to review before committing or pushing, gate a push, check "is this safe to push", explain or skip a block from the pre-commit hook's model review, or run a pre-push review. Runs three Codex reviews through review3.py when the Codex CLI is available, and three subagent reviews (or one advisory review) when it is not.
---

# Review gate

Three reviews of the same change, same prompt, same model, each started
fresh. Block the push only if all three request changes. Report the
blocking findings from every run, not just the ones they agree on.

## Pick a mode

Run `python3 .claude/skills/review-gate/review3.py --check` (on Windows,
`python`). It prints the Codex binary it found and exits 0, or exits 3.
It looks at `--codex PATH`, then `CODEX_BIN`, then `PATH`, then the
usual install spots: on macOS the ChatGPT app's own CLI
(`ChatGPT.app/Contents/Resources/codex-cli/bin/codex` under
`/Applications` or `~/Applications`), then every nvm node version newest
first (`$NVM_DIR/versions/node/*/bin/codex`), and on Windows nvm-windows
(`%NVM_HOME%\v*\codex.cmd`) and npm's global dir
(`%APPDATA%\npm\codex.cmd`). The exit-3 message names each place tried.

- **Exit 0: script mode.** Use it.
- **Exit 3, or you cannot run commands or Python: subagent mode.**
- **You cannot start other agents either: single mode.** One review,
  advisory only.

## Script mode

1. Run `review3.py` from the repository. With no arguments it reviews the
   unpushed commits, `@{upstream}...HEAD`, falling back to
   `origin/HEAD...HEAD` on a branch with no upstream. Pass
   `--range A...B` for another range, `--staged` for what the next
   commit holds (`git diff --cached`), or `--uncommitted` for
   `git diff HEAD`.
2. It starts three `codex exec` sessions in parallel: read-only sandbox,
   model `gpt-6-luna` (`--model` or `PUSH_REVIEW_MODEL` to change),
   high effort, web search off, user config ignored, nothing saved.
3. It prints the decision, the vote and every blocking finding with the
   runs that raised it. `--json` prints the same as JSON.
4. Exit codes: 0 approve, 1 block, 2 fewer than three usable answers or
   any other failure (a git error, a codex that will not start, a crash),
   3 no Codex. Only the vote exits 1. On 2, read the run logs it names,
   then rerun or fall back to subagent mode. Never count a failed run as
   a vote either way.

## In the pre-commit hook

`.githooks/pre-commit` runs `review3.py --staged` on every commit, so
an agent that commits gets the review without asking for it:

- It starts after vet and the repo analyzers pass and runs beside the
  test sweep, so it adds little to the hook's time. Its result prints
  last.
- 3 of 3 request changes fails the commit and prints the findings.
- No Codex fails the commit too, before the test sweep, and names the
  places it looked. Install the Codex CLI or set `CODEX_BIN`; to commit
  without the review, use `GOFASTR_SKIP_COMMIT_REVIEW=1` and say why in
  the commit message.
- Anything else lets the commit through: an approve, an incomplete
  review, or no Python.
- A run that takes longer than ten minutes counts as incomplete
  (`GOFASTR_COMMIT_REVIEW_TIMEOUT` sets the seconds). If the hook exits
  early, it stops the review and the commands its runs started.
- It skips merge, cherry-pick and revert commits, and commits that stage
  only Markdown.
- It reviews the index the commit is built from, so `git commit <paths>`
  is reviewed as committed.
- The codex runs start without git's per-repo variables
  (`GIT_INDEX_FILE`, `GIT_DIR` and the rest): a reviewer that runs git or
  the tests must not write into the commit's index.

When it blocks: fix the findings and commit again. If you read a finding
and it is wrong, commit with `GOFASTR_SKIP_COMMIT_REVIEW=1` and say in the
commit message which finding you rejected and why. Never use
`--no-verify`: it skips every other check too.

## Subagent mode

1. Work out the range the same way the script does, and fill in
   `prompt.md` yourself: `{repo}` is the repository's directory name,
   `{range}` the range, `{log}` the output of
   `git log --format='%h %s%n%n%b' <A>..<B>`, and `{diff}` the output of
   `git diff <range>` inside a diff fence. If the diff is over about
   120,000 characters, put `git diff --stat <range>` there instead, with
   a line telling the reviewer to read the full diff with git.
2. Start three reviewers. Give each one the filled prompt and nothing
   else: no conversation history, no summary of the change, no hint of
   what you suspect. Each must be able to read the repository and run
   git. Tell them not to edit files.
   - Use the same model for all three. If you can delegate to a Codex
     model, use `gpt-6-luna`.
   - In Claude Code, use the Agent tool with an explicit `model`.
   - If your environment caps how many agents run at once, run them in
     batches. The three must be independent; they need not be concurrent.
3. Read each answer's JSON object. An answer with no JSON object, or a
   reviewer that failed, is not a vote: start a replacement until you
   have three answers.
4. Vote and report as in script mode, and say the result came from
   subagent mode.

## Single mode

Review the change yourself against the filled `prompt.md`, in the same
JSON shape. Report it as one vote: list the findings, recommend a look at
any blocking one, and do not call it a block.

## Report

- **3 of 3 request changes:** block. List every blocking finding with
  the runs that raised it. A finding only one run raised still belongs
  in the list.
- **2 of 3 or fewer:** approve. List the minority's blocking findings as
  worth a look, not as blockers.
- **Fewer than three usable answers:** say so. Do not decide on two.

## Do not

- Add a second pass that verifies or re-judges the findings.
- Change the prompt to a skeptic prompt, a checklist, or a different lens
  per run. All three runs get the same plain prompt.
- Add extra context to the prompt, such as fact packets or code-graph
  pointers.
- Run the review as one request with no tools.
- Use `codex exec review` with only P0 and P1 counted as blocking. If you
  use it, count P2 as blocking.
