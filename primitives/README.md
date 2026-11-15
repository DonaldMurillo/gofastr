# The primitives registry

What framework contributors, human or agent, must hold true while changing
the code: the primitives the framework is built from, the invariants each
one keeps, and the decisions behind them. One YAML file per primitive,
checked by `go run ./cmd/primitives lint`.

Where this is going, and in what order, is `PLAN.md` beside this file.

It replaces prose. A rule that lives in a code comment, in `CLAUDE.md` or
in an architecture document is not enforced and goes stale. An enforced
rule here names the test, analyzer or contract rule that fails when it
is broken. The few that nothing can enforce carry a question for a
reviewer instead (`judge`).

## Read it one level at a time

```
go run ./cmd/primitives                 # the top-level primitives, one line each
go run ./cmd/primitives for <path>...   # what governs these files
go run ./cmd/primitives show <id>       # one primitive in full, and its children
```

Run `primitives for` on the files you are about to change. It prints the chain
of primitives that own them, outermost first, with every invariant and
decision on that chain. That is all the registry holds for those files.
`CLAUDE.md` still names the architecture documents to read before adding
or moving code they cover.

## Layout

A primitive is a directory holding `primitive.yml`. Nesting is depth: the
directory `primitives/crud/include/` is the primitive `crud.include`, a part of
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
on_change:                       # one line each, shown when this part changes
  - What a change here must also do, that no test can check.
imports:                         # generated into primitives_imports_test.go
  - deny: framework/image
    from: [framework/crud]
    why: One sentence on what the ban protects.
    instead: What to do when you need the banned package.
twins:                           # one job done in more than one place
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
      - test: framework/crud.TestName    # <package dir>.<TestName>
      - analyzer: mapwriter              # a registered internal/analyzers package
      - contract: GOFASTR1407            # a framework/contracts rule
    judge: A yes/no question where yes means a violation?
    applies:                             # files outside code: this rule governs too
      - framework/crud/eager.go          # same pattern form as code
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

## What the harness enforces

`.claude/settings.json` holds what must happen whatever an agent decides.

- **`primitives hook`** runs after every tool call that can change a
  file. It looks at what the working tree changed and, once per session,
  tells the agent about a changed file that is a twins site.
- **`primitives stop`** runs when a session tries to finish. It checks
  the registry and runs vet and the short tests of every package changed
  since `origin/main`, leaving out a file whose only change is to its
  comments. A package under a nested `go.mod` is run in that module, not
  from the repo root. A testdata module is skipped: those trees exist to
  be wrong, so `go build ./...` there is not a session failure. A failure sends the agent back once, with the
  output, as a block decision on stdout: the hook runs through `go run`,
  which cannot pass on the exit code Claude Code treats as blocking. A tree that already passed is not tested again. Once the
  code passes it asks, once per session, whether another code path does
  the same job and needs the same change. For each primitive whose code
  changed it shows the `on_change` lines, and names the `docs` when none
  of them changed. For each changed file it shows, once per session,
  every rule another primitive `applies` to it, with its `judge`
  question.
- **Deny rules** cover what cannot be taken back: a forced push, a push
  to `main`, discarding the whole working tree (`git reset --hard`,
  `git clean`, `git checkout .`, `git restore .`), and reading `.env`
  files or SSH keys. A deny rule matches the command as written, so it
  stops the usual spelling, not every route to the same end.

## What lint refuses

- An unknown key, a missing `summary`, `rule` or `decision`, an id that is
  not kebab-case, or a `primitive` that does not match its directory.
- A child whose parent has no `primitive.yml`.
- A `code` entry that matches no file, or a file claimed by two primitives
  where neither contains the other.
- A `docs` path that is not a file.
- An `applies` entry that matches no file, or only files its own
  primitive already owns.
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
