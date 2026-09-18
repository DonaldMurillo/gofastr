---
name: unslop
description: Cut AI tells from any writing. Must always apply.
---

# Unslop

Vendored from DemandTheLimits/claude-skills (plugins/dtl-developers/skills/unslop, commit
91825a2, 2026-09-17) at the maintainer's request. The scanner lives beside this file at
`scripts/slopscan.py`; run it from the repo root. The plugin's two hooks (a Stop hook over chat
replies and a guard that refuses ungated `gh` writes) are not wired here: the repo's own
`./scripts/pr-review-findings.sh` flow writes to GitHub on purpose. `scripts/unslop_hook.py` is
kept so a host that wants the Stop hook can add it to `.claude/settings.json`.

Edit text to remove AI patterns and add human voice.

## Rules and guidelines are different things

A **rule** has no legitimate exception. It is enforced by refusing the write, and there is
no override. Rules are the character-level ones in the next section.

A **guideline** is usually right and occasionally wrong. It is enforced by reporting the
violation back to you and letting you decide. If this case genuinely earns the exception,
re-run with `--ack "<reason>"` and say why. The reason is required because a justification
you have to write is a decision, and one you do not is a reflex.

Both are enforced by `scripts/slopscan.py`. Run it over every PR body, comment, commit
message and doc before it ships. Everything else here is judgement.

```bash
python3 .claude/skills/unslop/scripts/slopscan.py --file /tmp/note.md --kind comment
# exit 0 clean, 2 rule violated (stop), 3 guideline violated (add --ack "why")
```

## Process

1. Scan for the patterns below.
2. Rewrite. Preserve meaning, match intended tone.
3. Add soul (see next section).
4. Self-audit: "What makes this obviously AI generated?" Fix remaining tells.

## Adding soul

Removing patterns is half the job. Sterile, voiceless writing is just as obvious.

- **Have opinions.** React to facts instead of neutrally listing pros and cons.
- **Vary rhythm.** Short sentences. Then longer ones that take their time. Mix it up.
- **Acknowledge complexity.** "Impressive but also kind of unsettling" beats "impressive."
- **Use "I" when it fits.** First person isn't unprofessional.
- **Let some mess in.** Perfect structure looks machine-made.
- **Be specific.** Not "this is concerning" but "there's something unsettling about agents churning away at 3am."

## Rules (no override)

Text inside fenced code blocks and inline backticks is exempt, so quoting real command
output or naming a literal string you grep for stays legal.

### R1. No em dashes

Use periods or commas. No parentheses, no en dashes, no hyphen-as-dash substitutes.
Reaching for a parenthesis instead just trades one tell for another. If a thought needs
separation, end the sentence or use a comma.

This rule was in the skill for months while 898 em dashes accumulated in the skills around
it, which is why it is now a rule the harness enforces rather than a sentence you can skip.

### R2. No en dashes

"3 to 5", or a hyphen for a compact range like `09:00-17:00`.

### R3. Straight quotes only

No curly quotes.

### R4. Emoji: seven, by allowlist

`⚠️` warning, `⛔` hard stop, `✅` confirmed, `❓` question, `➡️` recommendation,
`🔴` and `🟢` for an EOS Scorecard, and `🤖` for an AI provenance line the host or workflow requires
at the foot of a PR body. These earn their place because they carry meaning a reader scans
for. Every other emoji is decoration and is refused.

The eighth was added the first time the gate refused one of our own PR bodies. That is the
allowlist working: a new glyph has to be argued for, not merely used.

## Guidelines (overridable with a stated reason)

### G1. Length, by what you are writing

| Writing | Note at | Refuses at |
| --- | --- | --- |
| Comment | 300 words | 600 |
| Issue body | 300 words | 600 |
| PR body | 300 words | 600 |
| Commit message | n/a | 120 |

The note is advisory and needs no override. It exists so the common case gets feedback
without a gate in front of it.

Match the weight to the payload. A deploy confirmation is one sentence. Eight distinct
failures with dates is a table, and that one is worth acking. Writing every comment at
investigation weight regardless of what it carries is the actual problem, not length.

### G2. Bold: three, cap of five

Bold every point and none of them reads as one. A bold lead-in that ends in a period, names
the item, and is followed by genuinely new detail is fine.

What is not fine, and what the old wording let through, is a full sentence in bold acting as
a pseudo-heading: `**1. Env vars are no longer step 3, they are a precondition.**` That is a
heading wearing an exemption's clothes.

### G3. Headings: none under 250 words, cap of four

A 400-word comment with four `###` headings is a document pretending to be a note. Structure
substituting for deciding what matters is the tell. If it truly needs six sections, it is an
issue, not a comment.

### G4. Titles: one clause, ten words

A leading scope prefix is allowed and does not count against the budget, because it is
mandated elsewhere: `docket-canary: the sync stopped`. Past that prefix, no colon, no em
dash, and no parenthetical. A parenthetical in a title is always a second sentence with the
punctuation filed off.

The title says what is wrong. The body says how much.

### G5. Name what you reference, and link it

A bare `#577` is not a reference. It is a number that means something to whoever typed it and
nothing to whoever reads it later.

Three separate problems, and repo-qualifying only fixes the first:

- **Numbers collide.** Ten numbers currently exist in more than one DTL repo, and `#20` is
  three unrelated issues. `#20` is genuinely ambiguous, not merely terse.
- **It does not say what kind of thing it is.** GitHub shares one number space across issues
  and pull requests, so `#577` could be either.
- **It only links inside GitHub.** In a comment, `#577` auto-links. In a `SKILL.md`, a spec, or
  a commit body, it is dead text. Most of our references live in the second kind of place.

The fix scales with the surface:

| Where | Minimum | Better |
| --- | --- | --- |
| GitHub comment, issue, PR body | `data-platform#577` | `data-platform#577, the sync retry` |
| Skill, spec, doc | `data-platform#577` plus a full URL | `[data-platform#577 the sync retry](https://github.com/DemandTheLimits/data-platform/pull/577)` |
| Commit message | `(#577)` is fine for this repo | qualify it only when it is another repo |

Commit messages are the exception the scanner skips. A commit lives in exactly one repo and
GitHub resolves a bare number against that repo, so `(#577)` there is a real convention rather
than a lazy one. Qualify it only when pointing at a different repo.

The test is whether a reader six months from now can tell what it was without opening it.
"Fixed in #577" fails. "Fixed by the sync retry in data-platform#577" passes, because the
sentence still carries meaning when the link rots.

Same for a branch or a commit. `b6f0275` alone is a hash; `b6f0275, which stopped the routine
restarting the board unconditionally`, is a reference.

`project #1` and `Rock #3` are not references and are not flagged. They are the names of
things.

### G6. Don't reuse a section skeleton

If the same heading opened your last three comments, it is furniture. `## Answer` on a
comment posted in reply to a question adds nothing its position does not already say. Ten
comments opening with the identical heading is a machine tell no vocabulary fix will hide.

## Patterns to detect and fix

### Content

1. **Puffery.** "pivotal moment", "testament to", "evolving landscape", "setting the stage for", "indelible mark", "deeply rooted". Cut puffery, state what happened.
2. **Name-dropping.** Listing media outlets without context. Pick one, say what was said.
3. **Superficial -ing phrases.** "highlighting...", "ensuring...", "reflecting...", "showcasing...", "fostering...". Delete or expand with real sources.
4. **Promotional language.** "nestled", "vibrant", "breathtaking", "groundbreaking", "renowned", "stunning", "must-visit". Use neutral descriptions.
5. **Vague attributions.** "Experts believe", "Industry reports suggest", "Some critics argue". Name the source or delete.
6. **Formulaic challenges.** "Despite challenges... continues to thrive." Replace with specific facts.

### Language

7. **AI vocabulary.** Additionally, crucial, delve, enduring, enhance, fostering, garner, interplay, intricate, landscape (abstract), pivotal, showcase, tapestry (abstract), testament, underscore, vibrant. Replace with plain words.
8. **Fancy ways to say "is".** "serves as", "stands as", "boasts", "features". Just say "is" or "has".
9. **"Not just X, but Y."** State the point directly instead.
10. **Rule of three.** Forcing ideas into groups of three. Use the natural number.
11. **Synonym cycling.** Protagonist, main character, central figure, hero all in one paragraph. Pick one, repeat it.
12. **False ranges.** "from X to Y" where X and Y aren't on a meaningful scale. List topics directly.

### Style

13. **Colon overuse.** Colons are fine before a list or example. Not as mid-sentence connectors. "If you're coming from traditional automation: instead of registering event handlers, you describe conditions" adds nothing with the colon. Rewrite to let the point stand on its own without comparison framing.
14. **Title case headings.** Use sentence case.
15. **Inline-header lists.** The tell is a bold label and colon that restates the line: "**Performance:** Performance improved...". Convert those to prose.

### Communication artifacts

16. **Chatbot phrases.** "I hope this helps!", "Let me know if...", "Of course!", "Certainly!", "Found the smoking gun!" Remove.
17. **Cutoff disclaimers.** "While specific details are limited..." Find sources or remove.
18. **Sycophantic tone.** "Great question! You're absolutely right!" Respond directly.

### Filler

19. **Filler phrases.** "In order to" becomes "To". "Due to the fact that" becomes "Because". "It is important to note that" gets deleted.
20. **Excessive hedging.** "could potentially possibly be argued that it might" becomes "may".
21. **Generic conclusions.** "The future looks bright." State specific plans or facts.

### Jargon

22. **Abstract metaphor nouns.** Substrate, wedge, vector, locus, vantage, nexus, primitive (as noun), harness (as metaphor), surface (as in "API surface"), bedrock, scaffolding (as metaphor), modality, paradigm, gold-plating, ratchet (as metaphor), evacuate (for moving code), endgame, north star, flywheel. These read as technical but usually have a plainer concrete word. "Substrate" becomes "base". "Wedge in" becomes "add". "Vector" becomes "way" or "method". "Gold-plating" becomes "more than the job needs". "Ratchet" becomes the mechanism's real name or "a limit that only tightens". "Evacuate" becomes "move out". "Endgame" becomes "the last phase". Pick the concrete word.

### Plain speech

23. **Say what it does, not how it feels.** "the database stays close at hand", "SQL you can read", "types that follow your schema" name a feeling. The fix names the mechanism or a number: "`.toSQL()` returns the exact string sent to the database", "a column rename fails the build". Ask what the sentence tells the reader to do or know, then write that. If you can't restate it as a concrete instruction, fact, or number, cut it. One more check: if the sentence could appear unchanged in another project's docs, it says nothing about this one. Cut it.
24. **Shorten or split dense sentences.** If the reader has to backtrack to parse a sentence, break it in two or drop clauses. One idea per sentence.
25. **Active voice.** Prefer it. Catch "is/are/was/were + past participle" and name the actor: "queries are validated" becomes "the compiler validates queries", "the file is parsed by the loader" becomes "the loader parses the file". Passive is fine only when the actor is unknown or genuinely doesn't matter.
26. **Cut adverbs, or use a stronger verb.** "runs quickly" becomes "is fast" or the number. "significantly improves" becomes the measured delta. An adverb propping up a weak verb means the verb is wrong.
27. **Prefer the plain word.** "utilize" becomes "use", "leverage" becomes "use", "facilitate" becomes "help", "numerous" becomes "many", "in the event that" becomes "if". The fancier synonym is rarely clearer.

## The two guards, on by default

The plugin registers both through `hooks/hooks.json`, so there is nothing to install.

`scripts/unslop_hook.py` is a Stop hook checking the **rules** against your final message, so
they reach chat replies and not only what gets published. Rules only, never guidelines: a long
answer in chat is often right and the reader can scroll. `UNSLOP_HOOK=off` disables it.

`scripts/gh_write_guard.py` is a PreToolUse hook that refuses `gh issue create|comment|edit|close|transfer`,
`gh pr create|comment|edit`, and a mutating `gh api` call against an issues or pulls path, because
those reach GitHub without the gates. Reads are untouched. `GH_WRITE_GUARD=off` disables it.

Both fail open on any error. See **HOOK.md** in this folder for the limits, including the two
this deliberately does not cover.
