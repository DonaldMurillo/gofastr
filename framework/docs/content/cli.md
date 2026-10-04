# The gofastr CLI

```bash
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@vX.Y.Z
```

One binary. `gofastr <command> --help` prints every flag; this page maps
each command to the doc that covers it.

## Scaffold

- `gofastr init <name>`: a new project with framework UI, `DESIGN.md`, a
  sample entity (`--no-entity` to skip), git, and the agent onboarding
  files. `--module=<path>` sets the Go module; `--db=sqlite|postgres`
  picks the driver (default `sqlite`; `sqlite3` and `postgresql` are
  accepted aliases, anything else is an error). The DSN lives in the
  0600 gitignored `.env` only: with `--db=postgres` the generated
  `main.go` reads `DATABASE_URL` from the environment and fails closed
  pointing at `.env` when it is unset — no DSN literal in committed
  source (a sqlite `file:<app>.db` path stays inline; it is not a
  credential). `init . --reinit` refreshes the
  AI-agent onboarding files in place (`--force` overwrites your edits);
  no Go code or git changes. A released CLI pins the generated `go.mod` to its
  matching GoFastr version. A local development build prints the exact
  `go get …@vX.Y.Z` step because it cannot infer a release safely.
- `gofastr new handler <name>` / `gofastr new route <path>`: scaffold
  one handler or route registration into an existing app.
- `gofastr agents init|sync|skill`: generate or refresh `AGENTS.md`
  and the per-battery detail files under `agents/`.
- `gofastr theme edit`: a local theme configurator with every token as a
  control, the whole component gallery as a live preview, and write-back to
  `theme/theme.go`.
- `gofastr theme init`: scaffold a typed `theme/theme.go` you own
  (`--out=<path>` writes elsewhere; `--force` overwrites).

## Blueprints

- `gofastr validate <yml>`: validate a blueprint without generating
  (exit 0 = valid; includes the unscoped-PII lint).
- `gofastr generate --from=<yml>`: one-shot scaffold of the whole app
  as owned Go ([tutorial](tutorial-blueprint-app.md)). `generate --add`
  / `generate entity <name>` / `generate screen <name>` scaffold *new*
  files into an existing app; owned files are never touched.
  `generate screen <name> --from-a11y=<file>` builds the screen from a
  Playwright aria snapshot ([blueprints](blueprints.md)).
  `generate --config=<codegen.yml>` runs the configurable codegen engine
  ([codegen](codegen.md)); `generate --watch` re-runs on every change.
  `generate all` is the full-project path (same engine as `--from`).
- `gofastr generate package [<name>]`: copy a canonical chrome package
  (`siteheader`, `sitefooter`, `docpage`) into the app as owned code.
  No name lists the packages with a one-line summary each. With a name,
  every file of the package lands in `--out=<dir>` (default `./<name>`):
  the Go, the owned `.style.css`, its `.tokens.css` when there is one,
  the `_style.gen.go` / `_tokens.gen.go`, and the tests. The package's
  self-import is rewritten to the path of the `go.mod` that encloses
  the target, so a copy under a nested module (`tools/go.mod`) imports
  through that module. The command needs an enclosing `go.mod` and
  refuses a target outside the module. Inside it, `--out` may be
  relative to the current directory (`../chrome/hdr` from a
  subdirectory works) or absolute, and `--dry-run` accepts exactly the
  targets the copy can write. The copy is one-shot: a non-empty target
  is refused, and a file that appears in the target mid-copy is never
  overwritten. The copy fails instead and removes the files and
  directories it created, leaving everything else alone, so the command
  can be re-run. After the copy the package is yours: edit the Go
  freely, run `gofastr gen styles` after editing a sheet, `go mod tidy`
  once for the chromium test's chromedp dependencies. A blueprint with
  marketing screens writes the same packages (tests included) through
  the same copy, so the two cannot drift.
- `gofastr pack [app-dir]`: snapshot a generated app into a
  best-effort `gofastr.yml`. Lossy; not an inverse of `generate`.

### Owned styles (`generate styles`)

- `gofastr generate styles [patterns]`: first, every `<name>.tokens.css`
  under the package patterns is checked, one set per program (a `main`
  package plus the packages its imports reach, the grouping
  `gofastr verify` uses), and `<name>_tokens.gen.go` is written beside
  each clean file: the app's own typed tokens, for `Theme.Extend` (format and rules in
  [theming](theming.md#app-tokens-in-css-nametokenscss)). Then, for
  every `<name>.style.css` under the package patterns (Go-style, relative to the working
  directory; `./...` by default; `vendor/`, `testdata/`,
  `node_modules/` and dot directories are skipped), run the owned-style
  checks and write `<name>_style.gen.go` beside the CSS, in the
  directory's existing Go package. Every finding prints as
  `file:line:col: severity GOFASTRnnnn message`; a file with an
  error-severity finding generates nothing, the run continues so one
  pass reports everything, and the exit code is non-zero. `app.style.css`
  is the app sheet; every other name is a scoped owner. Names match
  `^[a-z][a-z0-9-]*$`, must not start with `ui-`, and are unique within
  each program.

  The generated file carries the CSS as a raw-string constant with a
  `// Source hash: sha256:<hex>` header, registers it as
  `var Style = <name>Style{ownstyle.Must("<name>", kind, <name>CSS)}`,
  and exposes the class vocabulary: `.key` becomes `Key() string`,
  `.column.over-limit` adds `ColumnWith(ColumnVariants{OverLimit:
  true}) string` (`"column over-limit"`), `.priority--urgent` becomes
  the `Priority` string type with `ParsePriority("urgent")
  (Priority, bool)`, and `:scope.fresh` adds `Root()`/`RootWith(...)`.
  CSS doc comments become Go doc comments. A package holding exactly
  one style file exports `Style`; a package holding several exports
  each as `<Owner>Style` (`IssuecardStyle`, `BoardStyle`). A class
  name that becomes an invalid or colliding Go identifier is a
  generator error, never a mangled name.

  The sheets are checked against the built-in tokens plus every app
  token, so `gofastr gen styles` and `gofastr verify` report the same
  findings. After the last sheet, a literal written in two or more
  sheets for the same token type is a GOFASTR1822 warning. A
  `/* gofastr:allow(GOFASTRnnnn) reason */` comment waives that rule on
  its line (or the next line with code); a marker with no reason waives
  nothing.

  The command also writes `.gofastr/tokens.css`: every theme and app
  token as `--name: <light value>; /* dark: <value> */` under `:root`,
  for editor completion while writing `.style.css` files. It is never
  served. `gofastr verify` fails (GOFASTR1814) when a `*.style.css` or
  `*.tokens.css` has no generated sibling, or the sibling's source hash
  no longer matches the CSS.

## The daily loop

- `gofastr dev`: rebuild on save, browser livereload, contract findings
  for what you changed (after the reload, never blocking it), and the dev
  MCP tools for your coding agent ([dev-livereload](dev-livereload.md)).
  `--dir` sets the watch root, `--pkg` the main package under it,
  `--addr`/`-p` the port; `--no-a11y` skips the accessibility lint on
  each rebuild.
- `gofastr build`: codegen + `go vet` + accessibility lint + the embed
  server-action gate + contract verification + `go build`. Only
  error-severity contract findings stop the build (`gofastr verify` is
  the full report; an existing app adopts the gate with a baseline, see
  [contracts](contracts.md)). Flags: `--no-a11y` skips the a11y lint,
  `--no-embed-check` skips the embed gate, `--no-contracts` skips the
  contract gate, `--no-generate` skips codegen, `--pkg` selects the main
  package, and `-o`/`--output` names the binary (default `bin/server`).
  `--allow-unverified-embeds` keeps proven embed violations fatal while
  downgrading a surface the analyzer cannot follow ([embed](embed.md)).
- `gofastr test`: run the project's tests.
- `gofastr docs [topic]`: these docs, offline, versioned with the
  binary (`--list` every topic, `--grep <term>` to search).

## Ship

- `gofastr migrate up|down|status|generate|force|repair`: versioned
  migrations, advisory-locked, checksum- and dirty-state-guarded
  ([migrations](migrations.md)); `repair` rebuilds SQLite tables carrying
  the stale pre-v0.67 owner-column key
  ([SQLite driver](sqlite-driver.md)).
- `gofastr generate cli`: a customer-facing terminal client for your
  API, with scoped API-token auth ([app-cli](app-cli.md)).
- `gofastr generate sdk`: Go + JS/TS clients your app can host behind
  a live docs page ([sdk](sdk.md)).
- `gofastr upgrade`: move to a newer release. Lists every migration
  note between your `go.mod` version and the target (`--to vX.Y.Z`;
  without it the newest tagged release is resolved via the proxy) and
  points at the exact lines each change affects — Go through the type
  checker, CSS through its tokenizer, `gofastr.yml` through its parser;
  the registry format is documented under "The migration registry"
  below. `--apply` runs the steps ([upgrading](upgrading.md)). If
  `go.mod` was bumped before the run, pass `--from vX.Y.Z` (the release
  the code was written for); otherwise the range is empty.

### The migration registry (`gofastr upgrade`)

The registry lives in `internal/upgrade/` and is embedded in the CLI:
`registry.yml` holds the `through:` marker every release PR bumps and
the marker sinks, and `releases/<version>.yml` holds one file per
release that carries migration-relevant changes (the file name must be
its version). Each note is a one-line
`change`, whether it is `breaking`, a one-line `guidance`, and one of
two things: a `find:` block saying which code the change affects, or a
`nodetect:` one-liner saying why nothing can (a default that flipped, a
removed CLI flag). A breaking note with a `find:` also says what its
hits mean: `hits: edit` when every hit spells something the release no
longer accepts (a removed symbol, a changed signature, a class no
longer emitted), `hits: review` when the code may still be right (a
default that flipped, a stricter check). The report prints the edit
hits first, then the review hits, then the notes nothing matched; a
hit read from a compile error is always an edit. The parser is strict:
an unknown key, a regex that does not compile, a malformed symbol or a
breaking `find:` note with no `hits:` fails the build's registry
tests, never a user's upgrade. CI also refuses a `hits: review` note
naming a symbol its own release removed.

```yaml
# internal/upgrade/releases/v0.87.0.yml
version: v0.87.0
title: Site chrome moves to owned packages
notes:
  - change: 'ui.SiteHeader is now siteheader.Render'
    breaking: true
    hits: edit
    guidance: "Swap the ui.SiteHeader call for siteheader.Render(siteheader.Config{...})."
    find:
      uses: [gofastr/framework/ui.SiteHeader]
      strings:
        classes: [ui-site-header]
```

The scan type-checks every Go file the go tool would build in some
configuration, tests included:

- the module at or above the project root, scoped to the root;
- each nested module below it (its own `go.mod`), except under
  `vendor`, `testdata`, `node_modules` and directories starting with
  `.` or `_`, which the go tool skips too; a module the root's
  `go.work` already lists is loaded once;
- each file a build constraint keeps out of the host build
  (`//go:build windows`, `//go:build integration`, a `_linux.go`
  suffix), loaded under one GOOS, GOARCH and tag set that satisfies
  it. Only that file's hits are reported from that load, and an error
  in a file the host load already checked does not mark the app
  broken;
- a `//go:build ignore` file declaring a package other than its
  directory's: a program run by name, loaded alone the way
  `go run gen.go` builds it.

A file none of those reach is listed in a NOTE at the top of the
report, with its reason: a constraint nothing satisfies
(`linux && windows`), a `//go:build ignore` file declaring its
directory's own package (disabled code that no build includes), or a
configuration that did not compile it. Read those by hand.

Each matcher reads the code the way its language means it:

- `uses`: Go objects, spelled `import.path.Name` or
  `import.path.Type.Member` (a leading `gofastr/` is the module
  shorthand). Every reference the type checker resolves to the symbol is
  a hit — calls, selectors, method values, embedded promotion,
  composite-literal keys, generics, uses through an alias of the
  symbol's type. A member of an interface also matches every method
  an app type declares to implement that interface, whether or not
  its package imports the interface's.
- `shapes`: a use of a symbol that SURVIVES the release with a new
  shape, matched by the type that use resolves to. The type string is
  what a declaration spells with package names, not paths, and
  parameter names appear as the release wrote them; the object
  resolves against the gofastr version the app builds with today, so
  an entry written for the old shape hits before the port and falls
  silent after it. That is the point: `uses` fires on the old and the
  new spelling of a changed shape alike, so a ported app can never go
  clean on it. When the app no longer compiles against the version it
  is moving to, a compile error on a line where the symbol resolved to
  another shape is reported under the note with the error attached, so
  the pre-port scan still finds the call, and so is an error anywhere
  that names the symbol, such as an interface assertion failing on the
  method that changed. A member of an interface also matches every
  method an app type declares to implement it, read against that
  method's own signature. An app alias of a kit type reads as the type
  it names, so the regex is written against the kit's own spelling.
  ```yaml
  shapes:
    - symbol: gofastr/core-ui/app.NewLayout
      type: '^func\(name string\) \*app\.Layout$'
  ```
  matches v0.85's single-argument `NewLayout` and not v0.86's
  `NewLayout(name, spec, build)`.
- `imports`: an import path, or a `path/...` subtree.
- `fields`: a composite-literal field, an assignment to it, or a keyed
  write into it. `field` names `Type.Field`, and at most one condition
  narrows it. `key` is a constant string key in the field's map: in a
  map literal, in the single initialiser of a variable the field is
  set from (one level, in any file or package, never a reassigned
  variable), or in `cfg.Field["key"] = ...`; keys compare with ASCII
  case folded, as HTML attribute names do. `value` is a regex the
  constant string value must match, found the same ways. `refused`
  names a `core-ui/urlsafe` policy (`anchor`, `resource`,
  `image_source`): the constant string must be a URL that policy
  refuses, the predicate the component itself applies at render.
- `strings`: constant-folded Go string values. `classes` matches a
  whitespace-delimited class token (BEM variants included), or a
  `.name` selector when the value holds a CSS rule block. In a value
  holding markup only class attributes count, read through the same
  tokenizer as the runtime check below: any quoting, case or spacing,
  character references decoded, only the first of duplicate class
  attributes; `attrs` an
  attribute name (a trailing `-` is a prefix); `properties` a `--x`
  custom property; `match` a regex, the last resort.
- `css`: `.css` files through the CSS tokenizer. `classes` matches a
  class selector, `properties` a custom property, and `selectors` a
  whole compound selector such as `.cui-pos-center > .cui-slot`
  (whitespace around combinators does not matter). Escapes decode
  first: `.ui\2d button` is `.ui-button`.
- `config`: `gofastr.yml` keys (`*` matches any one key or list item),
  with an optional `value` regex over the scalar or a `refused` URL
  policy, as in `fields`.
- `gomod`: the `go` directive; `go_below: "1.27"` flags an older one.
- `text`: a per-line regex over files matching a glob, for languages
  nothing above reads (shell, JS). Go and CSS files are refused: their
  matchers read them structurally. So is a glob that would never
  match: a malformed segment, or `**` sharing a segment with other
  characters.

When the app does not type-check against its current gofastr version
(the `go.mod` was bumped first), the Go matchers fall back to the
compile errors: an error message naming a `uses` symbol or a `fields`
entry with no key or value condition (under the declared type or a
type alias of it, such as `framework.EntityConfig` for
`framework/entity.EntityConfig`; the quoted import path when two
imports share a name, and the bare name under a dot import, count
too), or a missing `imports` package, is a hit at the error's
position. An error on a line a matcher already hit counts as
explained only when the error names that hit's symbol (a changed
method signature still resolves, so the call is found even though the
error text names no package; a changed field type names only the
struct literal, and that counts); an unrelated error on the same line
stays in the report. Errors no note explains are listed at the end
of the report.

`marker_sinks:` (top level) lists where a kept `ui-*` name is an
identifier, not a class: `calls` (a function or method argument
position), `fields` (a struct field), `attr_keys` (a map-literal key).
A `strings.classes` value that only reaches marker sinks is not a hit,
so registered sheet names and `data-cui-comp` markers stay silent.
A prose argument of a `testing` method (`t.Fatal`, `t.Errorf`, `t.Log`,
`t.Skip`, and `t.Run`'s subtest name) is never markup the app renders,
so no string matcher reads it; `t.Setenv` hands its arguments to the
code under test, so those are read.

The registry has a second reader. `gofastr upgrade` scans an app's
*source*; a name built at run time (`fmt.Sprintf("ui-%s", kind)`, a
class read from the database) never appears there, but the rendered
HTML can't hide it. So in Go test binaries and under `gofastr dev`, the
framework scans each markup response it serves (pages, island RPC
answers, widget chrome, HTML carried in JSON signal values) for the
registry's retired classes and `data-cui-*` attributes, read the way a
browser reads them (nothing inside comments or raw-text elements such
as `script`, `iframe` and `noscript`; character references decoded;
the first of duplicate attributes only):
`framework.TestHarness` fails the
test on each one, and `gofastr dev` warns once per path and name
([testkit](testkit.md)). Production binaries never scan and never load
the registry — the check has no serving cost and no link on
`golang.org/x/tools`.

## Verify

- `gofastr verify [capability...]`: the contract analyzers. Covers routing,
  permissions, security, data, entities, architecture, rendering,
  accessibility, performance, testing, ai. Strict by default; relax in
  `gofastr.contracts.yml` or waive one instance with
  `//gofastr:allow(RULE) reason` ([contracts](contracts.md)).
- `gofastr verify --list` / `--explain <rule>`: the rule catalog, and
  any one rule in full: why it matters, how to fix it, a worked example.
- `gofastr verify --json` / `--sarif <file>`: machine-readable output.
  Each JSON diagnostic carries its whole rule, so an agent acting on one
  finding needs no second call.
- `gofastr verify --fix`: apply the mechanical fixes, then re-verify.
- `gofastr verify --changed[=<ref>]`: report only findings in files this
  change touched; the analysis still runs whole-tree, so cross-file
  findings are still caught. For pre-commit hooks and PR review.
- `gofastr verify --strict --baseline-write`: record today's findings as
  accepted debt so only NEW ones fail. How an existing codebase adopts the
  gate.
- `gofastr verify --rule <id> --fix`: apply one rule's fixes at a time
  so edits stay reviewable; `--analyzer <name>` scopes to one analyzer,
  `--config <file>` picks a non-default config, `--no-vet` skips `go vet`.

## Audit

The `audit` subcommands predate `verify` and remain for the two things it
does not cover: a runtime browser scan, and the dependency report.

- `gofastr audit a11y --url <base>`: axe-core scan of a running app in
  both color schemes (`--email`/`--password` log in first). The
  password takes the `-` marker and is then read from
  `GOFASTR_AUDIT_PASSWORD` or piped stdin; a literal value still
  works but warns that argv is visible to every local process via
  ps. The *static* accessibility rules are part of
  `gofastr verify accessibility`.
- `gofastr audit lint`: the original AI-mistake scanner. Superseded by
  `gofastr verify security data`, which covers the same rules with a
  reason and a fix attached to each.
- `gofastr audit deps`: list dependencies that perform init-time
  global registrations.

## Extras

- `gofastr semantic index|watch|query|stats|clear`: the local semantic
  index ([semantic search](semantic-search.md)).
- `gofastr harness`: the experimental agent harness (`harness mcp`
  runs it as a stdio MCP server; `harness creds` manages encrypted API
  keys — `creds add <provider> <account> -` reads the secret from
  `GOFASTR_HARNESS_SECRET` or piped stdin, and a literal argv value
  warns the same way).
- `gofastr version`: print version info.

## Common mistakes

- **Updating `go.mod` but not the CLI** (or the other way around). They
  version independently. After `go get …@vX.Y.Z`, also
  `go install …/cmd/gofastr@vX.Y.Z`. `gofastr upgrade --apply` keeps
  them in step ([upgrading](upgrading.md)).
- **`generate --force` on an app you've edited.** It regenerates the
  *entire* set and discards your changes. To add to an existing app use
  `generate --add` / `generate entity <name>`; owned files are never
  touched there.
- **`dev --pkg ./cmd/server` from the wrong directory.** Keep `--dir`
  at the project root and point `--pkg` below it; otherwise the watcher
  misses `internal/` and relative paths (a sqlite `db_url`, static
  dirs) resolve against the command directory.
- **`migrate force` as a routine fix.** It only rewrites the tracking
  table. It's for dirty-state recovery or adopting a baseline. Read
  [migrations](migrations.md) first.
