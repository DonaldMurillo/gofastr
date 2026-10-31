# Bugbot review rules for GoFastr

The repo's `CLAUDE.md` hard rules are the review contract. Flag a change that
breaks one of them and name the rule by number. Read
`core-ui/ARCHITECTURE.md` and `framework/ARCHITECTURE.md` when a change
touches UI, the runtime, or the `framework/` package layout.

## What is already checked

`make analyze` (the vet analyzers in `internal/analyzers/`) and the
contracts pipeline (`framework/contracts`, `gofastr verify`) run in CI.
They already catch map ranging while writing output, unbounded request and
response bodies, control bytes reaching log sinks, world-readable files,
fixed temp paths, credential comparisons with `==`, timestamp-minted ids,
discarded decode errors, bespoke CSS, hard navigation, bespoke
`EventSource`, and bare user-facing English that skips `i18n.Phrase`. Do
not repeat those findings. Spend the review on bugs a type-aware lint
cannot see: wrong logic, missed edge cases, races, broken contracts
between packages, and tests that pass without proving anything.

A `//gofastr:allow(<analyzer>) <why>` comment marks a site that has the
shape on purpose. Flag it only when the stated reason is wrong.

## Hard rules to enforce

1. In-page state stays on its route. A list's sort, page and filter live
   in its query string; an embedded region (a pager in a card, an expand)
   is an island. Flag a new route added for either.
2. Pagination, sort and filter math runs on the server. Flag JS that
   re-implements it.
3. SSE is push-only and lives on the single `/__gofastr/sse` bus. Flag SSE
   used to answer a user action. Dev-reload tooling (`framework/dev`,
   kiln build mode) is the only exception.
4. Flag `location.href = ...` or a full page reload added as a fix.
5. A new `data-cui-*` attribute needs matching changes to
   `core-ui/ARCHITECTURE.md` and the runtime tests in the same PR.
6. An entity that holds per-user data and is exposed through auto-CRUD
   must set `EntityConfig.Scope.OwnerField`. A missing owner scope is a
   data leak between users. Treat it as high severity.
7. Generators and apps ship no bespoke CSS and no hand-rolled structural
   markup. Styling belongs in `framework/ui`, `core-ui/app` and
   `core-ui/style` tokens. Warning signs: a `*BaseCSS` string growing, an
   invented `.mrd-*` or `.gofastr-*` class, a raw CSS value where a
   `var(--*)` token exists, or overriding a component's internals from
   outside. The app's own `<name>.style.css` is allowed for its site
   header, footer and docs page only.
8. Before new UI markup, check `framework/ui` and `core-ui` for an
   existing component (Hero, Grid, Stack, Cluster, DetailList, Form,
   FormField, AuthCard, Sidebar, ThemeToggle, Card, Section, DataTable,
   StatCard, PageHeader and others). Flag a local copy of one.

## Docs ship with the code

A PR that adds, renames or removes an exported API, route, CLI subcommand,
JSON field or generated artifact must update `framework/docs/content/*.md`
in the same PR. Flag the missing doc change and name the file it belongs
in.

## Tests

- A guard condition needs a test that fails when the guard is removed. A
  new `if` that refuses input, with no test feeding it that input, is
  untested. Say which input the missing test should send.
- A test that reads files outside its own package must declare them with
  `//gofastr:test-reads <pattern>`, or `cmd/affected` will not select it
  when those files change.
- A fixed security finding turns its `*_red_test.go` probe into a
  `*_security_test.go`. A deleted probe needs its reason in the PR.

## Skip

Do not review `dist/`, `gen/`, `testdata/`,
`core-ui/runtime/frag/SYMBOLS.txt`, `evals/upgrade-fixtures/fixtures/` or
`internal/upgrade/releases/*.yml`. The upgrade fixtures are old-release
code on purpose.
