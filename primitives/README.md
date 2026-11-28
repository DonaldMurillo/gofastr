# The primitives registry

What framework contributors, human or agent, must hold true while changing
the code: the primitives the framework is built from, the invariants each
one keeps, and the decisions behind them. One YAML file per primitive,
checked by `go run ./cmd/primitives lint`.

Where this is going, and in what order, is `PLAN.md` beside this file.

It replaces prose. A rule that lives in a code comment, in `AGENTS.md` or
in an architecture document is not enforced and goes stale. An enforced
rule here names the test, analyzer or contract rule that fails when it
is broken. The few that nothing can enforce carry a question for a
reviewer instead (`judge`).

## Read it one level at a time

```
go run ./cmd/primitives                 # the top-level primitives, one line each
go run ./cmd/primitives for <path>...   # what governs these files
go run ./cmd/primitives show <id>       # one primitive's details and its children
```

Run `primitives for` on the files you are about to change. It prints the owner
chain, outermost first, with its `on_change` notes, import bans, invariants and
decision summaries. It also shows recorded twins for those files and rules
other primitives apply to them.

`primitives show` prints one primitive's summary, code and docs paths,
`on_change` notes, import bans, invariants with their `applies` patterns,
and decisions with their rejected options and revisit conditions. It lists
the primitive's direct children.
Read the primitive's `primitive.yml` for the complete entry.

`AGENTS.md` still names the architecture documents to read before adding
or moving code they cover.

## Layout

A primitive is a directory holding `primitive.yml`. Nesting is depth: the
directory `primitives/crud/include/` is the primitive `crud.include`, a part of
`crud`. A primitive follows the job, not the package: it may own files in
several packages, and one package may be split across several primitives.

## primitive.yml

```yaml
# Must match the directory.
primitive: crud.include
summary: One sentence saying what this part does.
code:
  # A path.Match pattern, or a trailing slash for a whole tree.
  - framework/crud/eager.go
  - framework/imagefield/
docs:
  - framework/docs/content/includes.md
# Each line is shown when this part changes.
on_change:
  - What a change here must also do, that no test can check.
# Generated into primitives_imports_test.go.
imports:
  - deny: framework/image
    from: [framework/crud]
    why: One sentence on what the ban protects.
    instead: What to do when you need the banned package.
# One job done in more than one place.
twins:
  - job: What the sites all do.
    why: What keeps them from being one implementation today.
    sites:
      - path: framework/crud/include.go
        note: Which function, and for what.
      - path: framework/crud/eager.go
        note: The other one.
invariants:
  - id: match-by-primary-key
    rule: One present-tense sentence that is either true or false of the code.
    enforced_by:
      # <package dir>.<TestName>
      - test: framework/crud.TestEagerLoadScrubsSoftDeleteAndHidden
      # A registered internal/analyzers package.
      - analyzer: mapwriter
      # A framework/contracts rule.
      - contract: GOFASTR1407
    judge: A yes/no question where yes means a violation?
    # Files outside code that this rule also governs, using the same patterns.
    applies:
      - framework/crud/include.go
decisions:
  - id: match-by-primary-key
    date: 2026-11-14
    # Use superseded with superseded_by to name a successor.
    status: accepted
    decision: One sentence saying what was chosen.
    rejected:
      - option: The alternative.
        why: The reason it lost.
    revisit_if: The condition that reopens this.
    # An invariant id or <primitive>/<id>.
    upholds: [match-by-primary-key]
```

Quote any string that holds `: ` or ` #`. Block scalars (`|`, `>`) are not
read; a rule or a reason is one sentence on one line.

A file has one owner chain, but a rule can govern files of other
primitives. `applies` names them, in the same form as `code`, and the
rule then shows for those files too: in `primitives for`, in the stop
check's notice and as a `judge` question. The rule keeps one home.
Do not copy a rule into each primitive it governs.

A twins entry is a debt, not a feature. One job belongs in one
implementation, and the fix for a twin is to merge its sites and delete
the entry. An entry exists only for the time the sites are still apart,
and its `why` says what keeps them apart: often nothing but work not yet
done, rarely a real reason code cannot be shared. `primitives lint`
prints how many are left. Until a twin is merged, `primitives for` on
any of its sites prints every other site. A site need not be owned by
the primitive that lists it.

### Changes to shared rules

Before editing, use `primitives for` on the related sites it lists.
Read the rules that apply there and test related inputs against the
shared rule. Fix reproducible violations of that rule as part of the
change. Include regression tests for the fixes and for valid behavior
that must still work. In the change summary, explain why any listed
site needs no change.

## What the harness enforces

`.claude/settings.json` holds what must happen whatever an agent decides.

- **`primitives hook`** runs after every tool call that can change a
  file. It looks at what the working tree changed and, once per session,
  tells the agent about a changed file that is a twins site.
- **`primitives stop`** runs when a session tries to finish. It checks
  the registry and generated import tests, then runs vet and the short
  tests of every package changed since `origin/main`, leaving out a file
  whose only change is to its comments. Packages under a nested `go.mod`
  run in that module. A testdata module is skipped: those trees exist to
  be wrong, so `go build ./...` there is not a session failure. A failure sends the agent back once, with the
  output, as a block decision on stdout: the hook runs through `go run`,
  which cannot pass on the exit code Claude Code treats as blocking. A tree that already passed is not tested again. Once the
  code passes it asks, once per session, whether another code path does
  the same job and needs the same change. For each primitive whose code
  changed it shows the `on_change` lines, and names the `docs` when none
  of them changed. For each changed file, including test files, it shows
  once per session every rule another primitive `applies` to it, with its
  `judge` question.
- **Deny rules** cover what cannot be taken back: a forced push, a push
  to `main`, discarding the whole working tree (`git reset --hard`,
  `git clean`, `git checkout .`, `git restore .`), and reading `.env`
  files or SSH keys. A deny rule matches the command as written, so it
  stops the usual spelling, not every route to the same end.

## What lint refuses

- An unknown key, a missing `summary`, `rule` or `decision`, an id that is
  not kebab-case, or a `primitive` that does not match its directory.
- An unquoted value that ` #` cuts short: YAML reads the rest of the line
  as a comment.
- A child whose parent has no `primitive.yml`.
- A `code` entry that matches no file, or a file claimed by two primitives
  where neither contains the other.
- A `docs` path that is not a file.
- An `applies` entry that matches no file, or only files its own
  primitive already owns.
- An `imports` entry whose `from` or `deny` uses a wildcard or names a
  directory without non-test Go source.
- A `twins` entry with no `why`, with fewer than two sites, or with a
  site that is not a file.
- An `enforced_by` test that does not exist, an analyzer that is not
  registered, a contract rule that is not in the catalog.
- An invariant with neither `enforced_by` nor `judge`.
- A decision that upholds an invariant that does not exist, or a
  superseded decision that names no successor.

## Generated code

`go run ./cmd/primitives gen` writes `primitives_imports_test.go` into every package
an `imports` rule names: a `TestPrimitiveImports` that fails when the banned
package enters that package's dependency closure, printing the rule's
`why` and `instead`. `gen --check` fails when a generated file is missing,
outdated or orphaned. Never edit a generated file.

## Judged invariants

Prefer `enforced_by`. Use `judge` alone only for a rule no test or
analyzer can state. `go run ./cmd/primitives judge --json` lists the questions
that apply to the files changed against `origin/main`, each with its rule
and files, for a reviewer model to answer. It asks nothing itself and
costs nothing: questions are limited to changed files on judged
invariants.

## Adding an entry

1. Find the owner with `primitives for <path>`. If the path has none, add a
   primitive, under an existing parent where one fits.
2. Write the invariant as a sentence that can be false.
3. Name what enforces it. If nothing does, write the test first, break
   the code, and watch the test fail.
4. Record a decision only where an alternative was considered and lost.
5. Run `go run ./cmd/primitives lint` and `go run ./cmd/primitives gen`.
6. Delete the comment or prose the entry replaces.
