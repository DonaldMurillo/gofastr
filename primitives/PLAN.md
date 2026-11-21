# Plan

What this work is for: when someone, human or agent, changes this
repository, they do the whole job, and the repository tells them what
that is at the moment it matters. Three ideas started it: far fewer
comments, the framework's parts and decisions written down as data, and
one way to do each thing.

## What the evals found

They are in `evals/primitives/results/`. In short:

- A test protects a rule. A comment, a line in `CLAUDE.md` or a registry
  entry informs and does not bind.
- Deleting comments cost nothing where a test enforced what they said.
- An agent fixes what it was pointed at and stops. Told at the right
  moment what else a change needs, Opus did it: docs in 6 of 6 runs
  (7 of 12 before), the upgrade note in 4 of 4 (0 of 7 before), the
  other sites of a recorded twin in 12 of 12 (2 of 12 before).
- A registry entry helps only where someone recorded the right thing.
  Entries written without knowing the bugs named none of the other
  paths those fixes needed. Their reminders still worked once the stop
  check showed them.

So the registry is a memory of what a change needs, the harness is what
delivers it, and tests are what enforce it.

## What is in place

- The registry format, its linter and lookup (`cmd/primitives`), with
  `framework/crud` described.
- `.claude/settings.json`: deny rules, the same-job hook, the stop
  check.
- The replay eval (`evals/primitives`), six past bugs.
- The two duplicate implementations the registry recorded in
  `framework/crud`, merged.

## What comes next, in order

Each step is its own pull request or series of them. Do not start a
step before the one above it is done.

### 1. Shrink CLAUDE.md

Move each rule out of `CLAUDE.md` and the two `ARCHITECTURE.md` files
into the primitive it governs: an invariant where a test, analyzer or
contract rule enforces it, an `on_change` line where nothing can. Start
with the Hard rules. What stays in `CLAUDE.md` is how to find the
registry and what no primitive can own.

Done when `CLAUDE.md` is under 60 lines and the replay eval on Opus
shows no drop from the numbers above.

Status: `CLAUDE.md` is 51 lines. Hard rules 1 to 8 are registry entries
in `runtime`, `uihost`, `ui`, `ui.styling` and `crud.scope`, reaching
the trees they govern through `applies`; rule 9 is an `on_change` line;
rules 10 to 12, the commands and the code map moved to
`docs/contributing.md`. The numbered list stays, one line per rule,
because code cites the rules by number, and `TestAgentsMDNamesRealRules`
keeps its pointers real.
`TestAgentsMDStaysShort` (`guide/stays-short`) fails the build when
the guide passes 60 lines, so it cannot grow back. The guide now lives
in `AGENTS.md`, which every coding agent reads; `CLAUDE.md` holds only
`@AGENTS.md` (`guide/claude-md-only-imports`).

The replay suite cannot see this step. Each replay cell is cut from a
commit older than the registry and gets the pointer paragraph spliced
into that commit's `CLAUDE.md`, so its numbers measure the tooling, not
the file. The guide suite measured the file instead: both the long
guide and the short one held 12 of 12 hard-rule traps
(`2026-11-15-guide.md`, `2026-11-15-guide-agents.md`).

Done. The replay eval on Opus after the change held or rose on every
number (`2026-11-15-replay-step1.md`).

### 2. Sweep the comments, one package at a time

Follow the `primitives-sweep` skill. History and narration go. A rule
with a proven test loses its comment. A rule with no test gets the test
first. Exported doc comments, directives and test comments stay. Order:
`framework/crud` first, then the rest of `framework/`, `core/`,
`core-ui/`, `battery/`, `cmd/`, `internal/`.

Done when non-test Go code is under 8% comment lines, from 24% today.
`scripts/comment-ratio.sh [dir...]` counts them, and prints the share
for `_test.go` files on a second line, which a sweep should not raise.

Status: `framework/crud` is swept, 29.2% to 6.3%. Its rules are 185
invariants, each naming a test that was watched failing, and 79
decisions. The root `framework` package is swept too, 33.1% to 5.1%: 397
more invariants and 151 decisions on `app` and its children.
`framework/headless` is swept, 31.4% to 6.8% (300 invariants, 124
decisions); `framework/uihost`, 35.0% to 4.1% (351 and 114);
`framework/ui`, 24.0% to 6.8% (385 and 123). The replay eval after them
held every number (`2026-11-16-replay-ui-sweeps.md`). Six more packages
are swept: `framework/contracts` 20.8% to 3.1% (163 invariants, 33
decisions), `framework/migrate` 26.4% to 4.4% (98 and 23),
`framework/entityui` 22.2% to 3.7% (65 and 6), `framework/embed` 44.5% to
7.7% (54 and 33), `framework/entity` 31.4% to 7.6% (46 and 8) and
`framework/access` 35.3% to 7.6% (41 and 6), each with an owner in
`TestCoveredPackagesHaveNoUnownedFile`. The rest of `framework/` outside
`framework/experimental` is swept too: 53 package directories in eight
groups (image, static, events, query, local, ui, testing, small), each
from between 10.9% and 95.8% down to under 8%, except thirteen small packages
whose remaining comments are one-line docs on exported identifiers
(`factory`, `agentsinv`, `owner`, `routegroup`, `lifecycle`, `hook`,
`softdelete`, `db`, `tenant`, `imagefield`, `slowquery`, `db/sqlbusy`,
`sdkdocs/internal/docpage`). Those groups added 669 invariants, 234
decisions and 8 twins. `framework/experimental` (harness, webmcp,
apiversions) is swept too, at 4.5%: `harness` 4.3% (286 invariants, 112
decisions, 38 twins, one primitive per job), `webmcp` 7.3% and
`apiversions` 11.8%, all of it exported docs. The replay eval after it and
the first twelve `core/` packages held every number it can see, but no
replay task reads those packages
(`2026-11-19-replay-step2-experimental-core.md`). `framework/` as a whole
is at 5.5%.

`core/` is swept, 23.4% to 5.3%, its tests 12.9% to 12.6%. Every one of its
32 package directories is in `TestCoveredPackagesHaveNoUnownedFile`.
Per package (before, after):

| Package | Before | After | | Package | Before | After |
|---|---|---|---|---|---|---|
| a2a | 24.1% | 6.4% | | migrate | 30.4% | 3.4% |
| acp | 27.1% | 7.5% | | moduleproto | 44.5% | 12.3% * |
| backoff | | 6.2% | | netguard | 43.5% | 6.9% |
| config | 28.7% | 3.6% | | openapi | 22.8% | 7.3% |
| datexport | 44.3% | 7.9% | | query | | 7.7% |
| dotenv | | 2.5% | | render | | 4.2% |
| fanout | 44.1% | 7.2% | | router | | 5.1% |
| featureflag | 37.0% | 7.7% | | schema | | 5.7% |
| fuzzy | | 6.1% | | settings | 17.8% | 7.4% |
| handler | 29.3% | 7.2% | | static | 37.1% | 4.1% |
| i18n | 20.7% | 4.7% | | stream | 38.4% | 7.9% |
| jcs | | 1.0% | | textsafe | | 6.4% |
| markdown | 28.2% | 1.5% | | tracing | 16.9% | 3.0% |
| mcp | 13.2% | 3.7% | | unmounted | 35.3% | 19.5% * |
| middleware | | 2.9% | | upload | 36.9% | 5.7% |
| | | | | webbotauth | 22.5% | 2.1% |
| | | | | yaml | | 0.0% |

A blank "before" was swept in an earlier stack. `core/mcp` was swept
there too; a merge from main (92dec879) brought back the comments of five
of its files, and this stack swept them again. The two starred packages are
exceptions: what remains is a one-line doc on each exported identifier
(about 100 in `moduleproto`, a handful in the 41-line `unmounted`). The
stack (#810, #811, #815, #819 and the config PR) added 58 primitives, 375
invariants and 127 decisions. It merged more than a dozen twins into one implementation
each, among them the outbound URL check and guarded client
(`netguard.CheckOutboundURL`, `GuardRoundTripper`), the cross-origin check
(`netguard.OriginAllowed`), the SSE frame encoder (`internal/sseframe`),
the fanout loop guard (`fanout.UnwrapForeign`) and the SQL lexer
(`internal/sqllex`). The registry now holds 112 twins left to merge, 15
more than before, each with a `why`. Fixed on the way: webhook
registration's unbounded DNS lookup, `Bind` accepting nested duplicate
keys (breaking), fanout queues delivering after `stop`; and, from review,
a WebSocket `Close` that could hang behind a keepalive Ping, a `Write`
racing `Close` that was accepted and dropped, the strict-key walk's
quadratic allocation, a hostless URL passing with private networks
allowed, and a `Close` panic escaping the static-file hash goroutine. The replay eval
after them held every number but the upgrade note, which one task missed
as it had before (`2026-11-19-replay-core-sweeps.md`).

`core-ui/` is swept, 32.0% to 5.2%, its tests 15.0% to 14.9%. All 26 package
directories are in `TestCoveredPackagesHaveNoUnownedFile`. Per package
(before, after; a directory counts its subdirectories):

| Package | Before | After | | Package | Before | After |
|---|---|---|---|---|---|---|
| app | 38.3% | 7.9% | | registry | 34.1% | 6.0% |
| app/decide | 57.1% | 20.6% * | | runtime | 30.6% | 2.0% |
| check | 30.0% | 1.5% | | runtime/internal/treegate | 19.5% | 5.1% |
| component | 36.7% | 15.0% * | | runtime/minify | 22.7% | 0.5% |
| compute | 14.0% | 8.2% * | | seo | 30.8% | 10.6% * |
| di | 23.6% | 3.5% | | store | 31.4% | 5.4% |
| html | 25.2% | 12.3% * | | style | 33.0% | 5.3% |
| interactive | 33.1% | 7.9% | | uinodev1 | 31.0% | 5.8% |
| island | 40.9% | 5.5% | | urlprefix | 21.7% | 3.4% |
| localdb | 29.0% | 8.6% * | | urlsafe | 43.8% | 8.2% * |
| node | 34.3% | 8.9% * | | widget | 43.1% | 6.8% |
| noderender | 25.7% | 0.7% | | widget/preset | 57.8% | 11.5% * |
| ownstyle | 19.1% | 3.4% | | widget/theme | 15.0% | 1.3% |

In the starred packages, what remains is a doc on each exported
identifier, except for two lines in `urlsafe`. The two groups added 48
primitives, 263 invariants and 103 decisions, plus 46 tests for rules that
had none. They also recorded 6 twins and merged one (island's two
roster loops, now `localMembersLocked`). One bug was fixed on the way:
`registry.IsolateForTest` did not isolate the behavior-marker attributes.
The replay eval after the sweep held every number, moving one cell at most,
but no replay task reads `core-ui/` (`2026-11-20-replay-coreui-sweep.md`).

`battery/` is swept, 25.5% to 4.7%, its tests 11.2% to 11.0%. All 38 package
directories are in `TestCoveredPackagesHaveNoUnownedFile`. Per package
(before, after; a directory counts its subdirectories):

| Package | Before | After | | Package | Before | After |
|---|---|---|---|---|---|---|
| admin | 18.3% | 2.9% | | queue | 32.2% | 4.4% |
| auth | 30.3% | 4.1% | | relay | 30.9% | 3.0% |
| auth/authui | 28.0% | 6.3% | | rtc | 29.8% | 4.0% |
| cache | 33.7% | 5.8% | | search | 27.5% | 5.1% |
| desktop | 21.0% | 4.9% | | semantic | 23.3% | 3.8% |
| desktop/internal/fakecgo | 77.4% | 8.6% * | | service | 21.1% | 1.5% |
| email | 26.6% | 3.2% | | setup | 32.2% | 3.8% |
| log | 31.3% | 6.3% | | sitesearch | 20.2% | 4.8% |
| notify | 34.6% | 7.7% | | storage | 24.5% | 7.0% |
| print | 33.6% | 7.5% | | telemetry | 18.8% | 4.0% |
| print/chromepdf | 39.3% | 7.8% | | webhook | 30.3% | 6.8% |

In `fakecgo`, starred, what remains is a one-line doc on each exported
function and the package. The four groups (desktop; auth; admin, queue,
semantic and webhook; the thirteen others) added 119 primitives, 837
invariants (23 judged only) and 133 decisions, plus 214 tests for rules
that had none. Twelve batteries got their first top-level primitive
(`desktop`, `log`, `cache`, `email`, `search`, `sitesearch`, `storage`,
`setup`, `relay`, `print`, `notify`, `telemetry`); the seven that had one
were extended. They recorded 17 twins and merged two (desktop's two grant
stores now share `checkDecision`, and `desktoptest` uses
`desktop.MainWindowID`). Outside new tests the only other code changes are
two moves that split files by job (`admin.go` into `gate.go` and
`opspost.go`, `email/smtp.go` into `message.go`) and one bug fix: a
relative `desktop.Config.DataDir` was accepted though its doc said it
must be absolute, because the helper that checked it was never called
(breaking: `New` now panics on one, and `Init` creates a missing one
owner-only). Also fixed on the way: six tests weaker than their rules
(`TestOAuth2Plugin_StateExpiry` minted a state that failed to parse
before its expiry was read, `TestOIDCSec_WeakRSAKey` signed with the
strong key, `TestWebhookMetrics_CountersAndCollector` let an attempt count
twice, two mail-queue panic tests returned before the worker's fate was
known, and a search test named for a clause it did not check), and two
docs that said the opposite of the code (`search.md` on a non-string
weighted field, webhook's `Options` on a caller's HTTP client). Every
proof is in `docs/reviews/2026-11-20-battery-sweep-mutations.md`. Review
bots skipped the PR for its size, so four agents that did not write it
reviewed it adversarially (hard rule 13,
`docs/reviews/2026-11-21-battery-sweep-adversarial-review.md`): about 220
mutations of its new invariants, of which about 45 survived, and about 40
rules from deleted comments that nothing guarded, among them a TOTP code
that could verify two sessions, the updater's archive checksum and a
cached response served to an anonymous caller if the code regressed. Each
got a test watched failing or a judged invariant; the fixes found two more
real bugs, a temp path in a storage rename error and semantic answers
without `no-store`. Left
unproven on Linux: the macOS and Windows desktop rules, whose tests run
only there, and storage's case-folded keys, whose tests skip on a case-sensitive filesystem. The replay eval after it held
every number but `form-field-typo`, whose runs swing between one sibling
path and all five (11 of 30 here, 34 of 60 over three batches), and
`node-block-entity`, which was back at 22 of 24 on a rerun; no replay
task reads `battery/` (`2026-11-20-replay-battery-sweep.md`).
`dup-column-key` reads an entry that names its sibling sites and went
from 1 of 4 to 4 of 4 (`2026-11-20-dup-column-key-twin.md`).

Next: `cmd/`, `internal/`. The repository as a whole is at 10.4%.

### 3. Bring the known twins in, and merge them

Follow the `twin-sweep` skill. Every open family from the bug-hunt
ledgers becomes a `twins` entry, then a merge. A twin stays only where
code cannot be shared, with a parity test.

Done when `go run ./cmd/primitives lint` counts no twin whose reason is
work not yet done.

### 4. Cover the rest of the framework

A primitive for each part of `framework/`, `core-ui/` and `core/`, each
with its docs and its `on_change` lines. This is what makes the stop
check useful outside `framework/crud`.

Done when `go run ./cmd/primitives for <path>` finds an owner for every
non-test Go file under those three trees.

Status: done ahead of order for the root `framework` package, so its
sweep had owners to write to: `app` and its children, `app.processmodule`
among them, own all 87 files. `TestCoveredPackagesHaveNoUnownedFile`
fails when a file in a covered package has no owner; add a package to
its list as this step reaches it.

`framework/headless` (`headless` and five children), `framework/uihost`
(thirteen children; `uihost.go` was split into files by job so each has
one) and `framework/ui` (ten children beside `ui.styling`) are covered
too, ahead of their sweeps.
`framework/contracts` (engine, config, report and four analyzer
families), `framework/entityui` (list, bulk, record), `framework/migrate`
(diff, constraints, sqlite, objects), `framework/embed` (credential,
origin, reach), `framework/entity` (display, states) and
`framework/access` (store) are covered as well.
Every other package directory under `framework/` outside
`framework/experimental` is covered too: 91 primitives over 53
directories, split by job.

A rule that spans primitives keeps one home and lists the other files
it governs under `applies` (settled in step 1: `crud.scope`'s
one-builder rule reaches `eager.go` and `include.go` that way).

## Rules for all four

- Use Opus for the work and for the eval runs.
- After each step, rerun the replay eval and add the numbers to
  `evals/primitives/results/`. A step that makes them worse is not done.
- When a bug hunt finds a miss the harness should have caught, add it:
  a twin, an `on_change` line, or a replayed bug in the eval.
- Report an eval with the `eval-report` skill.

## Not planned

- Generating runtime code from the registry. It generates import tests
  and nothing has asked for more.
- A model that judges whether code follows an invariant. The `judge`
  field and `primitives judge` exist; no eval has shown a rule that only
  a judge could hold.
