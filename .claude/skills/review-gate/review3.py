#!/usr/bin/env python3
"""Run three independent Codex reviews of a change and block only on 3/3.

Usage:
  review3.py [--range A...B | --staged | --uncommitted] [--model M]
             [--codex PATH] [--json] [--check] [--timeout SECONDS]

Exit codes: 0 approve, 1 block (all three runs request changes),
2 fewer than three usable answers or any other failure (a git error, a
crash), 3 Codex not found (--check or a run). Only a vote exits 1.

The repo's pre-commit hook runs it with --staged in the background
while its test sweep runs; see .githooks/pre-commit.
"""

import argparse
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


def find_codex(explicit):
    for cand in (explicit, os.environ.get("CODEX_BIN")):
        if cand and os.path.isfile(cand):
            return cand
    return shutil.which("codex")


def default_range():
    """Unpushed commits: upstream...HEAD, else the remote's default branch."""
    up = git("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}", check=False)
    if up and up.strip():
        return up.strip() + "...HEAD"
    head = git("symbolic-ref", "--short", "refs/remotes/origin/HEAD", check=False)
    if head and head.strip():
        return head.strip() + "...HEAD"
    return None


def build_prompt(rng, mode):
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
        diff = ("The diff is too long to paste here. Its summary is below; "
                f"read the full diff with `git diff {rng}`.\n\n" + stat)
    else:
        diff = "```diff\n" + diff + "\n```"
    with open(os.path.join(HERE, "prompt.md"), encoding="utf-8") as f:
        text = f.read()
    for key, val in (("{repo}", repo), ("{range}", rng), ("{log}", log), ("{diff}", diff)):
        text = text.replace(key, val)
    return text, rng


def command(codex, model, out_file):
    cmd = [codex, "exec", "-s", "read-only", "-m", model,
           "-c", 'model_reasoning_effort="medium"',
           "-c", 'web_search="disabled"',
           "--ephemeral", "--ignore-user-config",
           "-o", out_file, "-"]
    if os.name == "nt" and codex.lower().endswith((".cmd", ".bat")):
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


def run_one(n, codex, model, prompt, timeout, workdir):
    out_file = os.path.join(workdir, f"run{n}.txt")
    log_file = os.path.join(workdir, f"run{n}.log")
    with open(log_file, "w", encoding="utf-8") as log:
        try:
            proc = subprocess.Popen(command(codex, model, out_file), stdin=subprocess.PIPE,
                                    stdout=log, stderr=subprocess.STDOUT,
                                    text=True, encoding="utf-8",
                                    start_new_session=os.name == "posix")
        except OSError as exc:
            return {"run": n, "error": f"could not start codex: {exc}"}
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
    ap.add_argument("--model", default=os.environ.get("PUSH_REVIEW_MODEL", "gpt-6-luna"))
    ap.add_argument("--codex", help="path to the codex binary (or set CODEX_BIN)")
    ap.add_argument("--timeout", type=int, default=900)
    ap.add_argument("--json", action="store_true", help="print the result as JSON")
    ap.add_argument("--check", action="store_true", help="only report whether Codex is available")
    a = ap.parse_args()

    codex = find_codex(a.codex)
    if a.check:
        print(codex or "codex not found: set CODEX_BIN or pass --codex")
        return 0 if codex else 3
    if not codex:
        print("review-gate: codex not found: set CODEX_BIN or pass --codex", file=sys.stderr)
        return 3

    mode = "staged" if a.staged else "uncommitted" if a.uncommitted else "range"
    rng = a.range or (default_range() if mode == "range" else None)
    if not rng and mode == "range":
        fail("no upstream or origin/HEAD; pass --range, --staged or --uncommitted")
    for sig in (signal.SIGTERM, signal.SIGINT):
        signal.signal(sig, _stop)
    prompt, rng = build_prompt(rng, mode)
    if prompt is None:
        print(f"review-gate: nothing to review in {rng}")
        return 0

    workdir = tempfile.mkdtemp(prefix="review-gate-")
    with ThreadPoolExecutor(RUNS) as pool:
        results = list(pool.map(lambda n: run_one(n, codex, a.model, prompt, a.timeout, workdir),
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
        print(json.dumps({"decision": decision, "range": rng, "model": a.model,
                          "votes": votes, "usable": len(usable), "runs": results,
                          "findings": findings, "logs": workdir}, indent=1))
        return code
    print(f"review-gate: {decision} ({votes}/{len(usable)} usable runs request changes) "
          f"on {rng}, model {a.model}")
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
