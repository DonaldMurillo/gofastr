---
name: primitives-sweep
description: Move what a package's comments and prose claim into the registry (primitives/**/primitive.yml), then delete the comments. Load it when asked to sweep, strip or cut comments, to add a primitive, invariant or decision to the registry, to record why something was built a certain way, or to shrink CLAUDE.md or an ARCHITECTURE.md into registry entries. Also load it before writing a comment longer than two lines in framework code, because the comment probably belongs in the registry instead.
---

# Primitives sweep

A comment that states a rule is not enforced and gets copied. The sweep
turns each such comment into a registry entry that names what fails when
the rule is broken, and only then deletes the comment. The format is
`primitives/README.md`. Read it first.

One sweep covers one package or one primitive. Keep it to one PR.

## What a sweep never touches

- Directives: `//go:`, `//gofastr:allow(...)` with its reason,
  `//gofastr:test-reads`, `//gofastr:generated`, build tags, `// Output:`
  in examples, licence headers.
- Doc comments on exported identifiers. Trim one to the contract (what
  it does, what it returns, when it errors) and move the history out,
  but keep it.
- Comments in `_test.go` files. A test says what it pins.
- Generated files.

## Steps

1. **Find the owner.** Run `go run ./cmd/primitives for <files>`. If a file
   has no owner, add a primitive first. Split by job, not by package,
   and nest it under the parent it is a part of.

2. **Sort every remaining comment into one of five kinds.**

   | Kind | Example | What happens |
   |---|---|---|
   | Rule | "must match nothing, never everything" | invariant |
   | Choice | "position was the first attempt" | decision |
   | Workaround | "until X is fixed, do Y here" | fix X, or open an issue and record a decision with `revisit_if` |
   | Narration | "loop over the rows" | delete |
   | History | "caught 2026-06 in PR #198" | delete; the commit log has it |

3. **Write the invariant as a sentence that can be false.** "Handles
   scope carefully" cannot be false. "A read-scope operator the renderer
   does not know matches no row" can.

4. **Name the enforcer and prove it.** Find the test, analyzer or
   contract rule that pins the invariant. Then break the code and run
   it. If nothing fails, the mapping is wrong or the rule is unenforced:
   write the test, watch it fail, fix the code back. A test name that
   merely sounds right is not an enforcer. Record each proof in the PR
   description as `mutation -> failing test`.

5. **Reach for `judge` last.** Use it alone only when no test or
   analyzer can state the rule, and write it as a yes/no question where
   yes means a violation. Pair it with a test when the rule is "these
   two sites must change together".

6. **Record a decision only where something lost.** One sentence for
   what was chosen, one `rejected` entry per alternative with the reason
   it lost, and a `revisit_if` when the comment implies one. Link it to
   its invariant with `upholds`.

7. **Two ways to do one job.** When the sweep finds two internal paths
   for the same job, do not record both. Either merge them (see the
   `twin-sweep` skill), or record an invariant that they agree, enforced
   by a parity test, plus a decision saying why two remain.

8. **Delete the comments** the entries replace. Where a symbol is named
   in a registry entry, no pointer back is needed: `primitives for <file>` finds
   it.

9. **Check.** `go run ./cmd/primitives lint`, `go run ./cmd/primitives gen`,
   `make test`, `make analyze`.

## Reporting

End with a table: comments removed by kind, invariants added, decisions
added, mutations proven, and every rule left unenforced with the reason.
An unenforced rule is a finding, not a footnote.

## Moving prose out of CLAUDE.md and the architecture documents

Same steps, with the paragraph as the comment. A hard rule becomes an
invariant on the primitive it governs, enforced by the analyzer or
contract rule that already checks it. Delete the paragraph once
`primitives for` on a governed file prints the rule. What stays in CLAUDE.md
is how to find the registry and what no primitive can own.
