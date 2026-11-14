# The canon

What framework contributors, human or agent, must hold true while changing
the code: the primitives the framework is built from, the invariants each
one keeps, and the decisions behind them. One YAML file per primitive,
checked by `go run ./cmd/canon lint`.

It replaces prose. A rule that lives in a code comment, in `CLAUDE.md` or
in an architecture document is not enforced and goes stale. A rule here
names the test, analyzer or contract rule that fails when it is broken.

## Read it one level at a time

```
go run ./cmd/canon                 # the top-level primitives, one line each
go run ./cmd/canon for <path>...   # what governs these files
go run ./cmd/canon show <id>       # one primitive in full, and its children
```

Run `canon for` on the files you are about to change. It prints the chain
of primitives that own them, outermost first, with every invariant and
decision on that chain. Nothing else needs reading first.

## Layout

A primitive is a directory holding `primitive.yml`. Nesting is depth: the
directory `canon/crud/include/` is the primitive `crud.include`, a part of
`crud`. A primitive follows the job, not the package: it may own files in
several packages, and one package may be split across several primitives.

## primitive.yml

```yaml
primitive: crud.include          # must match the directory
summary: One sentence saying what this part does.
code:                            # the files it owns
  - framework/crud/eager.go      # a path.Match pattern, or
  - framework/imagefield/        # a trailing slash for a whole tree
docs:
  - framework/docs/content/includes.md
imports:                         # generated into canon_imports_test.go
  - deny: framework/image
    from: [framework/crud]
    why: One sentence on what the ban protects.
    instead: What to do when you need the banned package.
invariants:
  - id: match-by-primary-key
    rule: One present-tense sentence that is either true or false of the code.
    enforced_by:
      - test: framework/crud.TestName    # <package dir>.<TestName>
      - analyzer: mapwriter              # a registered internal/analyzers package
      - contract: GOFASTR1407            # a framework/contracts rule
    judge: A yes/no question where yes means a violation?
decisions:
  - id: match-by-primary-key
    date: 2026-11-14
    status: accepted                     # or superseded, with superseded_by
    decision: One sentence saying what was chosen.
    rejected:
      - option: The alternative.
        why: The reason it lost.
    revisit_if: The condition that reopens this.
    upholds: [match-by-primary-key]      # or <primitive>/<id>
```

Quote any string that holds `: ` or ` #`. Block scalars (`|`, `>`) are not
read; a rule or a reason is one sentence on one line.

## What lint refuses

- An unknown key, a missing `summary`, `rule` or `decision`, an id that is
  not kebab-case, or a `primitive` that does not match its directory.
- A child whose parent has no `primitive.yml`.
- A `code` entry that matches no file, or a file claimed by two primitives
  where neither contains the other.
- A `docs` path that is not a file.
- An `enforced_by` test that does not exist, an analyzer that is not
  registered, a contract rule that is not in the catalog.
- An invariant with neither `enforced_by` nor `judge`.
- A decision that upholds an invariant that does not exist, or a
  superseded decision that names no successor.

## Generated code

`go run ./cmd/canon gen` writes `canon_imports_test.go` into every package
an `imports` rule names: a `TestCanonImports` that fails when the banned
package enters that package's dependency closure, printing the rule's
`why` and `instead`. `gen --check` fails when a generated file is missing,
outdated or orphaned. Never edit a generated file.

## Judged invariants

Prefer `enforced_by`. Use `judge` alone only for a rule no test or
analyzer can state. `go run ./cmd/canon judge --json` lists the questions
that apply to the files changed against `origin/main`, each with its rule
and files, for a reviewer model to answer. It asks nothing itself and
costs nothing: questions are limited to changed files on judged
invariants.

## Adding an entry

1. Find the owner with `canon for <path>`. If the path has none, add a
   primitive, under an existing parent where one fits.
2. Write the invariant as a sentence that can be false.
3. Name what enforces it. If nothing does, write the test first, break
   the code, and watch the test fail.
4. Record a decision only where an alternative was considered and lost.
5. Run `go run ./cmd/canon lint` and `go run ./cmd/canon gen`.
6. Delete the comment or prose the entry replaces.
