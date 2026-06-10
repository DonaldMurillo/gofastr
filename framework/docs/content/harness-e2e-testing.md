# Harness end-to-end testing

The unit tests cover the wiring; this guide walks through verifying the
real-world integrations that mocked tests can't.

## 1. Build the harness

```bash
go install ./cmd/gofastr
```

`gofastr` is now on `$PATH`. Confirm:

```bash
gofastr harness --help 2>&1 | head -5
```

## 2. Real provider tests (ZAI + OpenRouter)

The `e2e_real` build tag gates Go tests that hit real providers. They
skip individually if the relevant env var is missing.

```bash
ZAI_API_KEY=zai_...                      \
OPENROUTER_API_KEY=sk-or-...             \
OPENROUTER_MODEL=anthropic/claude-3.5-haiku  \
  go test -tags=e2e_real -v -run E2EReal ./framework/harness \
          -count=1 -timeout 5m
```

What each test verifies:

| Test | Verifies |
|---|---|
| `TestE2EReal_ZAI_GLM51` | ZAI streaming format, usage parsing, full turn lifecycle against GLM-4.6 |
| `TestE2EReal_ZAI_Models` | Hardcoded catalog still lists GLM-5.1 first |
| `TestE2EReal_OpenRouter_Chat` | OpenRouter SSE streaming, `HTTP-Referer` + `X-Title` headers accepted, usage parsing |
| `TestE2EReal_OpenRouter_Catalog` | `/v1/models` parser handles real OpenRouter payload; pricing populated |
| `TestE2EReal_OpenRouter_CacheParseNoCrash` | Cache fields don't crash the stream parser on real provider output |

Cost: a typical run is a handful of small completions, single-digit cents
across both providers.

## 3. MCP server via Claude Code

The harness exposes itself as an MCP server over stdio so any
MCP-capable client can drive it. To wire it into Claude Code:

### Step 1 — set the credstore passphrase + provider key

```bash
export GOFASTR_HARNESS_PASSPHRASE="$(openssl rand -hex 16)"
```

Then put your provider key into the credstore by running the harness
once interactively (it'll auto-create the credstore file under
`~/.config/gofastr/harness/creds.enc`):

```bash
# A one-shot interactive run; the credstore is decrypted on first
# access. We'll add a `creds add` subcommand later; for v0.1, put the
# key in via env var that the harness reads on boot:
ZAI_API_KEY=zai_... GOFASTR_HARNESS_PASSPHRASE="$GOFASTR_HARNESS_PASSPHRASE" \
  gofastr harness --prompt "ping"
```

### Step 2 — add the MCP server entry to Claude Code's config

Edit `~/Library/Application Support/Claude/claude_desktop_config.json`
(macOS) or `~/.config/Claude/claude_desktop_config.json` (Linux):

```json
{
  "mcpServers": {
    "gofastr-harness": {
      "command": "gofastr",
      "args": [
        "harness",
        "mcp",
        "--profile",
        "/absolute/path/to/framework/harness/profile/default.toml"
      ],
      "env": {
        "GOFASTR_HARNESS_PASSPHRASE": "your-passphrase-from-step-1"
      }
    }
  }
}
```

Restart Claude Code. The harness should appear under the MCP server
list in the model picker.

### Step 3 — verify the integration from Claude Code

In a Claude Code conversation:

1. **`tools/list` shape** — ask Claude Code to list MCP tools. You should
   see `gofastr-harness` exposing:
   - `harness.create_session`
   - `harness.list_sessions`
   - `harness.run_agent_with_shell_access` — the honestly-named tool
   - `harness.cancel_turn`, `harness.answer_permission`, `harness.set_model`
   - `harness.enter_plan_mode`, `harness.exit_plan_mode`

2. **Resource listing** — ask for `resources/list`. URIs should appear
   under the `harness/v1://` scheme.

3. **Run-agent smoke test** — in Claude Code, ask:

   > "Use the `gofastr-harness` MCP server to call `run_agent_with_shell_access`
   > with prompt 'Reply PONG' on the default session. Wait for the turn."

   Expected: Claude Code invokes the tool, the inner harness completes
   one turn, and the result block contains "PONG" plus a `_meta` object
   with `cost`, `turns`, `toolCalls`.

4. **Identity-class enforcement** — when the inner agent attempts a
   tool that triggers a `PermissionRequested`, the outer Claude Code's
   `harness.answer_permission` call should be **rejected** if it's
   issued from the same agent that originated the turn. This is hard
   rule 11 (agents-can't-self-approve) at work.

## 4. WebSocket transport

```bash
# Start the harness with the WS listener.
gofastr harness --listen 127.0.0.1:8421 --auth-token-file ~/.config/gofastr/harness/ci-token.json &

# In another terminal, mint a token via the auth flow (see
# control/auth/issuance.go) and connect with `websocat` or a small
# WebSocket client:
websocat -H 'X-Harness-Token: <token>' ws://127.0.0.1:8421/v1/ws?session=sess_...
```

## 5. What this guide does NOT cover

- **Copilot OAuth device flow** — requires interactive GitHub auth in a
  browser. The adapter is built and unit-tested against stubs; real
  validation needs a separate session.
- **Multi-week chaos testing** — concurrency-heavy scenarios that
  unit tests can't stress.
- **Large-scale cost reconciliation** — the local ledger is in place;
  reconciling against OpenRouter's `/credits` endpoint at month-end is
  an operations exercise, not a build verification.

## Common mistakes

- **Dropping `-tags=e2e_real` from the command.** The real-provider
  tests are behind that build tag — without it they aren't compiled at
  all, so `go test -run E2EReal` finds nothing and reports success
  having tested nothing.
- **Reading a green CI run as provider coverage.** Each test skips
  itself when its key (`ZAI_API_KEY`, `OPENROUTER_API_KEY`) is unset.
  A run with no keys is all skips. Use `-v` and check for `SKIP` lines
  before trusting the result.
- **Omitting `-count=1`.** A cached pass from a previous run can mask
  a provider-side regression — the whole point of this suite is to hit
  the real endpoint now.
- **Assuming Copilot is covered.** The OAuth device flow needs
  interactive browser auth; the adapter is unit-tested against stubs
  only. Real Copilot validation is explicitly out of this guide's
  scope.
