#!/usr/bin/env python3
"""Run three independent reviews of a change and block only on 3/3.

The reviewer is the Codex CLI when it is installed. Without Codex it is
the Claude Code CLI (`claude -p`) on Haiku, read-only.

Usage:
  review3.py [--range A...B | --staged | --uncommitted] [--model M]
             [--codex PATH] [--claude PATH] [--json] [--check]
             [--timeout SECONDS]

Exit codes: 0 approve, 1 block (all three runs request changes),
2 fewer than three usable answers or any other failure (a git error, a
crash), 3 no reviewer found, neither Codex nor Claude (--check or a
run). Only a vote exits 1.

The repo's pre-commit hook runs it with --staged in the background
while its test sweep runs; see .githooks/pre-commit.
"""

import argparse
import contextlib
import glob
import json
import os
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time
from concurrent.futures import ThreadPoolExecutor

HERE = os.path.dirname(os.path.abspath(__file__))
RUNS = 3
DIFF_CAP = 120_000  # characters of diff pasted into the prompt
MAC_APPS = "/Applications"  # a module name, so the tests can move it

# Live codex processes, so a SIGTERM (the hook giving up) stops them
# instead of leaving them to run out their reviews.
_children = []
# Reentrant: the signal handler takes it on the main thread, and a second
# signal can arrive while the first handler holds it.
_children_lock = threading.RLock()
_stopping = False
KILL = getattr(signal, "SIGKILL", signal.SIGTERM)


def _signal_tree(p, sig):
    """Signal a codex run and the commands it started. On POSIX each run
    leads its own process group; Windows has no groups to signal."""
    try:
        if os.name == "posix":
            os.killpg(p.pid, sig)
        elif sig == signal.SIGTERM:
            p.terminate()
        else:
            p.kill()
    except (ProcessLookupError, PermissionError):
        pass


def _stop(signum, _frame):
    global _stopping
    if _stopping:
        return  # the first handler is already stopping the runs
    with _children_lock:
        _stopping = True
        live = list(_children)
    for p in live:
        _signal_tree(p, signal.SIGTERM)
    deadline = time.monotonic() + 3
    for p in live:
        try:
            p.wait(max(0.1, deadline - time.monotonic()))
        except subprocess.TimeoutExpired:
            pass
        _signal_tree(p, KILL)
    # os._exit, not sys.exit: SystemExit would unwind into the thread
    # pool's shutdown, which waits for every worker to return.
    os._exit(128 + signum)


def fail(msg):
    """Exit 2 with msg on stderr: a failure is never a vote to block."""
    print(f"review-gate: {msg}", file=sys.stderr)
    sys.exit(2)


def git(*args, check=True):
    out = subprocess.run(["git", *args], capture_output=True, text=True,
                         encoding="utf-8", errors="replace")
    if check and out.returncode != 0:
        fail(f"git {' '.join(args)} failed: {out.stderr.strip()}")
    return out.stdout if out.returncode == 0 else None


def _version_key(path, depth):
    """Sort key for the version directory depth levels above path:
    v22.10.0 after v22.9.1, a name with no digits first."""
    d = path
    for _ in range(depth):
        d = os.path.dirname(d)
    name = os.path.basename(d).lstrip("v")
    return tuple(int(p) if p.isdigit() else -1 for p in name.split("."))


def codex_install_globs():
    """Where codex is usually installed when PATH does not reach it, in
    the order tried, as (glob, depth) pairs: depth is how many levels
    above a match its version directory sits, 0 for a fixed path. On
    macOS the ChatGPT app's own codex CLI comes first: it updates with the
    app and needs no node. nvm keeps one global bin per node version, so a
    codex installed under an older node drops off PATH when the default
    moves to a newer one; every installed version is tried, newest first."""
    home = os.path.expanduser("~")
    spots = []
    if sys.platform == "darwin":
        for apps in (MAC_APPS, os.path.join(home, "Applications")):
            spots.append((os.path.join(apps, "ChatGPT.app", "Contents", "Resources",
                                       "codex-cli", "bin", "codex"), 0))
    nvm = os.environ.get("NVM_DIR") or os.path.join(home, ".nvm")
    spots.append((os.path.join(nvm, "versions", "node", "*", "bin", "codex"), 2))
    if os.name == "nt":
        if os.environ.get("NVM_HOME"):
            spots.append((os.path.join(os.environ["NVM_HOME"], "v*", "codex.cmd"), 1))
        if os.environ.get("APPDATA"):
            spots.append((os.path.join(os.environ["APPDATA"], "npm", "codex.cmd"), 0))
    return spots


def codex_install_spots():
    """The files codex_install_globs matches, newest version first."""
    found = []
    for pattern, depth in codex_install_globs():
        if depth:
            found += sorted(glob.glob(pattern), key=lambda p, d=depth: _version_key(p, d),
                            reverse=True)
        else:
            found.append(pattern)
    return found


def find_codex(explicit):
    """--codex, then CODEX_BIN, then PATH, then codex_install_spots."""
    for cand in (explicit, os.environ.get("CODEX_BIN")):
        if cand and os.path.isfile(cand):
            return cand
    found = shutil.which("codex")
    if found:
        return found
    for cand in codex_install_spots():
        if os.path.isfile(cand) and (os.name == "nt" or os.access(cand, os.X_OK)):
            return cand
    return None


def codex_missing():
    """The not-found message, naming every place that was tried."""
    tried = ["--codex", "CODEX_BIN", "PATH"] + [g for g, _ in codex_install_globs()]
    return "codex not found (tried " + ", ".join(tried) + "): set CODEX_BIN or pass --codex"


def find_claude(explicit):
    """The fallback reviewer: --claude, then CLAUDE_BIN, then PATH."""
    for cand in (explicit, os.environ.get("CLAUDE_BIN")):
        if cand and os.path.isfile(cand) and (os.name == "nt" or os.access(cand, os.X_OK)):
            return cand
    return shutil.which("claude")


def reviewer_missing():
    return codex_missing() + "; claude not found either (tried --claude, CLAUDE_BIN, PATH)"


def default_range():
    """Unpushed commits: upstream...HEAD, else the remote's default branch."""
    up = git("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}", check=False)
    if up and up.strip():
        return up.strip() + "...HEAD"
    head = git("symbolic-ref", "--short", "refs/remotes/origin/HEAD", check=False)
    if head and head.strip():
        return head.strip() + "...HEAD"
    return None


def build_prompt(rng, mode, diff_file=None):
    """The filled prompt. A diff past DIFF_CAP is summarized: the reviewer
    reads the rest with git, or from diff_file when it cannot run git."""
    top = git("rev-parse", "--show-toplevel").strip()
    repo = os.path.basename(top)
    if mode == "staged":
        rng = "--cached"
        log = "(staged for the next commit, which has no message yet)"
    elif mode == "uncommitted":
        rng = "HEAD"
        log = "(uncommitted changes, not yet committed)"
    else:
        log = git("log", "--format=%h %s%n%n%b", rng.replace("...", "..")).strip() or "(none)"
    diff = git("diff", rng)
    if not diff.strip():
        return None, rng
    if len(diff) > DIFF_CAP:
        stat = git("diff", "--stat", rng)
        how = f"read the full diff with `git diff {rng}`"
        if diff_file:
            with open(diff_file, "w", encoding="utf-8") as f:
                f.write(diff)
            how = f"read the full diff in the file {diff_file}"
        diff = (f"The diff is too long to paste here. Its summary is below; {how}.\n\n" + stat)
    else:
        diff = "```diff\n" + diff + "\n```"
    with open(os.path.join(HERE, "prompt.md"), encoding="utf-8") as f:
        text = f.read()
    for key, val in (("{repo}", repo), ("{range}", rng), ("{log}", log), ("{diff}", diff)):
        text = text.replace(key, val)
    return text, rng


def codex_env():
    """The environment for a codex run, without git's per-repo variables.
    The hook hands review3.py the commit's index in GIT_INDEX_FILE so
    --staged reads it; a reviewer that ran git, or a test that runs git,
    in any repository would read and write that index instead of its own.
    A review on 2026-11-08 ran this package's tests, and their `git add`
    in a temp repo left an entry with no object in the commit's index."""
    names = set((git("rev-parse", "--local-env-vars", check=False) or "").split())
    names |= {"GIT_INDEX_FILE", "GIT_DIR", "GIT_WORK_TREE", "GIT_OBJECT_DIRECTORY",
              "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_PREFIX"}
    return {k: v for k, v in os.environ.items() if k not in names}


# What a Claude reviewer may do: read files, and nothing else. No Bash,
# not even git: `git diff --output=<path>` writes a file, so no git
# allowlist is read-only. dontAsk refuses everything else without a
# prompt. A diff too long for the prompt is saved for it to Read.
CLAUDE_TOOLS = ["Read", "Grep", "Glob"]
CLAUDE_NOTE = ("\n\nIn this session you can read and search files but cannot run "
               "commands. Judge the change from the diff and the files.\n")


def command(kind, binary, model, out_file, workdir=None):
    if kind == "claude":
        # The answer comes on stdout; the prompt goes in on stdin. No
        # user or project settings, no MCP servers, nothing saved.
        cmd = [binary, "-p", "--model", model,
               "--tools", ",".join(CLAUDE_TOOLS),
               "--allowedTools", *CLAUDE_TOOLS,
               "--permission-mode", "dontAsk",
               "--add-dir", workdir or os.path.dirname(out_file),
               "--setting-sources", "", "--strict-mcp-config",
               "--no-session-persistence"]
    else:
        cmd = [binary, "exec", "-s", "read-only", "-m", model,
               "-c", 'model_reasoning_effort="high"',
               "-c", 'web_search="disabled"',
               "--ephemeral", "--ignore-user-config",
               "-o", out_file, "-"]
    if os.name == "nt" and binary.lower().endswith((".cmd", ".bat")):
        cmd = ["cmd", "/c", *cmd]
    return cmd


def parse(text):
    """The answer's last JSON object with a decision, or None."""
    dec = json.JSONDecoder()
    found = None
    for i, ch in enumerate(text):
        if ch != "{":
            continue
        try:
            obj, _ = dec.raw_decode(text, i)
        except ValueError:
            continue
        if isinstance(obj, dict) and obj.get("decision") in ("approve", "request_changes"):
            found = obj
    return found


def run_one(n, kind, binary, model, prompt, timeout, workdir, env):
    out_file = os.path.join(workdir, f"run{n}.txt")
    log_file = os.path.join(workdir, f"run{n}.log")
    with open(log_file, "w", encoding="utf-8") as log, contextlib.ExitStack() as stack:
        # codex writes its answer to out_file itself; claude prints it.
        if kind == "claude":
            out = stack.enter_context(open(out_file, "w", encoding="utf-8"))
            err = log
        else:
            out, err = log, subprocess.STDOUT
        try:
            proc = subprocess.Popen(command(kind, binary, model, out_file, workdir), stdin=subprocess.PIPE,
                                    stdout=out, stderr=err, env=env,
                                    text=True, encoding="utf-8",
                                    start_new_session=os.name == "posix")
        except OSError as exc:
            return {"run": n, "error": f"could not start {kind}: {exc}"}
        with _children_lock:
            _children.append(proc)
            if _stopping:
                _signal_tree(proc, KILL)
        try:
            proc.communicate(prompt, timeout=timeout)
        except subprocess.TimeoutExpired:
            _signal_tree(proc, KILL)
            proc.communicate()
            return {"run": n, "error": f"timed out after {timeout}s"}
    if proc.returncode != 0:
        # The reviewer exits 0 when the run finished; an answer left by a
        # run that then failed is not a vote.
        return {"run": n, "error": f"{kind} exited {proc.returncode}; see {log_file}"}
    try:
        with open(out_file, encoding="utf-8") as f:
            answer = parse(f.read())
    except OSError:
        answer = None
    if answer is None:
        return {"run": n, "error": f"no usable answer; see {log_file}"}
    findings = answer.get("findings")
    return {"run": n, "decision": answer["decision"],
            "findings": findings if isinstance(findings, list) else []}


def line_number(v):
    """A finding's line as an int, or None: models write 42, "42" or null."""
    if isinstance(v, bool):
        return None
    if isinstance(v, int):
        return v
    if isinstance(v, str) and v.strip().isdigit():
        return int(v.strip())
    return None


def merge(results):
    """Blocking findings from every run, keyed by path and line."""
    merged = {}
    for r in results:
        for f in r.get("findings", []):
            if not isinstance(f, dict) or not f.get("blocking", True):
                continue
            key = (str(f.get("path", "")), line_number(f.get("line")))
            entry = merged.setdefault(key, {"path": key[0], "line": key[1], "runs": [], "issues": []})
            entry["runs"].append(r["run"])
            entry["issues"].append(str(f.get("issue", "")).strip())
    return sorted(merged.values(), key=lambda e: (e["path"], e["line"] or 0))


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--range", help="git range, e.g. origin/main...HEAD")
    ap.add_argument("--staged", action="store_true", help="review git diff --cached")
    ap.add_argument("--uncommitted", action="store_true", help="review git diff HEAD")
    ap.add_argument("--model", help="reviewer model (default: PUSH_REVIEW_MODEL or gpt-6-luna "
                    "with Codex, PUSH_REVIEW_CLAUDE_MODEL or haiku with Claude)")
    ap.add_argument("--codex", help="path to the codex binary (or set CODEX_BIN)")
    ap.add_argument("--claude", help="path to the claude binary, used when there is no "
                    "codex (or set CLAUDE_BIN)")
    ap.add_argument("--timeout", type=int, default=900)
    ap.add_argument("--json", action="store_true", help="print the result as JSON")
    ap.add_argument("--check", action="store_true", help="only report which reviewer is available")
    a = ap.parse_args()

    kind, binary = "codex", find_codex(a.codex)
    if not binary:
        kind, binary = "claude", find_claude(a.claude)
    if a.check:
        print(binary or reviewer_missing())
        return 0 if binary else 3
    if not binary:
        print("review-gate: " + reviewer_missing(), file=sys.stderr)
        return 3
    if kind == "codex":
        model = a.model or os.environ.get("PUSH_REVIEW_MODEL", "gpt-6-luna")
    else:
        model = a.model or os.environ.get("PUSH_REVIEW_CLAUDE_MODEL", "haiku")

    mode = "staged" if a.staged else "uncommitted" if a.uncommitted else "range"
    rng = a.range or (default_range() if mode == "range" else None)
    if not rng and mode == "range":
        fail("no upstream or origin/HEAD; pass --range, --staged or --uncommitted")
    for sig in (signal.SIGTERM, signal.SIGINT):
        signal.signal(sig, _stop)
    workdir = tempfile.mkdtemp(prefix="review-gate-")
    claude = kind == "claude"
    prompt, rng = build_prompt(rng, mode, os.path.join(workdir, "full.diff") if claude else None)
    if prompt is None:
        print(f"review-gate: nothing to review in {rng}")
        return 0
    if claude:
        prompt += CLAUDE_NOTE
    with ThreadPoolExecutor(RUNS) as pool:
        env = codex_env()
        results = list(pool.map(lambda n: run_one(n, kind, binary, model, prompt, a.timeout, workdir, env),
                                range(1, RUNS + 1)))

    usable = [r for r in results if "decision" in r]
    votes = sum(r["decision"] == "request_changes" for r in usable)
    if len(usable) < RUNS:
        decision, code = "incomplete", 2
    elif votes == RUNS:
        decision, code = "block", 1
    else:
        decision, code = "approve", 0
    findings = merge(usable)

    if a.json:
        print(json.dumps({"decision": decision, "range": rng, "reviewer": kind, "model": model,
                          "votes": votes, "usable": len(usable), "runs": results,
                          "findings": findings, "logs": workdir}, indent=1))
        return code
    print(f"review-gate: {decision} ({votes}/{len(usable)} usable runs request changes) "
          f"on {rng}, {kind} model {model}")
    for r in results:
        if "error" in r:
            print(f"  run {r['run']}: {r['error']}")
    for f in findings:
        where = f"{f['path']}:{f['line']}" if f["line"] else f["path"]
        print(f"- {where} (runs {', '.join(map(str, f['runs']))})")
        for issue in dict.fromkeys(i for i in f["issues"] if i):
            print(f"    {issue}")
    print(f"logs: {workdir}")
    return code


if __name__ == "__main__":
    try:
        code = main()
    except SystemExit as exc:
        # argparse and fail() exit with an int; a bare string would exit 1,
        # which the hook reads as a block.
        if not isinstance(exc.code, int):
            print(exc.code, file=sys.stderr)
            code = 2
        else:
            code = exc.code
    except Exception:  # noqa: BLE001 - a crash must not read as a block
        import traceback
        traceback.print_exc()
        code = 2
    sys.exit(code)
