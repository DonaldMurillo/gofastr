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
  credential). With the sample entity, entity CRUD mounts under `/api`
  (`WithAPIPrefix("/api")`) and `battery/auth` is wired with `/login` and
  `/register` screens in `screens_auth.go`; `.env` also carries a
  freshly minted `JWT_SECRET`, which `main.go` reads from the
  environment. Auth dev mode (plain session cookie, relaxed login
  limiter) follows `GOFASTR_DEV`, which `gofastr dev` sets, so a copied
  `.env` never relaxes a deployment. `init .` refuses to run when git
  already tracks `.env` (untrack it with `git rm --cached .env` first),
  since the new secret would land in the next commit. `init .` names the
  project after its directory. Its module is the one an existing
  `go.mod` declares (a `--module` that disagrees is refused), else
  `--module`, else `local/<dir>`. It refuses, writing nothing, when the
  directory already holds a file it would replace (`main.go`, `screens.go`, `.env`,
  `gofastr.settings.yml`, the onboarding files, and with the sample
  entity `screens_auth.go` and `entities/entities.go`); `--force`
  overwrites them, each through a temporary file renamed over the old
  one, so a crash or a full disk leaves the old file whole and a
  replaced file keeps its mode. An existing `.gitignore` is kept and gains the entries
  it lacks. `init <name> --reinit` or `init . --reinit` refreshes the
  AI-agent onboarding files of an existing project (`--force` overwrites
  your edits); no Go code or git changes. An unknown flag is an error
  (exit 2). A released CLI pins the generated `go.mod` to its
  matching GoFastr version. A local development build prints the exact
  `go get …@vX.Y.Z` step because it cannot infer a release safely.
  The generated `go.mod` uses this CLI release's minimum Go version, even
  when the `go` on PATH is older, and a `toolchain` line only when the
  release prefers a newer toolchain than that minimum (`go mod tidy` drops
  one that repeats the `go` line). `init .` preserves
  higher requirements already set in `go.mod`, including an explicit
  `toolchain default`. Project names
  must use lowercase letters, digits, hyphens, or underscores and start
  with a letter or digit. Windows device names such as `con`, `nul`,
  `com0` through `com9`, and `lpt0` through `lpt9` are refused on every OS.
- `gofastr new handler <name>` / `gofastr new route <path>`: scaffold
  one handler or route registration into an existing app. `--method`,
  `--path` and `--handler` take `--method=POST` or `--method POST`; an
  unknown flag is an error.
- `gofastr agents init|sync|skill`: generate or refresh `AGENTS.md`
  and the per-battery detail files under `.agents/`.
- `gofastr theme edit`: a local theme configurator with every token as a
  control, the whole component gallery as a live preview, and write-back to
  `theme/theme.go`.
- `gofastr theme init`: scaffold a typed `theme/theme.go` you own
  (`--out=<path>` writes elsewhere; `--force` overwrites).

### Windows init reports an existing directory

`gofastr init <name>` checks for an existing destination before writing
files. It exits 1 if that directory exists and leaves it unchanged,
including `.env` and its signing key. A fresh project should exit 0.

If a complete scaffold appears but the captured result says "already
exists", check whether a launcher or agent runner invoked the command
twice. The first run can create the project while the captured error
belongs to the second run. Capture process IDs and arguments to check
whether both results came from one invocation.

Check which executable runs before changing the project:

```powershell
Get-Command gofastr -All
where.exe gofastr
gofastr version
```

Run that executable once from a normal terminal in a fresh temporary
directory, and record stdout, stderr, and `$LASTEXITCODE`. Check any
`.cmd` or `.ps1` wrapper and the runner's retry policy. Wait for a running
command's result instead of starting it again. `go mod init` and
`git init` can write progress to stderr even when they succeed; use the
exit code to decide whether a command failed.

Inspect a project that was already created before retrying. `init .`
refuses a directory that already holds the scaffold, and with `--force`
writes a new `.env` signing key; it is not a way to acknowledge a
successful scaffold. For a project whose scaffold completed, continue
with `go mod tidy` from that project directory.

### When the CLI and the project disagree

Every command that works on a project (all but `init`, `version`,
`docs` and `upgrade`; never `--help` or a mistyped command) compares the
GoFastr version the project's `go.mod` requires with the CLI's own
release. When they differ it prints a warning on stderr with the
`go install .../cmd/gofastr@<version>` line that fixes it, then runs the
command anyway: generated code and checks come from the CLI's release,
not the project's. A development build of the CLI, a `replace`
directive for GoFastr, and the GoFastr module itself skip the check.
`GOFASTR_NO_VERSION_CHECK=1` silences it.

## App specs

- `gofastr validate <yml>`: validate an app spec without generating
  (exit 0 = valid; includes the unscoped-PII lint).
- `gofastr generate app <yml>`: one-shot scaffold of the whole app
  as owned Go ([tutorial](tutorial-app-spec.md)). `generate app --add`
  / `generate entity <name>` / `generate screen <name>` scaffold *new*
  files into an existing app; owned files are never touched.
  `generate screen <name> --from-a11y=<file>` builds the screen from a
  Playwright aria snapshot ([app specs](app-spec.md)).
  `generate [--config=<settings file>]` runs the configurable codegen engine
  ([codegen](codegen.md)); `generate --watch` re-runs on every change.
- `gofastr generate package [<name>]`: copy a canonical chrome package
  (`siteheader`, `sitefooter`, `docpage`) into the app as owned code.
  No name lists the packages with a one-line summary each. With a name,
  every file of the package lands in `--out=<dir>` (default `./<name>`):
  the Go, the owned `.style.css`, its `.tokens.css` when there is one,
  the `_style.gen.go` / `_tokens.gen.go`, and the tests. The copy is an
  [unmount](unmount.md) of the package: its files carry the unmount
  marker, it gets an `unmounted.gen.go`, and it is recorded in the
  `unmounted:` section of `gofastr.settings.yml` beside the `go.mod`
  that encloses the target. The package's self-import is rewritten to
  that module's path, so a copy under a nested module (`tools/go.mod`)
  imports through that module. The command needs an enclosing `go.mod`
  and refuses a target outside the module. Inside it, `--out` may be
  relative to the current directory (`../chrome/hdr` from a
  subdirectory works) or absolute, and `--dry-run` accepts exactly the
  targets the copy can write. The copy is one-shot: a non-empty target
  is refused, as is a target inside another recorded copy, and a
  file that appears in the target mid-copy is never overwritten. The
  copy fails instead and removes the files and directories it created,
  leaving everything else alone, so the command can be re-run. After
  the copy the package is yours: edit the Go freely, run `gofastr gen
  styles` after editing a sheet, `go mod tidy` once for the chromium
  test's chromedp dependencies. An app spec with marketing screens
  writes the same packages (tests included) through the same copy, so
  the two cannot drift; those copies are not recorded.
- `gofastr unmount [<package>] [--out=<dir>] [--dry-run]`: copy a
  gofastr package (`battery/auth` or its full import path) and the
  packages under it into the app, rewire the module's imports to the
  copy and record it in `gofastr.settings.yml`. It refuses a package
  that imports gofastr internals or that a package outside the module
  (another gofastr package, a plugin) imports. No package lists the copies
  ([unmounting a package](unmount.md)).
- `gofastr remount <package> [--force] [--dry-run]`: rewire the imports
  back to the framework's package, delete the copy and its entry.
  Refuses a copy that changed since unmount unless `--force`.
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
  `^[a-z][a-z0-9-]{0,63}$`, must not start with `ui-`, and are unique within
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

  `ownstyle.Must` compiles the sheet against the default theme, and
  `App.WithTheme` compiles every registered sheet against the app's
  theme (`ownstyle.CheckTheme`): a theme that renames or drops a
  breakpoint a sheet's `(--above-md)` names panics at boot. A theme
  variant registered later that cannot compile a sheet does not fail
  the request: the sheet ships empty under that theme and the error
  is logged.

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

- `gofastr dev`: rebuild on save, browser livereload, a layout layer
  (Alt+Shift+L outlines the page's layout and flags overflow), contract findings
  for what you changed (after the reload, never blocking it), and the dev
  MCP tools for your coding agent ([dev-livereload](dev-livereload.md)).
  `--dir` sets the watch root, `--pkg` the main package under it,
  `--addr`/`-p` the port (not both); `--no-a11y` skips the accessibility
  lint on each rebuild. Each takes its value as `--addr=x` or `--addr x`;
  an unknown flag, or a flag whose value is missing, is an error. While the app is building, failed to build, or exited,
  the address answers 503 with what happened (the build error
  included) instead of refusing connections.
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
- `gofastr test [flags] [packages]`: run the project's tests (`./...`
  when no packages are named). Flags: `--run`, `--bench`, `--timeout`
  (each as `--run=X` or `--run X`), `--race`, `--cover`, `--short`; an
  unknown flag is an error.
- `gofastr docs [topic]`: these docs, offline, versioned with the
  binary (`--list` every topic, `--grep <term>` to search).
- `gofastr docs serve`: the docs website on `127.0.0.1:8083`, for
  offline browsing. The first run downloads the static export attached
  to this binary's release (about 6 MB, checked against its published
  SHA-256) into the user cache directory; later runs need no network.
  `--release vX.Y.Z` serves another release, `--refresh` downloads
  again (the old copy stays until the new one is in place), `--open`
  opens a browser, `--port N` moves it, and `--dir <path>` serves an
  export you built (`go run ./examples/site --export <path>`). Static
  pages, navigation, theme, the command palette and the site's own 404
  page work; server-backed demos do not. `--full` instead runs
  `go install github.com/DonaldMurillo/gofastr/examples/site@<tag>`
  (Go toolchain and network on first run, `--refresh` to rebuild) and
  starts the live site, so every demo works; it needs a fixed `--port`,
  refuses one that is already taken, and serves v0.82.0 or later. A local or `@main` build has
  no release, so it needs `--release` or `--dir`.

## Ship

- `gofastr migrate up|down|status|generate|force|repair`: versioned
  migrations, advisory-locked, checksum- and dirty-state-guarded
  ([migrations](migrations.md)); `repair` rebuilds SQLite tables carrying
  the stale pre-v0.67 owner-column key
  ([SQLite driver](sqlite-driver.md)).
- `gofastr export [--base /sub] [--pkg ./cmd/web] <dir>`: render every
  page into `<dir>` as a static site. It runs the app with
  `GOFASTR_EXPORT` set and `App.Start` exports instead of serving, so
  any app gets it ([static export](static-export.md)); a registered
  [site search](site-search.md) battery writes its index too.
- `gofastr generate cli`: a customer-facing terminal client for your
  API, with scoped API-token auth. Derived: every run rebuilds its
  `*.gen.go` files and keeps `custom.go` ([app-cli](app-cli.md)).
- `gofastr generate client [--force] [--dry-run] [--json]`: rebuild
  `entities/client/client.go`, the app's typed Go client, from
  `entities/*.go`. Plain `gofastr generate` and `gofastr build` run it
  whenever that file exists ([app spec](app-spec.md)).
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
its version). A PR that lands a breaking change writes its note into
`releases/<next version>.yml` in the same PR; that one file may sit
above `through` until the release PR keeps or renames it to the version
that ships (a file at or below `through` must name a CHANGELOG release). Each note is a one-line
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
naming a symbol its own release removed. A review note earns its
`find:` only when each hit is a place to decide something: a note
whose condition is an absence (an entity declaring no posture, an
owner field with no matching column), a fact of the database, or a
rendering decision no spelling shows is a `nodetect:` note, because a
hit on every ordinary use of a symbol says nothing the guidance does
not.

```yaml
# internal/upgrade/releases/v0.99.0.yml (an illustration, not a shipped file)
version: v0.99.0
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

The retired-markup check that test binaries and `gofastr dev` run over
rendered pages reads a table generated from this registry, not the
registry itself, so a release-note edit does not re-run every package
that serves markup. The table lists every kit class and attribute
(`ui-*`, `fui-*`, `data-cui-*`, `data-fui-*`, `data-hui-*`) a breaking
note's `find:` names, with that note's version and `change:` text; the
note above names `ui-site-header`. So run `go generate ./internal/retired`
and commit `internal/retired/retired_table.gen.go` whenever you add,
drop or rename such a name, edit the `change:` text of a note that names
one, or rename the release file that holds one (a pending release file
renamed to the version that ships). `TestTableIsCurrent` fails with that
command while the table is out of date.

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
  (whitespace around combinators does not matter). `declarations`
  lists `properties` (standard names, never a `--x`) with a `value`
  regex, matched against the declaration's value with whitespace
  collapsed and `!important` dropped: `outline-offset: 2px` against
  `'^[12]px$'`. An optional `glob` limits an entry to matching
  stylesheets (`"**/*.style.css"` for owned sheets only). Escapes decode first: `.ui\2d button` is `.ui-button`.
- `config`: `gofastr.yml` keys (`*` matches any one key or list item),
  with an optional `value` regex over the scalar or a `refused` URL
  policy, as in `fields`.
- `gomod`: the `go` directive; `go_below: "1.27"` flags an older one.
  Versions order the way Go's own `go` command orders them: a bare
  language version sits below every release of that minor, and a
  prerelease below the final release, so `1.26` < `1.26rc1` < `1.26.0`
  < `1.27`; a `go 1.27rc1` directive is not below `1.27`.
- `text`: a per-line regex over files matching a glob, for languages
  nothing above reads (shell, JS). Go and CSS files are refused: their
  matchers read them structurally. So is a glob that would never
  match: a malformed segment, or `**` sharing a segment with other
  characters. Minified scripts are skipped, by name (`.min.js`,
  `.min.mjs`, `.min.cjs`) and by line (a script line over 1000 bytes):
  a hit inside a vendored maplibre or monaco build points at code the
  app did not write and cannot act on. The line skip is for scripts
  only; a data file such as a `.jsonl` journal keeps its long lines.
  CSS never reaches the text matcher.

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
  `gofastr.settings.yml` or waive one instance with
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
  `--config <file>` picks a non-default config, `--no-vet` skips the vet
  stage (`go vet` and the GoFastr analyzers).

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
  global registrations, then the [unmounted](unmount.md) packages.

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
- **`generate app --force` on an app you've edited.** It regenerates the
  *entire* set and discards your changes. To add to an existing app use
  `generate app --add` / `generate entity <name>`; owned files are never
  touched there.
- **`dev --pkg ./cmd/server` from the wrong directory.** Keep `--dir`
  at the project root and point `--pkg` below it; otherwise the watcher
  misses `internal/` and relative paths (a sqlite `db_url`, static
  dirs) resolve against the command directory.
- **`migrate force` as a routine fix.** It only rewrites the tracking
  table. It's for dirty-state recovery or adopting a baseline. Read
  [migrations](migrations.md) first.

## Desktop applications

- `gofastr desktop build`: build the platform executable or bundle; Linux defaults
  to plain ELF and supports opt-in `--format=tar.gz` release archives.
- `gofastr desktop install --bundle=<extracted-directory>`: Linux-only, validate
  and install a per-user bundle and any embedded URL handler. Does not change
  saved default-handler preferences or autostart.
- `gofastr desktop uninstall --id=<reverse.dns>`: Linux-only, remove the unchanged
  owned installation, preserving application and browser-profile data.

`desktop run`, `build` and `types` take each value as `--id=x` or `--id x`
and refuse an unknown flag; a value that starts with a dash is written
`--sign=-x` (a lone `--sign -` is the ad-hoc identity).

See [desktop](desktop.md#the-gofastr-desktop-verbs) for the complete verb table,
runtime requirements and installation limitations.
