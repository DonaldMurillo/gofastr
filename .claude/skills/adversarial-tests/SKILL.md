---
name: adversarial-tests
description: How to run (or commission) an adversarial security-test pass against this codebase without producing 3000 lines of repetitive matrix tests. Auto-loads when the user mentions "red tests", "adversarial tests", "security test pass", "find security gaps", or is about to spawn a sub-agent to author *_security_test.go files. Encodes the "property × surface, not case × file" rule, the naming + triage policy, and a ready-to-paste prompt template for the next pass.
---

# Adversarial test pass — how to commission one without drowning

The previous adversarial pass authored 32 `_red_test.go` files / ~3000
lines, of which **~10 were genuinely unique bugs** and **~17 were
distinct production fixes**; the rest was 60-case matrices testing
the same property at different surfaces, plus a handful of wrong-
layer tests that contradicted documented contracts.

This skill exists so the next pass produces signal, not volume.

## The one rule

Adversarial tests are organized along **properties × surfaces**, not
**cases × files**.

A "property" is the underlying invariant: *no control bytes in
outbound headers*, *every URL field allow-lists schemes*, *every
in-process CRUD method fails closed without tenant context*. A
"surface" is each place the property must hold (each header writer,
each URL field, each method).

The wrong shape (what we got last time):

```
60 cases × 1 surface per test file, repeated across N files,
where 50 of the cases are noise that test the same byte range.
```

The right shape:

```
For each property:
  - 3-5 case shapes (the distinct attack classes, not 60 variants)
  - Asserted at every surface where the property must hold (loop over surfaces, not over cases)
  - One test file per property family, NOT one per surface
```

A test file with 60 cases is almost always asking the wrong question.
Ask "what surfaces does this property apply to?" before "what
attack strings can I generate?".

## Properties × surfaces this codebase keeps re-encountering

When you commission a pass, ask the agent to scan for *each* of these
properties across *every* call site, not for "more cases":

| Property | Common surfaces | Reference fix |
|---|---|---|
| URL scheme allow-list (http(s) / relative / fragment only) | any field that flows to `<a href>`, `<img src>`, `<form action>`, `<link href>`, `Link:` response header, OpenAPI `servers[].url`, SEO meta tags, Image/File entity fields | `framework/ui/safety.go::safeURL`, `framework/uihost/uihost.go::isSafeHeadURL`, `framework/crud/crud_upload.go::isSafeMediaURL`, `framework/experimental/apiversions/version.go::safeReplacementURL` |
| C0 / DEL strip on outbound strings | response header values (`Content-Type`, `Access-Control-*`, `Link:`), log attribute values (`method`, `path`), SSE field bodies, DSL opaque literals, route-group prefix | `core/handler/respond.go::sanitizeHeaderValue`, `core/middleware/cors.go::stripCtrlBytes`, `core/middleware/logging.go::safeLogMethod`, `core/stream/sse.go::scrubSSEDataLines`, `framework/dsl/dsl.go::stripDSLControlBytes` |
| Strict JSON top-level parsing (reject duplicate / case-folded / unknown keys) | every `Bind` consumer | `core/handler/bind.go::validateBodyKeys` |
| Fail-closed scoping (multi-tenant + owner-required) on in-process paths | every CRUD method that touches DB state (not just the HTTP path — middleware doesn't protect in-process callers) | `framework/crud/owner.go::requireTenantContext` + every method in `framework/crud/crud_api.go` |
| Forensic completeness on rollback / soft-delete | batch envelope, upsert ON CONFLICT, audit hook | `framework/crud/crud_batch.go::scrubRolledBackData`, `framework/crud/crud_upsert.go::errSoftDeletedResurrection` |
| Panic isolation at extension points | every place a third-party callback runs (hooks, plugins, custom handlers) | `framework/hook/hook.go::runHookSafely` |

When you commission the next pass, the prompt MUST mention this table
and tell the agent to *find new surfaces*, not *new attack strings*.

## Triage rubric (apply per test, before writing or accepting)

For every adversarial test the agent proposes:

1. **What property does this assert?** State it in one sentence. If
   you can't, the test is wrong-shaped.
2. **Where else does this property apply?** If only one place, it's
   either a real one-off or wrong-layer. If many, the agent should be
   testing every surface, not 60 cases on one.
3. **Is this contradicting a documented contract?** Grep for the
   nearest sibling `_security_test.go` and any doc comment on the
   exercised function. If yes → flag for human (this is where the
   "flip the contract" vs "delete the test" decision lives).
4. **Is the test asserting on developer input or attacker input?**
   Developer-supplied configuration (OpenAPI path keys, server URLs
   passed by the host app, etc.) has a different threat model than
   request-borne input. Tests that treat developer input as attacker
   input are wrong-layer → delete.
5. **Cap case count at ~5 per property.** 1 happy path + 3-4 distinct
   attack shapes (e.g. CR, LF, NUL, DEL — not 60 random control
   bytes). If you need 60 to feel covered, you're confusing
   *coverage* with *count*.

## Naming + location policy

Already encoded in `AI_TEST_AUDIT.md` at repo root — re-read that
file before the pass. The short version:

- File suffix: `_security_test.go` (drop the `_red_` infix entirely)
- Drop the redundant `<pkg>_` prefix when the file lives in a
  package of that name
- Function names ≤ 40 chars stating the behaviour
  (NOT `TestFoo_RedRejectsBarBlahMatrix` essays)
- One file per property family, not one per case shape
- Merge into the closest existing `_security_test.go` sibling unless
  the topic is genuinely new

## Append decisions to `AI_TEST_AUDIT.md`

Every test the pass *deletes* or *weakens* (rather than fixing
production for) gets a one-paragraph entry in `AI_TEST_AUDIT.md` with
the *why*. This is the human-auditable record of AI judgment calls
across passes. Without it, the next reviewer can't tell deliberate
deletions from drift.

## Ready-to-paste prompt for the next pass

When you want to run another adversarial pass, paste this as the
agent's brief (edit the scope line):

```
You are running an adversarial security test pass against the
GoFastr codebase. Read `AI_TEST_AUDIT.md` and
`.claude/skills/adversarial-tests/SKILL.md` first — they
encode the policy you must follow.

Scope: <e.g. framework/crud + framework/uihost; or "any package
touched on this branch">

Rules:
- Organize by **property × surface**, NOT case × file. For each
  security property you find, scan EVERY surface where it applies
  and assert at each one — not 60 cases at one surface.
- Cap case count at ~5 per property: one happy path + three or four
  distinct attack shapes covering the threat class. No 60-row
  matrices.
- Naming: `_security_test.go` suffix; function names ≤40 chars
  stating the behaviour.
- Before authoring a test, grep for the closest existing
  `_security_test.go` sibling. If one exists, merge into it
  unless the topic is genuinely new.
- Before authoring a test that contradicts a documented escape
  hatch or behaviour comment, FLAG it for the user instead of
  silently asserting the opposite. The doc comment is the
  contract until the user explicitly flips it.
- Tests that treat developer-supplied input (OpenAPI path keys,
  server URLs from host app config, etc.) as attacker input are
  wrong-layer — do not author them.
- For every test that the pass deletes or weakens, append a
  one-paragraph entry to `AI_TEST_AUDIT.md` with the *why*.

For each finding, produce:
- The underlying property (one sentence)
- Every surface where the property applies
- The production fix (or `weaken` / `delete` decision)
- The new short test name

Pre-existing properties to extend coverage on (don't re-derive — pin
to known surfaces only). See the table in
`.claude/skills/adversarial-tests/SKILL.md` for symbol references:
- URL scheme allow-list
- C0 / DEL strip on outbound strings
- Strict JSON top-level parsing
- Fail-closed multi-tenant / owner scoping on in-process paths
- Forensic completeness on rollback / soft-delete
- Panic isolation at extension points

Hard NO:
- No new `_red_test.go` files
- No 60-case matrices
- No Module_Feature_Precondition_Assertion essay test names
- No silent contradictions of documented contracts
```

## How to invoke

Two ways:

1. Paste the prompt block above into a sub-agent task (this is the
   common case — adversarial passes are heavy enough to warrant their
   own agent context).
2. Drive the pass yourself by following the rules in this file — the
   prompt block IS the policy, just internalized.

After the pass: run `./scripts/test-all.sh` and confirm exit 0;
audit `find . -maxdepth 3 -type f -size +500k …` for stray binaries
per `CLAUDE.md`; review the new `AI_TEST_AUDIT.md` entries.
