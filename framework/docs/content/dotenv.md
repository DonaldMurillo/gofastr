# `.env` files (`core/dotenv`)

`framework.NewApp()` automatically loads `.env` files at the start of construction before options run, so plugins, batteries, and config callbacks see merged values.

## Precedence and load order

1. **Real process `os.Environ` always wins:** `dotenv.Apply` only sets variables not already present in the environment, so operator-supplied `DATABASE_URL=...` always overrides dotfiles.
2. `.env.local` (gitignored per-developer secrets)
3. `.env.<APP_ENV>` (only when `APP_ENV` is set, e.g. `.env.production`)
4. `.env` (committed shared defaults)

`NewApp` skips missing files. If loading fails (for example on a malformed assignment), `NewApp` **panics** before applying any file values or running app options; the error names the file and line number without printing the line's text so secrets don't leak.

## File format and `${VAR}` expansion

```dotenv
# comments and blank lines are allowed
FOO=bar
# Double quotes interpret \n, \t, \" and \\.
QUOTED="hello world"
# Single quotes keep the value verbatim.
LITERAL='hello\nworld'
# The export prefix is optional.
export PORT=8080
# Double quotes expand ${VAR}.
PATH_TPL="${HOME}/bin"
# \$ blocks expansion at that position.
ESCAPED="\${not expanded}"
```

Rules:
- Keys match `[A-Za-z_][A-Za-z0-9_]*`. Multi-line values are not supported (use `\n` in double quotes).
- Inline `#` comments after **unquoted** values become part of the value — quote values when appending comments, or put comments on their own line.
- Only bracketed `${VAR}` inside double quotes expands (bare `$VAR` is left literal). Lookup checks `os.Environ`, earlier files in the precedence chain, then earlier keys in the same file, without re-expanding substituted values (no cycles or double-expansion):

```dotenv
PW='a${b}c'
# Expands to postgres://u:a${b}c@h/db.
DSN="postgres://u:${PW}@h/db"
```

## Reading env vars before `NewApp`

If `main.go` opens a database before calling `framework.NewApp`, load the exact same file list first using `framework.DefaultDotEnvPaths()` and check the error:

```go
if err := dotenv.LoadAndApply(framework.DefaultDotEnvPaths()...); err != nil {
	log.Fatal(err)
}
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
