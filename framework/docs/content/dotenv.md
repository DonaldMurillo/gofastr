# `.env` files (`core/dotenv`)

`framework.NewApp()` automatically loads `.env` files at the start of construction before options run, so plugins, batteries, and config callbacks see merged values.

## Precedence and load order

1. **Real process `os.Environ` always wins:** `dotenv.Apply` only sets variables not already present in the environment, so operator-supplied `DATABASE_URL=...` always overrides dotfiles.
2. `.env.local` (gitignored per-developer secrets)
3. `.env.<APP_ENV>` (only when `APP_ENV` is set, e.g. `.env.production`)
4. `.env` (committed shared defaults)

Missing files are skipped; malformed files fail fast with file name and line number (never printing the line's text so secrets don't leak into logs).

## File format and `${VAR}` expansion

```dotenv
# comments and blank lines are allowed
FOO=bar                       # bareword value
QUOTED="hello world"          # double-quoted: \n \t \" \\ and ${VAR} expanded
LITERAL='hello\nworld'        # single-quoted: verbatim, no escapes or expansion
export PORT=8080              # optional export prefix
DSN="postgres://${DB_HOST}/app"
ESCAPED="\${not expanded}"    # \$ prevents expansion
```

Rules:
- Keys match `[A-Za-z_][A-Za-z0-9_]*`. Multi-line values are not supported (use `\n` in double quotes).
- Inline `#` comments after **unquoted** values become part of the value — quote values when appending comments.
- Only bracketed `${VAR}` inside double quotes expands (bare `$VAR` is left literal). Lookup checks `os.Environ`, earlier files in the precedence chain, then earlier keys in the same file, without re-expanding substituted values (no cycles or double-expansion).

## Reading env vars before `NewApp`

If `main.go` opens a database before calling `framework.NewApp`, load the exact same file list first using `framework.DefaultDotEnvPaths()`:

```go
_ = dotenv.LoadAndApply(framework.DefaultDotEnvPaths()...)
db, err := sql.Open(driver, getEnv("DATABASE_URL", "file:app.db"))
```

Never hardcode just `dotenv.LoadAndApply(".env")` before `NewApp` — because existing env vars win, loading `.env` first would pin base defaults and block `.env.local` / `.env.<APP_ENV>`.

Set `GOFASTR_DOTENV=off` in the **process environment** to disable automatic loading in `NewApp` (useful in hermetic integration tests).

## Common mistakes

- **Expecting `.env` to override an exported shell variable:** real environment variables always win by design.
- **Putting `GOFASTR_DOTENV=off` inside `.env`:** must be set in the process environment before `NewApp` runs.
- **Using bare `$VAR` or single quotes for expansion:** only `"${VAR}"` inside double quotes expands.
- **Appending `# comment` after an unquoted value:** becomes part of the value; quote the value first.
- **Loading only `.env` before `NewApp`:** use `framework.DefaultDotEnvPaths()...` so `.env.local` and `.env.<APP_ENV>` retain precedence.
