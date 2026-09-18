#!/usr/bin/env python3
"""Writing gate for the DTL skills. Enforces `unslop` on anything about to be published.

Sibling of secretscan.py and used the same two ways: imported by board.py to vet a title,
body or comment before it reaches GitHub, and called as a CLI gate by any skill writing a
PR body, a commit message, or a report.

TWO CLASSES OF FINDING, AND THE DIFFERENCE MATTERS.

  RULE (exit 2, no override). Characters with no legitimate use in DTL prose: em dash, en
  dash, curly quotes, and any emoji outside the allowlist. Text inside fenced code blocks
  and inline backticks is exempt, so quoting real command output or a literal string you
  grep for stays legal.

  GUIDELINE (exit 3, overridable). Length, bold density, heading density, and title shape.
  These are usually right and occasionally wrong. The scanner reports what it found and the
  caller decides; passing --ack "<reason>" proceeds and records the reason. Requiring a
  written reason is what makes the override a decision rather than a reflex.

Exit 0 is clean, and may still print advisory notes to stderr. Those need no override; they
exist so the common case gets feedback without a gate in front of it.

CLI:
  slopscan.py --file body.md --kind comment
  slopscan.py --text "..." --kind title
  cmd | slopscan.py --kind body
  slopscan.py --file body.md --kind comment --ack "eight distinct failures, each with a date"

Exit 0 = clean, 2 = rule violated (stop), 3 = guideline violated (overridable), 1 = usage.
"""
import argparse
import re
import sys

# ---------------------------------------------------------------------------
# Rules. Hard, unoverridable, and deliberately short.
# ---------------------------------------------------------------------------

# Every emoji in the DTL skills is a semantic status marker, not decoration. An allowlist
# keeps those working while still stopping the next one arriving without a reason.
#   warning / hard stop / confirmed / question / recommendation / EOS red / EOS green
EMOJI_ALLOWED = {
    "\u26a0",          # warning
    "\u26d4",          # hard stop
    "\u2705",          # confirmed
    "\u2753",          # question
    "\u27a1",          # recommendation
    "\U0001F7E2",      # EOS green
    "\U0001F534",      # EOS red
    "\U0001F916",      # AI provenance, required in the Claude Code PR attribution line
}

# Box-drawing and arrows are NOT emoji. They build the directory trees in the domain docs
# and carry real notation ("from_phase -> to_phase"). 564 of them are in the repo on
# purpose. Never flag these.
NOT_EMOJI = re.compile("[\u2190-\u21ff\u2500-\u257f]")

EMOJI_RE = re.compile("[\U0001F000-\U0001FAFF\u2600-\u27bf\u2b00-\u2bff]")

VARIATION_SELECTOR = "\ufe0f"

# Every character named here is written as an escape, never as itself. This file is scanned
# by the same repo-wide check as everything else, and a scanner that trips its own rules is
# a scanner nobody trusts.
RULES = [
    ("em dash",     re.compile("\u2014"), "end the sentence, or use a comma"),
    ("en dash",     re.compile("\u2013"), "use 'to' for a range, or a hyphen"),
    ("curly quote", re.compile("[\u2018\u2019\u201c\u201d]"), "use a straight quote"),
]

# ---------------------------------------------------------------------------
# Guidelines. Thresholds calibrated against 49 real GitHub comments so that the
# advisory tier fires often and the blocking tier stays rare.
# ---------------------------------------------------------------------------

BUDGETS = {
    #  kind      note_words  max_words
    "comment":  (300, 600),
    "body":     (300, 600),
    "pr":       (300, 600),
    "commit":   (0,   120),
    "title":    (0,   0),      # titles are governed by TITLE rules, not word budgets here
    "label":    (0,   0),      # a label list is scanned for rules, never for prose length
}

BOLD_NOTE, BOLD_MAX = 3, 5
HEADING_MAX = 4
HEADING_FLOOR_WORDS = 250        # below this, a document should not have headings at all

# A leading scope prefix is mandated elsewhere ("docket-canary: ..."), so it is allowed and
# does not count against the title's word budget.
TITLE_PREFIX = re.compile(r"^[a-z0-9._-]+: ")
TITLE_MAX_WORDS = 10

# A bare "#577" names nothing. Numbers collide across repos (ten do on this board), the
# reader cannot tell an issue from a PR, and outside a GitHub comment it does not even link.
# "project #1" is a project number, not a reference, so it is excluded.
BARE_REF = re.compile(r"(?<![\w/`.\-:])#(\d{1,5})\b")
# Things that look like a reference and are not one. A project or Rock number, and the
# ordinal idiom ("the #1 driver"). CSS hex colours are handled by the `:` in the lookbehind
# above, NOT by matching hex digits: #577 is three valid hex characters and also a real PR,
# so content cannot separate them. Only the preceding colon can.
PROJECT_NUM = re.compile(r"(?:project|rock|epic)\s+#\d+", re.I)
NOT_A_REF = re.compile(r"#1\s+(?:driver|spot|priority)")
BARE_REF_MAX = 0                 # any unqualified reference is worth a second look

FENCE_RE = re.compile(r"^\s*```")
CODE_SPAN_RE = re.compile(r"`[^`\n]*`")
BOLD_RE = re.compile(r"\*\*[^*\n]+\*\*")
HEADING_RE = re.compile(r"^#{1,6} ", re.M)


def strip_code(text):
    """Blank out fenced blocks and inline code spans, preserving line count.

    The exemption is the whole reason a rule can be hard: quoting a log line that really
    contains an em dash, or naming the literal string `History.-` that a skill greps for,
    must stay legal.
    """
    out, fenced = [], False
    for line in (text or "").split("\n"):
        if FENCE_RE.match(line):
            fenced = not fenced
            out.append("")
            continue
        out.append("" if fenced else CODE_SPAN_RE.sub("", line))
    return "\n".join(out)


def _context(text, pos, width=48):
    line_start = text.rfind("\n", 0, pos) + 1
    line_end = text.find("\n", pos)
    line = text[line_start: line_end if line_end != -1 else len(text)]
    col = pos - line_start
    return line[max(0, col - width): col + width].strip()


def scan_rules(text):
    """Return [(rule, fix, context)] for hard violations. Code is already stripped."""
    found = []
    for name, rx, fix in RULES:
        for m in rx.finditer(text):
            found.append((name, fix, _context(text, m.start())))
    for m in EMOJI_RE.finditer(text):
        ch = m.group(0)
        if ch == VARIATION_SELECTOR or NOT_EMOJI.match(ch):
            continue
        if ch.rstrip(VARIATION_SELECTOR) in EMOJI_ALLOWED:
            continue
        found.append(("emoji not on the allowlist",
                      "allowed: " + " ".join(sorted(EMOJI_ALLOWED)),
                      _context(text, m.start())))
    return found


def scan_guidelines(text, kind):
    """Return (notes, violations). Notes are advisory; violations need an --ack."""
    notes, bad = [], []
    words = len(text.split())
    headings = len(HEADING_RE.findall(text))
    bolds = len(BOLD_RE.findall(text))

    if kind == "title":
        stripped = TITLE_PREFIX.sub("", text.strip())
        n = len(stripped.split())
        if n > TITLE_MAX_WORDS:
            bad.append((f"title is {n} words, limit is {TITLE_MAX_WORDS}",
                        f"cut {n - TITLE_MAX_WORDS}; a leading 'repo: ' prefix is free"))
        if ":" in stripped:
            bad.append((f"title has {stripped.count(':')} colon(s) past the scope prefix, "
                        f"limit is 0", "one clause, no label"))
        if "(" in stripped:
            bad.append((f"title has {stripped.count('(')} parenthetical(s), limit is 0",
                        "that is a second sentence; put it in the body"))
        return notes, bad

    note_at, max_at = BUDGETS.get(kind, BUDGETS["comment"])
    if max_at and words > max_at:
        bad.append((f"{kind} is {words} words, limit is {max_at}",
                    f"cut {words - max_at}; is this really one {kind}?"))
    elif note_at and words > note_at:
        notes.append(f"{words} words, most {kind}s land under {note_at}.")

    if bolds > BOLD_MAX:
        bad.append((f"{bolds} bold spans, limit is {BOLD_MAX}",
                    "bold every point and none of them read as one"))
    elif bolds > BOLD_NOTE:
        notes.append(f"{bolds} bold spans, past {BOLD_NOTE} they stop marking anything.")

    # Not commit messages: a commit lives in one repo and GitHub resolves a bare #N against
    # that repo, so `(#42)` in a subject is a correct convention rather than a lazy one. The
    # problem is cross-repo references and surfaces where nothing auto-links at all.
    bare = [] if kind == "commit" else [
        m for m in BARE_REF.finditer(text)
        if not PROJECT_NUM.search(text[max(0, m.start() - 30):m.start() + 8])
        and not NOT_A_REF.match(text, m.start())]
    if len(bare) > BARE_REF_MAX:
        shown = ", ".join("#" + m.group(1) for m in bare)
        bad.append((f"{len(bare)} unqualified reference(s), limit is {BARE_REF_MAX}: {shown}",
                    "say which repo and what it was: [data-platform#397 renamed the "
                    "namespaces](https://github.com/DemandTheLimits/data-platform/pull/397)"))

    if headings and words < HEADING_FLOOR_WORDS:
        bad.append((f"{headings} heading(s) in {words} words, headings allowed from "
                    f"{HEADING_FLOOR_WORDS}", "under that, write prose"))
    elif headings > HEADING_MAX:
        bad.append((f"{headings} headings, limit is {HEADING_MAX}",
                    "this wants to be an issue, not a comment"))

    return notes, bad


def report_rules(found, where=""):
    """Every violation, never a tail count.

    The list used to stop at 12 and print "and N more". A caller cannot fix what it cannot
    see, and the only way to see the rest was to attempt the write again.
    """
    head = (f"REFUSING{(' ' + where) if where else ''}. {len(found)} unslop rule "
            f"violation(s), limit is 0, not overridable:")
    lines = [head]
    for name, fix, ctx in found:
        lines.append(f"  x {name}: ...{ctx}...")
        lines.append(f"      {fix}")
    return "\n".join(lines)


def report_guidelines(bad, notes, where=""):
    head = f"unslop guideline(s) violated{(' in ' + where) if where else ''}. You decide:"
    lines = [head]
    for what, why in bad:
        lines.append(f"  ! {what}")
        lines.append(f"      {why}")
    for n in notes:
        lines.append(f"  - {n}")
    lines.append("")
    lines.append('  Sometimes long is right. If this is one of those, re-run with')
    lines.append('  --ack "<why this case earns the exception>" and it will go through.')
    return "\n".join(lines)


def gate(text, kind="comment", ack=None, where=""):
    """Shared entry point for importers. Returns (exit_code, message)."""
    stripped = strip_code(text)
    found = scan_rules(stripped)
    if found:
        return 2, report_rules(found, where)
    notes, bad = scan_guidelines(stripped, kind)
    if bad and not ack:
        return 3, report_guidelines(bad, notes, where)
    msg = ""
    if bad and ack:
        msg = f"acknowledged ({len(bad)} guideline(s)): {ack}"
    elif notes:
        msg = "\n".join(f"  - {n}" for n in notes)
    return 0, msg


def main():
    p = argparse.ArgumentParser(description=__doc__,
                                formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--file")
    p.add_argument("--text")
    p.add_argument("--kind", default="comment",
                   choices=sorted(set(list(BUDGETS) + ["title"])),
                   help="what is being written; sets the length budget")
    p.add_argument("--ack", metavar="REASON",
                   help="proceed despite a guideline violation, stating why")
    a = p.parse_args()

    where = ""
    if a.file:
        where = a.file
        try:
            with open(a.file, encoding="utf-8", errors="replace") as fh:
                text = fh.read()
        except OSError as e:
            sys.exit(f"cannot read {a.file}: {e}")
    elif a.text is not None:
        text = a.text
    else:
        text = sys.stdin.read()

    code, msg = gate(text, a.kind, a.ack, where)
    if code:
        print(msg, file=sys.stderr)
        sys.exit(code)
    if msg:
        print(msg, file=sys.stderr)
    print("clean")


if __name__ == "__main__":
    main()
