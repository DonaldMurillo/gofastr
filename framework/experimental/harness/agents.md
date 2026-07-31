# framework/experimental/harness — the GoFastr agent harness

> **Experimental (v0.1).** This subsystem lives under
> `framework/experimental/` so the stability policy's `/experimental/`
> exemption applies: APIs here change without notice and without a
> deprecation window. Consumers must pin a version.

`gofastr harness` is a from-scratch agent harness shipped as a subcommand
of the `gofastr` CLI. It runs an agent loop against OpenRouter or ZAI GLM,
reads project context from **AGENTS.md**, and loads reusable behavior from
**SKILL.md** packages. Third-party Go deps are forbidden — stdlib and
`golang.org/x/*` only.

**Use this when** the prompt mentions: the harness, `gofastr harness`, the
agent loop, providers (OpenRouter / ZAI GLM), skills or SKILL.md, AGENTS.md
context loading, the control protocol (`inproc` / `rest` / `ws` /
`mcpserver`), or the bundled TUI / web clients.

**Import:** `github.com/DonaldMurillo/gofastr/framework/experimental/harness`
— subpackages under `.../harness/{engine,control,provider,tool,skill,context,
session,memory,hook,profile,...}`.

**Honest scope.** `docs/content/harness-architecture.md` is the design, not a
feature list. Built today: the engine loop, OpenRouter + ZAI providers, the
built-in tools + permission engine, the encrypted-file credstore, the SQLite
session log, the TUI + web clients, and the control transports. **Not built:**
the TOFU ack gate (content hashes are computed but never gated against an
approval store), MCP-server spawn inside `New` (the `mcpclient` package exists
but is not wired in), the Copilot provider, and provider failover. See the
doc's "Maturity — what is and isn't built" section.

**Layering:** the framework root must **not** import this package — enforced
by `layering_test.go` → `TestFrameworkRootDoesNotImportHarness`. Only explicit
consumers (the `gofastr harness` subcommand) import it.
