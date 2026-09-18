#!/usr/bin/env python3
"""Stop hook: refuse to end a turn whose final message breaks an unslop RULE.

Why a hook at all. A skill loaded at turn 3 has no grip at turn 60, which is how `unslop`
banned em dashes while 898 of them accumulated in the skills around it. board.py's gate
covers what gets published; this covers what gets said. Together they close both halves of
data-platform#528.

SCOPE IS DELIBERATELY NARROW. Only the final assistant message, and only the hard rules
(em dash, en dash, curly quote, non-allowlisted emoji), with fenced blocks and inline code
exempt. Length and structure guidelines are NOT checked here: a long chat answer is often
correct, the user can scroll, and a hook that argues about how long an answer should be
would be turned off within a day.

FAILS OPEN. Any error, any surprise, exit 0. A guard that wedges a session is worse than an
em dash. Kill switch: UNSLOP_HOOK=off.

The plugin registers this hook for Claude Code and Codex. Codex asks the user to trust
non-managed plugin hooks before running them. See HOOK.md in the unslop skill folder.
"""
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))


def main():
    if os.environ.get("UNSLOP_HOOK", "").lower() in ("off", "0", "false"):
        return 0

    try:
        payload = json.load(sys.stdin)
    except Exception:
        return 0

    # Never nag twice for the same turn: the model already got the message once.
    if payload.get("stop_hook_active"):
        return 0

    try:
        from slopscan import scan_rules, strip_code
    except Exception:
        return 0

    # Codex gives Stop hooks the final message directly. Claude Code supplies a transcript,
    # which remains the fallback. Do not parse a Codex transcript: its format is not a stable
    # hook interface.
    text = payload.get("last_assistant_message")
    if not isinstance(text, str) or not text.strip():
        transcript = payload.get("transcript_path")
        if not transcript or not os.path.exists(transcript):
            return 0
        try:
            with open(transcript, encoding="utf-8", errors="replace") as fh:
                lines = fh.readlines()
        except OSError:
            return 0
        text = last_assistant_text(lines)
    if not text:
        return 0

    try:
        found = scan_rules(strip_code(text))
    except Exception:
        return 0
    if not found:
        return 0

    names = sorted({name for name, _, _ in found})
    detail = "\n".join(f"  x {name}: ...{ctx}..." for name, _, ctx in found[:6])
    print(
        "Your reply breaks an unslop rule: " + ", ".join(names) + ".\n"
        + detail + "\n"
        "These are rules, not guidelines: there is no context in which they are right, and\n"
        "there is no override. Rewrite the message without them and send it again. End the\n"
        "sentence or use a comma; do not swap in parentheses or a hyphen.",
        file=sys.stderr,
    )
    return 2


def last_assistant_text(lines):
    """Concatenated text blocks of the final assistant message in the transcript."""
    for raw in reversed(lines):
        raw = raw.strip()
        if not raw:
            continue
        try:
            rec = json.loads(raw)
        except Exception:
            continue
        if rec.get("type") != "assistant":
            continue
        content = (rec.get("message") or {}).get("content")
        if isinstance(content, str):
            return content
        if isinstance(content, list):
            parts = [b.get("text", "") for b in content
                     if isinstance(b, dict) and b.get("type") == "text"]
            joined = "\n".join(p for p in parts if p)
            if joined.strip():
                return joined
        return ""
    return ""


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:
        sys.exit(0)
