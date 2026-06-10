# .env files

`framework.NewApp()` reads `.env` files at the start of construction —
before any options run — so plugins, batteries, and option callbacks
that peek at `os.Environ` see the merged values.

## Defaults

Files are probed in this order (earlier wins on conflict):

1. `.env.local` — gitignored; per-developer secrets.
2. `.env.<APP_ENV>` — only when `APP_ENV` is set
   (e.g. `.env.production`).
3. `.env` — committed defaults shared by all environments.

Missing files are silently skipped; a malformed file fails fast with
the file name and line number.

## Precedence

Existing `os.Environ` always wins over file values. Operators expect
their explicit `DATABASE_URL=...` to override anything in a dotfile,
and the loader honors that. The framework calls `os.Setenv` only for
keys not already present.

## File format

A strict subset of the de facto dotenv spec:

```
# comments and blank lines are allowed
FOO=bar                       # bareword value
QUOTED="hello world"          # double-quoted: \n \t \" \\ interpreted
LITERAL='hello\nworld'        # single-quoted: VERBATIM, no escapes
export PORT=8080              # optional `export` prefix tolerated
PATH_TPL="${HOME}/bin"        # ${VAR} expansion (double-quoted only)
ESCAPED="\${not expanded}"    # \$ blocks expansion at that position
```

Hard rules:

- Keys must start with a letter or underscore; the rest is
  `[A-Za-z0-9_]`. Anything else is a parse error.
- Multi-line values are **not** supported. Use `\n` inside a
  double-quoted value.
- Inline comments after an UNQUOTED value are preserved as part of
  the value — write a quoted value if you need to embed `#`.
- The parser fails fast on malformed input rather than skipping the
  bad line (loud-by-default).

## Variable expansion `${VAR}`

Only inside double-quoted values. Bracket form **only** — bare `$VAR`
is left verbatim (less ambiguous, fewer footguns).

Lookup order for `${NAME}`:

1. Other keys already parsed from the same file (or earlier file in
   the precedence chain).
2. `os.Environ`.
3. Empty string (no error — matches shell behaviour).

Hardening:

- **Cycle detection.** `A=${A}` or any mutual chain `A=${B}` /
  `B=${A}` resolves to empty rather than looping.
- **Depth cap** (16 levels). Deeper chains stop expanding rather
  than blow the stack.
- **`\${...}` escape.** A backslash before `${` is preserved by the
  string-unescape phase so the expander sees `\$` and emits a
  literal `$` without triggering a lookup.
- **Malformed `${...` (no closing brace)** is left verbatim — never
  silently drops bytes.

## Disabling auto-load

Set `GOFASTR_DOTENV=off` in the **process** environment (not in a
`.env` file — chicken-and-egg) before `NewApp` runs. Useful when:

- You ship custom paths via `dotenv.LoadAndApply("ops/secrets.env")`
  before `NewApp` and don't want the default probe to also fire.
- You run integration tests that need a hermetic env.

## Using the package directly

```go
import "github.com/DonaldMurillo/gofastr/core/dotenv"

// Parse without touching os.Environ:
vals, err := dotenv.Load(".env.local", ".env")

// Apply (existing env wins):
loaded, skipped := dotenv.Apply(vals)

// One-shot:
err := dotenv.LoadAndApply(".env")
```

`dotenv.Expand(s, local, envFn)` is exposed for callers that want to
run the same `${VAR}` substitution against a custom lookup (e.g. when
loading a non-`.env` config and wanting consistent expansion rules).

## Common mistakes

- **Expecting a `.env` value to override the real environment.**
  Precedence is the other way: `Apply` only calls `os.Setenv` for keys
  not already present. An operator's `DATABASE_URL=…` always beats the
  dotfile — by design.
- **Putting `GOFASTR_DOTENV=off` in a `.env` file.** Chicken-and-egg:
  the loader would have to read the file to learn it shouldn't read
  the file. The kill switch only works as a process env var set before
  `NewApp` runs.
- **Using bare `$VAR` expansion.** Only the bracket form `${VAR}`
  expands, and only inside double-quoted values. Bare `$VAR`,
  single-quoted strings, and unquoted values are left verbatim.
- **Appending `# comment` after an unquoted value.** Inline comments
  after an UNQUOTED value are preserved as part of the value. Quote
  the value if the comment (or a literal `#`) shouldn't be in it.
