# GoFastr harness architecture

> Read this before adding, moving, or extracting any package under
> `framework/harness/`. The harness is intentionally narrow at the core
> and extensible at the edges — the rules below explain *why* each layer
> exists, what is locked, and how to add new behavior without growing
> the loop.

---

## The shape in one paragraph

`gofastr harness` is a from-scratch agent harness, shipped as a
subcommand of the existing `gofastr` CLI. It runs an agent loop
against OpenRouter or ZAI GLM (Copilot lands in v0.2 after the rest
of the loop is hardened), reads project context from **AGENTS.md**
(with vendor-specific files as additive fallbacks), and loads reusable
behavior from **SKILL.md** packages. The engine is a **headless
service** with a transport-agnostic wire protocol: bundled clients are
a pure-stdlib TUI and a local web UI built on GoFastr itself, and the
same protocol is exposed over four peer transports (`inproc`, `rest`,
`ws`, `mcp`-server) so external TUIs, IDE plugins, scripts, other
language clients, remote machines, or other agents (Claude Code,
Codex, Cursor) can drive the same engine. The harness has two preset
profiles selected at boot — `--framework` (work *on* GoFastr) and
default (work *with* GoFastr) — each a bundle of skills, MCP servers,
tools, permissions, and a system-prompt header. Third-party Go
dependencies are forbidden; only stdlib and `golang.org/x/*` are
allowed. The agent loop is small on purpose; everything that is not
orchestration enters through one of five extension seams. Multi-client
attach, originator tracking, total input ordering, and permission
arbitration all live in `control/multiplex` above the engine; the
engine itself knows about *clients*, never about transports.

---

## Non-goals

- **No third-party Go dependencies.** `golang.org/x/*` (Go-team
  subrepos) is acceptable; everything outside stdlib + x/* is not.
  This includes the TUI (rolled by hand) and MCP (both client and
  server, from scratch). See § Build order — the cost of these
  decisions is acknowledged and accepted.
- **No bundled language-model SDK.** Raw HTTP per provider.
- **No vendor-specific instruction format as primary.** AGENTS.md
  and SKILL.md are the canonical surfaces; CLAUDE.md, .cursorrules,
  GEMINI.md, .windsurfrules, .github/copilot-instructions.md are
  read if present and appended, never the source of truth.
- **No "MVP-shaped" cut corners that would block a later
  capability** — but **interfaces ≠ implementations**. Every
  interface listed in this doc is designed up front; packages are
  placeholder-empty until their build-order phase. See § Build order.
- **No replace-directives or pinned framework SHAs** for the
  dogfooded web client. The harness lives in the GoFastr monorepo
  and ships at the same SHA as the framework. Framework changes
  that break the web client are fixed in the same PR; the harness
  is a first-class consumer of `framework/`, not a downstream pin.

---

## Hard rules

1. **The agent loop contains orchestration only.** No provider-specific
   code, no surface-specific code, no profile-specific code, no skill
   business logic. If a behavior can be expressed as middleware, an
   event subscriber, or a plugin registration, it lives outside the
   loop. No exceptions.
2. **No third-party imports.** Stdlib and `golang.org/x/*` only. The
   single import boundary is enforced by `go.mod` review and by the
   `harness/internal/depscheck` build tag.
3. **AGENTS.md is the primary project-instruction surface.** Vendor
   files are additive fallbacks read by separate `ContextSource`
   implementations.
4. **SKILL.md is the primary skill format.** Three-tier progressive
   disclosure (name+desc at startup, body on activation, supporting
   files on explicit reference) is honored end-to-end.
5. **MCP servers must support two discovery modes.** `eager` (load
   every tool schema at startup) and `lazy` (load only name+desc,
   fetch schemas on first invocation). The default for high-tool-count
   servers is lazy.
6. **Profiles are config, not code.** `--framework` and default are
   two preset bundles loaded from `profile/framework.toml` and
   `profile/default.toml`. Adding a third profile must never require
   touching the loop or core packages. The two built-in presets are
   also **embedded into the `gofastr` binary** (`go:embed`): when an
   installed binary runs outside the source tree and the on-disk
   `profile/*.toml` is absent, the loader falls back to the embedded
   copy, so `gofastr harness` / `gofastr harness --framework` work
   anywhere. An explicit `--profile <path>` still requires an on-disk
   file (no embedded fallback).
7. **The engine knows about *clients*, never transports.** Transports
   are owned by `control/`. The engine sees an opaque `Client` and
   tracks per-client identity (originator, `identity_class`) without
   ever inspecting how that client is wired. Bridging transports
   to clients happens at `control/multiplex`.
8. **All tool calls pass through tool middleware.** Permissions,
   sandboxing, timeouts, redaction, memoization all enter as
   middleware. The dispatcher itself never embeds policy.
9. **`SendInput` is totally ordered at the multiplexer.** All
   commands across all transports merge into one queue at
   `control/multiplex`; the engine processes one turn at a time per
   `EngineRun`. Mid-turn `SendInput` is rejected with
   `Error{Reason: TurnInProgress}`; the client must `CancelTurn`
   first. No client owns state; everything is replayable from the
   session log.
10. **MCP is a peer-class transport, in both directions.** The
    harness consumes external MCP servers as `ToolSource`s *and*
    exposes its own engine as an MCP server so any MCP-capable
    client (Claude Code, Codex, Cursor, custom agents) can drive a
    harness session. Same control protocol, different wire.
11. **Every client has an `identity_class` set at attach time:
    `human` or `agent`.** Permission prompts (and any future
    consent-shaped event) honor the distinction. An `agent` client
    cannot self-approve a permission for a turn it originated —
    `AnswerPermission` from the same `Client.ID()` that originated
    the turn is rejected. By default, at least one `human` ack is
    required; `--auto-approve` bypasses, honestly named.
12. **Every byte from outside the trust boundary is untrusted
    content.** AGENTS.md, SKILL.md, MCP-server-supplied tool
    descriptions and resource bodies, fetched web pages, tool
    results — all are wrapped in clearly-delimited prompt sections
    (`<untrusted-...>...</untrusted-...>`) with a standing
    instruction to never follow instructions inside those tags.
13. **Trust-on-first-use for code-adjacent files loaded into prompts
    or executed by hooks** — with bulk-ack scoped to user-owned
    directories. SHA-256 every `SKILL.md`, `AGENTS.md`, fallback
    context file, and project hook on first load. New / changed →
    interactive ack with diff. Approvals persist in
    `~/.config/gofastr/harness/approved.lock`.
    - **Bulk-ack policy.** Files from user-owned directories
      (`~/.config/gofastr/harness/`, `framework/harness/skills/` —
      built-ins shipped in the binary) may be bulk-approved with
      one decision per *directory* (recorded in
      `approved.lock` as a `dir-trust` entry covering all files
      whose hashes were captured at install time).
    - **Project-local files are never bulk-acked.** Files from
      `<repo>/.gofastr/harness/` are individually reviewed every
      time the hash changes. This is the supply-chain trust
      boundary.
    - **Diff-class detection.** When a previously-approved file
      changes, the harness classifies the diff (metadata-only,
      prose, code-adjacent, executable-changes). Only
      code-adjacent and executable-changes block; prose-only
      changes auto-promote with a non-blocking notice. Defense in
      depth comes from rule 12 (untrusted-content tags at
      runtime).
    - **Hooks** defined in project-local config
      (`<repo>/.gofastr/harness/`) are additionally gated behind
      `--allow-project-hooks` and individually ack'd. No
      bulk-ack for project-local hooks ever.
14. **`Command` and `Event` are closed sealed unions.** Plugins
    extend via tools, events-via-subscription, and slash commands
    in their own namespaces — *not* new wire-protocol verbs. The
    set of verbs is fixed in `control/protocol.go`; any extension
    that needs to ride the wire goes through the `CustomCommand` /
    `CustomEvent` open verbs whose payload is plugin-defined JSON.
    See § Protocol versioning & evolution.
15. **Every wire identifier has a documented format.** `SessionID`,
    `LogID`, `CallID`, `jti`, and `OriginatorID` are all
    ULID-prefixed strings (`sess_…`, `log_…`, `call_…`, `tok_…`,
    `cli_…`). See § Glossary → ID formats. Branch ID-rewrite is
    deterministic from `(source_id, new_log_id)`.

---

## Threat model

The harness runs an LLM with `Bash`, `Read`, `Write`, and `WebFetch`
tools. **Any path that lets an actor send `SendInput` is therefore an
arbitrary-code-execution path** unless that path is also gated by
permissions. The threat model below names the principals, the assets,
and the policy each transport enforces. Every subsequent section is
audited against this matrix.

### Principals

- **Human user at the keyboard.** Identity class `human`.
- **Bundled in-process client** (TUI, web). Identity class `human`
  (proxies the user).
- **External client over `rest`/`ws` from the same user account.**
  Identity class `human` by default; may be downgraded.
- **External agent over `mcp` (stdio or HTTP).** Identity class
  `agent`. Confused-deputy hazard — see below.
- **Project-local code:** AGENTS.md, SKILL.md under `<repo>/`,
  hooks under `<repo>/.gofastr/harness/`, MCP servers spawned by
  the active profile. **Treated as untrusted unless approved
  (TOFU).**
- **Web content** fetched by the agent (`WebFetch`). Always
  untrusted.
- **MCP-server-supplied tool descriptions and resource bodies.**
  Untrusted; injection hazard.

### Assets

- Provider tokens (Copilot, ZAI, OpenRouter) in the keychain or
  encrypted-file store, plus *in-memory* during a session.
- The session log at `~/.local/share/gofastr/harness/sessions.db`
  — every `TextDelta`, `ToolResult`, and tool argv.
- Repo contents the agent can `Read` (incl. `.env`, secrets).
- The user's home directory (`Bash` reach).
- Capability tokens for attach.
- The harness binary and its config (`~/.config/gofastr/harness/`).

### Defenses by transport

| Transport | Auth | Identity class | Extra policy |
|---|---|---|---|
| `inproc` | trust boundary = process | `human` | None |
| `rest` over Unix socket | filesystem perms `0600` + capability token | `human` (override allowed) | Host/Origin checks; `X-Harness-Token` header |
| `rest` / `ws` over TCP loopback | capability token + Host/Origin checks | `human` | Custom header (anti-DNS-rebinding) |
| `rest` / `ws` over LAN | capability token + TLS | `human` | Token must be bound to specific session IDs |
| `mcp` stdio | capability token via **env var** + parent-process attestation | `agent` | Tool exposed as `harness.run_agent_with_shell_access` (honest name) |
| `mcp` streamable HTTP | capability token in `Authorization` header | `agent` | Same as TCP transport rules |

### Token issuance

- `GET /v1/auth/token` on the Unix socket emits a 6-digit code to
  the harness's TTY (or via system notification); the requester
  POSTs the code back within 60 s to receive the token.
- Tokens carry an explicit claim set:
  `{sessions: [...], commands: [...], identity_class: ..., exp: ts,
  can_mint: false}`. Default expiry 24 h.
- Revocation list checked on every request.
- Tokens stored in the OS keychain when shared with bundled
  clients; never in plaintext config.

### Standing rules baked into permissions

- Default Bash blocklist: `security`, `secret-tool`, `keyctl`,
  `kwalletcli` (credential exfiltration paths).
- `WebFetch` strips `Authorization` and `X-Harness-Token` headers
  from any URL it is asked to fetch.
- `WebFetch` runs an **SSRF preflight**: it rejects URLs resolving to
  private/loopback/link-local ranges (incl. cloud metadata
  `169.254.169.254`) and re-validates the target on every redirect hop,
  failing closed. The test-only `WebFetch.AllowPrivateHosts` field
  disables the preflight so unit tests can reach `httptest` loopback
  servers — never set it in production.
- Untrusted content (rule 12) is the prompt-injection defense.
- TOFU (rule 13) is the supply-chain defense.

### What this model does *not* defend against

- A user knowingly approving a malicious skill or hook (the TOFU
  diff is presented; the user takes the consequences).
- A compromised harness binary (out of scope; would be detected by
  OS-level binary signing).
- Bash escapes from `sandbox-exec` (sandbox is best-effort).
- The agent being prompt-injected into doing something harmful
  *within* its allowlist (no protection — the allowlist is the
  policy boundary).

---

## Build order

The non-goal "no MVP-shaped cut corners" stands — every interface
listed in this doc exists from day one. But interface ≠ implementation;
packages can ship as **placeholder-empty** until their phase, and the
implementation order below is the contract for what must actually
work when.

### v0.1 — demonstrably correct slice

The smallest end-to-end path that proves the architecture: one
provider, one transport, three tools, one client, full safety
posture.

- Engine: full loop, request + tool middleware, event bus,
  multiplex, originator + identity-class tracking, total ordering.
- Providers: **OpenRouter** + **ZAI GLM**. (No Copilot.)
- Transports: `inproc` + `rest` (Unix socket + loopback, with
  Host/Origin checks). No `ws`, no `mcp`-server.
- Tools: `Read`, `Write`, `Bash` + permission engine + sandboxing
  middleware + redaction middleware.
- Clients: **TUI** (pure-stdlib + `golang.org/x/term`, accept the
  scope cost), web client built on GoFastr.
- MCP client (consumer): enabled, eager + lazy discovery.
- Skills: SKILL.md loader, 3-tier disclosure, slash-command
  registration via `Plugin.Register`.
- AGENTS.md primary reader + CLAUDE.md fallback (transitional).
- Hooks: user-config only. `--allow-project-hooks` defined but
  default off.
- Persistence: SQLite session log, keychain-encrypted; redaction
  middleware on `ToolResult`; 30-day TTL on full-content events.
- Memory: file-based typed memory.
- Threat model: complete; all defenses listed above active.
- Profiles: `framework` and `default` shipped. `framework` defaults
  to `openrouter:claude-sonnet-4` (not Copilot).

### v0.2

- Copilot provider (added; not default for `--framework` until 30
  days stable).
- `ws` transport.
- `mcp`-server transport, **stdio only**, with the rename:
  `harness.run_agent_with_shell_access` (replaces `send_input`).
- Plan mode (`EnterPlanMode` / `ExitPlanMode`).
- Hook system extended to project scope behind `--allow-project-hooks`.
- Cost dashboard (web).

### v0.3

- `mcp`-server transport, **streamable HTTP** with resource
  subscriptions, prompts, capability negotiation, session
  resumption.
- Background tasks, scheduler, cron.
- `RoutingProvider` (multi-model per turn — added as a `Provider`
  composition, not middleware).
- `delegate` tool (sync-only, blocks parent; see § Future
  extensions for the explicit scope).
- Conformance suite (`control/conformance/`) — cross-transport
  parity tests.

### v0.4+

- Worktree-isolated subagent runtime (reusing `framework/`'s
  worktree module).
- Distributed sessions over `ws` with capability tokens.
- Time-travel / step-through replay UI.
- Container-sandboxed Bash via tool middleware.

---

## Glossary

| Term | Meaning |
|---|---|
| **`EngineRun`** | A live agent-loop instance bound to a profile, model, and session log. The thing that processes turns. |
| **`AttachContext`** | A connection between one `Client` and one `EngineRun`. Carries originator identity. Many `AttachContext`s per `EngineRun` are normal. |
| **`LogID`** | Append-only event log identity in the SQLite store. A `LogID` can be opened into a new `EngineRun` (resume) or branched. |
| **`Session`** | Informal umbrella term. In wire protocol and CLI, `SessionID` resolves to `EngineRun` + the active `LogID` it writes to. Use precise names when writing code; only the doc uses "session" loosely. |
| **`Client`** | An entity attached to an `EngineRun` via some transport. Has an `ID()`, an `identity_class` (`human` or `agent`), and a subscription to the event stream. |
| **`Originator`** | The `Client.ID()` whose `SendInput` started the current turn. Used by permission middleware. |
| **`Transport`** | The wire that carries `Command`s in and `Event`s out (`inproc`, `rest`, `ws`, `mcp`). |
| **`untrusted-...` block** | A clearly-delimited section of the system prompt holding content from outside the trust boundary. Standing instruction prevents the model from following instructions inside. |
| **TOFU** | Trust-on-first-use. The harness hashes a file on first load and asks the user to ack. Subsequent runs verify the hash. |

### ID formats (normative)

All identifiers are ULID-derived (Crockford base32, 26 chars,
sortable, URL-safe). Each has a typed prefix so a string can be
inspected without context:

| ID | Pattern | Notes |
|---|---|---|
| `SessionID` | `^sess_[0-9A-HJKMNP-TV-Z]{26}$` | One per `EngineRun`. |
| `LogID` | `^log_[0-9A-HJKMNP-TV-Z]{26}$` | Persistence-layer ID; an `EngineRun` writes to exactly one `LogID`. |
| `CallID` | `^call_[0-9A-HJKMNP-TV-Z]{26}$` | One per tool call. |
| `jti` (token ID) | `^tok_[0-9A-HJKMNP-TV-Z]{26}$` | Revocation key. |
| `OriginatorID` (= `Client.ID()`) | `^cli_[0-9A-HJKMNP-TV-Z]{26}$` | Stable for the lifetime of the client's attach. |
| Event sequence ID | `uint64`, monotonic per `SessionID` | Used by every stream-resume mechanism (SSE `id`, WS `lastEventId`, MCP resource version). |

**Branch ID-rewrite algorithm.** When events are copied between
`LogID`s at a `TurnEnded` boundary, every embedded ID is rewritten
as `new_id = ulid_from_seed(sha256(old_id || new_log_id))`.
Deterministic: two clients branching the same source at the same
boundary produce the same new IDs (good for cache + cross-tool
agreement).

---

## Package map

```
cmd/gofastr/
└── harness.go              Subcommand entry; flag parsing; profile load; engine bootstrap

framework/harness/
├── engine/                 The agent loop. Orchestration only.
│   ├── loop.go             Turn loop — request, stream, tool execution, terminate/continue
│   ├── stream.go           Provider-shape-agnostic stream parser (text, tool, thinking, usage)
│   ├── request.go          Request middleware chain
│   ├── tool_dispatch.go    Tool middleware chain + dispatcher
│   ├── cancel.go           Cancellation tree (turn → tool calls → child engines)
│   └── events.go           Typed event bus + subscriber registry
├── provider/               Provider adapters. One subdir per implementation.
│   ├── provider.go         Provider interface (Chat, Models, TokenCount)
│   ├── routing/            RoutingProvider (Provider composition — multi-model per turn)
│   ├── credstore/          Encrypted-file primary; OS-keychain integrations opt-in
│   │   ├── encfile.go      AES-GCM with passphrase or machine-bound key (primary)
│   │   ├── keychain_darwin.go  (opt-in plugin — macOS Security framework)
│   │   ├── keychain_linux.go   (opt-in plugin — libsecret via D-Bus)
│   │   └── keychain_windows.go (opt-in plugin — CredRead/Write via x/sys/windows)
│   ├── helper/             Credential-helper subprocess — holds tokens, signs requests
│   ├── copilot/            v0.2 placeholder — OAuth device-code flow, /chat/completions
│   ├── zai/                OpenAI-compatible client (api.z.ai)
│   └── openrouter/         OpenAI-compatible client, model catalog, pricing, BYO-key passthrough
├── tool/                   Built-in tools + permission engine + tool packs.
│   ├── tool.go             Tool interface: Run(ctx, ToolCall, EventSink) → (ToolResult, error)
│   ├── registry.go         ToolSource interface + dynamic registration
│   ├── permission/         Allow/ask/deny rules, glob matching, persistable allowlists
│   ├── pack/               Tool bundles (fs, git, web, gofastr, ...)
│   └── builtins/           Read, Write, Edit, Bash, Grep, Glob, Ls, WebFetch, ...
├── mcpclient/              MCP client (eager + lazy discovery modes).
│   ├── client.go           MCP wire protocol client (stdio + http+sse + streamable HTTP)
│   ├── discovery.go        Tool-list + on-demand schema fetch
│   ├── pin.go              sha256 pinning of MCP server binaries declared in profiles
│   └── source.go           ToolSource adapter — MCP tools appear identical to built-ins
├── skill/                  SKILL.md loader + progressive disclosure.
│   ├── skillmd/            SKILL.md parser (frontmatter, body, supporting files)
│   ├── tier.go             3-tier disclosure machinery
│   ├── tofu.go             TOFU hash + ack flow
│   └── registry.go         Skill registry, activation triggers, /skills:name invocation
├── context/                Project-instruction sources (config list, not polymorphism).
│   ├── reader.go           Walks a configured list of (path, label) tuples; concatenates
│   ├── agentsmd.go         AGENTS.md primary reader (nested file support, walk-upward)
│   └── fallback.go         CLAUDE.md, .cursorrules, GEMINI.md, .windsurfrules,
│                           .github/copilot-instructions.md — appended if profile enables
├── session/                Persistence + replay.
│   ├── store.go            SessionStore interface
│   ├── sqlite/             SQLite append-only event log (keychain-encrypted)
│   ├── retention.go        TTL on full-content events; metadata-only after expiry
│   ├── redact.go           Secret-regex redaction middleware (AWS/GitHub/Bearer/-----BEGIN)
│   ├── replay.go           Step-through walker (view only; does not re-execute)
│   └── branch.go           Branch-at-TurnEnded with tool_use ID rewriting
├── memory/                 Typed auto-memory (file-based).
│   └── file.go             Markdown-files-with-frontmatter implementation
├── hook/                   Shell-level lifecycle hooks.
│   ├── hook.go             Hook spec (event, command, blocking)
│   ├── tofu.go             SHA-256 ack for any new hook (per rule 13)
│   └── runner.go           PreToolUse / PostToolUse / UserPromptSubmit / Stop / Compact / SessionStart
├── profile/                Profile loader + presets.
│   ├── profile.go          Profile spec — skills, mcp servers (sha256-pinned), tools, permissions, model, prompt header
│   ├── framework.toml      Preset: working on GoFastr
│   └── default.toml        Preset: working with GoFastr
├── control/                Engine-as-a-service: transport-agnostic protocol.
│   ├── protocol.go         Wire types (Command, Event) and codec (JSON)
│   ├── client.go           Client interface — Subscribe(events), Send(command), ID(), IdentityClass()
│   ├── multiplex/          Multi-client routing — originator tracking, total input ordering,
│   │                       permission-arbitration policy, broadcast events to all attached.
│   │                       This is where "engine knows about clients, not transports" lives.
│   ├── resources/          Aggregation layer (sessions, profiles, providers, tools, skills);
│   │                       depended on by mcpserver/ and rest/ for catalog responses.
│   ├── conformance/        Cross-transport parity test matrix (every transport runs the same scenarios).
│   ├── inproc/             Go-channel transport for bundled clients
│   ├── rest/               HTTP/REST + SSE; Host/Origin checks; X-Harness-Token header
│   ├── ws/                 WebSocket transport (full duplex)            — v0.2
│   ├── mcpserver/          MCP-server transport (engine as MCP)         — stdio in v0.2, HTTP in v0.3
│   └── auth/               Token issuance (TTY confirmation), claim sets, revocation list
├── client/                 Bundled clients (each speaks the control protocol).
│   ├── tui/                Pure-stdlib + x/term TUI
│   │   ├── terminal.go     Raw mode (termios via x/term), resize handling
│   │   ├── render.go       ANSI rendering, scrollback, syntax highlighting
│   │   ├── input.go        Key parsing (escape sequences, mouse, paste)
│   │   ├── modal.go        Permission prompts, diff preview, file picker
│   │   └── statusline.go   Cost meter, model indicator, profile name
│   └── web/                gofastr-powered local web UI (sidecar, co-equal)
│       ├── server.go       Embeds framework.App; random local port
│       ├── entities.go     Session, Turn, ToolCall, Event entities
│       ├── pages/          Server-rendered pages — session timeline, MCP inspector, cost dashboard
│       └── stream.go       SSE bridge to control-plane events
├── plugin/                 Plugin interface.
│   └── plugin.go           type Plugin interface { Register(h *Harness) error }
└── harness.go              Harness struct — composes all the above
```

---

## The agent loop

The loop is small but it does have a small amount of policy — calling
it "150 lines of pure orchestration" was overstated. The honest list:

1. **Accept input** from the multiplexer (`SendInput`, `ToolResult`
   from a prior tool dispatch, or system events). Originator and
   identity class travel with the input.
2. **Assemble an empty `Request`** and pass it through the request
   middleware chain. Middleware injects: system prompt header,
   AGENTS.md content, skill metadata + activated skill bodies,
   selected memory entries, history (possibly compacted), tool
   schemas, cache hints. The loop never assembles content directly
   — that's how middleware stays the place to extend.
3. **Send to the provider** and parse the stream into typed events.
   Emit each event onto the bus.
4. **Dispatch tool calls** through the tool middleware chain.
   Results feed back as input.
5. **Decide whether to loop or yield.** This is the one piece of
   policy in the loop. Yield conditions, all explicit:
   - The stream ended with no tool calls (model produced a final
     answer).
   - `CancelTurn` arrived.
   - A middleware returned a terminal error.
   - A `Yield` content block appeared in the response (explicit
     end-turn signal from a provider that supports it).

Everything else — permission prompts, cost accounting, compaction,
skill injection, AGENTS.md injection, memory injection, hook firing,
diff preview, MCP tool resolution — is middleware or an event
subscriber.

```
┌─────────────────────────────────────────────────────────────┐
│  clients ── transports ──▶  control/multiplex               │
│                                  │                          │
│                          inputCh (totally ordered)          │
│                                  │                          │
│                              engine.Loop                    │
│                                  │                          │
│                       request middleware chain              │
│                                  │                          │
│                          Provider.Chat                      │
│                                  │                          │
│                            stream parse                     │
│                                  │                          │
│                       events ─▶ event bus ─▶ subscribers    │
│                                  │            ▲             │
│                       tool calls? yes         │             │
│                                  │            │             │
│                    tool middleware chain      │             │
│                              ↳ EventSink ─────┘             │
│                                  │                          │
│                       tool result ─▶ inputCh                │
│                                  │                          │
│                    end-turn?  loop : yield                  │
└─────────────────────────────────────────────────────────────┘
```

---

## Extensibility — the five seams

Every extension point is one of these five. New ideas land in a seam,
not in the core.

### 1. Request middleware

```go
type RequestHandler func(*Request) (*Response, error)
type RequestMiddleware func(*Request, RequestHandler) (*Response, error)
```

Composed in order around the provider call. Used for:

- AGENTS.md injection (read once per session, cached)
- Skill injection (activate triggers, inject SKILL.md tier-1 names +
  active tier-2 bodies)
- Memory injection (relevant entries selected by tag + heuristic)
- Cache-breakpoint placement (provider-aware)
- History compaction trigger
- Provider routing / fallback / retry
- Cost budget enforcement (abort if cap exceeded)
- A/B prompt experiments
- Logging, telemetry, journaling

### 2. Tool middleware

```go
type EventSink interface {
    Emit(Event)   // ToolCallProgress, partial results, side-channel notices
}

type Tool interface {
    Name() string
    Schema() *jsonschema.Schema
    Run(ctx context.Context, call *ToolCall, sink EventSink) (*ToolResult, error)
}

type ToolHandler    func(ctx context.Context, call *ToolCall, sink EventSink) (*ToolResult, error)
type ToolMiddleware func(ctx context.Context, call *ToolCall, sink EventSink, next ToolHandler) (*ToolResult, error)
```

The shape is locked upfront so streaming tools, cancellation, and
middleware all share one signature. Middleware can observe partial
results (redaction sees streamed chunks; timeout has a `ctx`;
permission can interpose before `next` runs). Used for:

- Permission gate (allow/ask/deny rules, identity-class-aware)
- Sandbox wrap (subshell, `sandbox-exec` on macOS, container later)
- Timeout / cancellation (via `ctx`, not error returns)
- Result truncation / pagination
- Memoization for read-only tools
- Redaction (strip secrets from outputs **and** from streamed
  `ToolCallProgress`)
- PreToolUse / PostToolUse shell hook invocation

#### Permission UX (session-scoped allow rules)

Default `ask` policy without affordances produces ~18 prompts per
non-trivial turn. The permission middleware presents the user with
**four choices** per `PermissionRequested` event, not two:

1. **Allow once** — answer this call only.
2. **Allow this argv-glob for the session** — remember
   `Tool:argv-glob` (e.g. `Bash:grep *`, `Bash:find . -name *`)
   for the duration of the `EngineRun`.
3. **Allow this tool for the session** — remember `Tool:*`
   broadly.
4. **Deny.**

A visible "Session policy" panel (TUI sidebar, web sidebar) shows
the active session-scoped allows; one keystroke revokes any of
them.

Pre-shipped **quiet-mode preset** for known-safe read-only Bash
patterns (default ON, configurable per-permission-preset):

```
Bash: git status, git log, git diff, git branch
Bash: ls *, pwd, cat <repo-file>, head, tail, wc
Bash: grep *, rg *, find . *
Read, Glob, Ls (anywhere under the repo)
```

Never prompts for these. `--strict-permissions` disables the
preset for users who want every call gated.

Session-scoped allows persist for the `EngineRun` only — they
never carry across sessions or persist to disk. The user can
promote a session-scoped allow to a profile-level rule via
`/permissions:promote <rule>`.

### 3. Event bus

Typed pub/sub. Every interesting state change emits an event:

| Event | Fired when |
|---|---|
| `TextDelta` | provider emits text chunk |
| `ThinkingDelta` | provider emits reasoning chunk (where supported) |
| `ToolCallStarted` | tool dispatch begins |
| `ToolCallProgress` | streaming tool with partial result |
| `ToolResult` | tool finishes (success or error) |
| `TurnStarted` / `TurnEnded` | loop iteration boundary |
| `TurnTiming` | emitted at `TurnEnded` with per-component duration map (request middleware, provider TTFB, provider total, each tool wall-time, SQLite write fsync). Web client uses this for the timeline view; operators answer "where was the time?" from a single event. |
| `CompactionTriggered` | history compaction begins |
| `CostIncremented` | new usage tallied |
| `PermissionRequested` | tool middleware needs ask-mode answer |
| `Cancelled` | user or budget aborted the turn |
| `Error` | provider, tool, or middleware error |
| `StreamGap` | stream resume crossed a TTL boundary; events between `from` and `to` are unavailable. |
| `TokenExpiring` | 5 min before token `exp`; clients refresh. |
| `HookTimeout` / `HookError` | a configured hook missed its deadline or exited non-zero. |
| `MCPServerDown` | an MCP server gave up after exhausting restart budget; its tools entered degraded state. |
| `SessionEnded` | engine shutdown reason (`idle`, `user`, `error`, `binary-shutdown`). |

Surfaces, hooks, plugins, and the cost dashboard all subscribe.

### 4. Pluggable backends

Single interface per concern. Backends are wired by the profile or by
`Plugin.Register`.

Five interfaces with concrete polymorphism need; the rest are
concrete types or config-driven choices to avoid speculative
abstraction.

| Interface | Purpose | Initial impls | Why an interface |
|---|---|---|---|
| `Provider` | LLM transport + chat | zai, openrouter, copilot (v0.2), routing (v0.3) | Each provider's wire format and auth differ deeply |
| `Transport` | Control-plane wire transport | inproc, rest, ws, mcpserver | Wire semantics differ (backpressure, framing, reconnect) |
| `Client` | Anything that drives the engine | tui, web, external (rest/ws/mcp) | Identity class + subscription patterns vary |
| `ToolSource` | Tools the registry exposes | builtins, mcpclient, user-plugin | Lifecycle differs (in-proc vs external process) |
| `SessionStore` | Session log + replay | sqlite | One impl today; second impl (postgres) would force redesign anyway — kept honest as a swap point |

**Deliberately not interfaces** (concrete types or config lists):

- **Skills** — one loader (`skill/skillmd`). No second loader designed.
- **Project context** — a config list of `(path, label)` tuples
  processed by `context/reader.go`. AGENTS.md and fallback files
  are six entries in that list, not six implementations.
- **Memory** — one file-backed implementation. A server-backed
  store would force a different shape.
- **Credentials** — encrypted-file is the primary type;
  keychain helpers are opt-in build-tagged plugins, not pluggable
  via an interface seam.
- **Permission policy** — one engine with allow/ask/deny rules
  parameterized by config. Not two implementations.

Switching one of the five real interfaces happens via
`profile.With(...)` or `Plugin.Register` — never a fork of the
engine.

### 5. Plugin contract

```go
type Plugin interface {
    Register(h *Harness) error
}
```

A plugin can: register request/tool middleware, subscribe to events,
register a `ToolSource`, register a `Transport`, register a `Provider`,
register slash commands (in a namespace it owns — see § Slash
commands), add permission rules, contribute hooks.

Profiles are lists of plugins. New profile = new TOML file referencing
existing plugins. New behavior = new plugin shipped as code.

---

## Providers

Two providers ship in v0.1; Copilot lands in v0.2. The abstraction is
`Provider`:

```go
type Provider interface {
    Name() string
    Chat(ctx context.Context, req *Request) (<-chan Event, error)
    Models(ctx context.Context) ([]Model, error)
    TokenCount(ctx context.Context, msgs []Message) (int, error)
}
```

### v0.1 providers

**ZAI GLM.** OpenAI-compatible. API key in credstore. Models:
`glm-4.6`, `glm-4.5-air`, `glm-z1`. Endpoint: `api.z.ai/api/paas/v4`.

**OpenRouter.** OpenAI-compatible. API key in credstore. Model
catalog from `openrouter.ai/api/v1/models` (cached locally with TTL;
pricing metadata feeds the cost dashboard). Endpoint:
`openrouter.ai/api/v1/chat/completions`. Required headers
`HTTP-Referer` and `X-Title` for analytics; some upstream models
require them.

### v0.2 — GitHub Copilot (deferred for reason)

Copilot's chat API is **reverse-engineered**, not a documented public
API. Token-exchange shape and `Copilot-Integration-Id` whitelist have
changed multiple times in 2025. Models available depend on
subscription tier and per-org policy. Streaming response shape differs
subtly from official OpenAI in tool-call delta accumulation and
`finish_reason` semantics. Auth flow:

1. `POST github.com/login/device/code` with the Copilot client ID
2. User opens displayed URL, enters displayed code
3. Poll `github.com/login/oauth/access_token` for the GH token
4. Exchange via `api.github.com/copilot_internal/v2/token` → short-lived
   Copilot token (must be refreshed; respect `endpoints.api` in the
   response — GitHub has moved this for some users)
5. Use Copilot token against `api.githubcopilot.com/chat/completions`
   with `Editor-Version`, `Copilot-Integration-Id` headers
6. Model catalog from `api.githubcopilot.com/models` — but per-call
   availability differs from the catalog

When v0.2 ships, the `--framework` profile does **not** default to
Copilot until 30 days of stable operation. The default-failover
path (Copilot 401 → OpenRouter with same model name) is pre-wired.

### Internal canonical message shape

The engine works in an Anthropic-shape canonical form (it's the most
expressive: tool_use/tool_result are first-class, content blocks are
typed). Each provider adapter translates outbound and inbound. **Two
honest limits on this canonicalization:**

- **Thinking / reasoning blocks are provider-bound.** Anthropic
  returns signed thinking blocks that must be echoed back verbatim
  to the same provider; OpenAI o-series returns reasoning summaries
  with different semantics; ZAI GLM has nothing equivalent. The
  canonical form encodes these as **opaque, provider-stamped**
  blocks. `SetModel` across families discards thinking; same family
  preserves it.
- **Cache hints are per-provider.** Anthropic uses `cache_control`
  on content blocks; OpenAI uses `prompt_cache_key` (Responses
  API); ZAI has no documented caching; OpenRouter passes through
  whatever the upstream takes. The "cache-breakpoint placement"
  middleware is therefore provider-aware, not generic.

These limits are not implementation laziness — they reflect that the
providers genuinely differ at the semantic layer and pretending
otherwise produces silent bugs.

---

## MCP discovery — two modes

Both modes are spec-compliant; the difference is when the harness
calls `tools/list` and `tools/get_schema`.

### Eager (default for low-count servers, ≤20 tools)

At server connect:

1. `tools/list` → all names + descriptions
2. `tools/get_schema` per tool → all schemas
3. Register every tool with the registry; schemas available in system
   prompt immediately

### Lazy (default for high-count servers, >20 tools)

At server connect:

1. `tools/list` → all names + descriptions only
2. Register name + description placeholders in the registry
3. On first invocation of a placeholder tool: `tools/get_schema` →
   hydrate schema → re-dispatch

The threshold is configurable per server in the profile. The lazy mode
maps 1:1 to SKILL.md tier-1 → tier-2 progressive disclosure: tool
names + descriptions are tier-1 metadata, schemas are tier-2 body.

---

## Skills (SKILL.md) and context (AGENTS.md)

### AGENTS.md

Read by `context/agentsmd`. Spec-compliant:

- Looks for `AGENTS.md` at the repo root
- Nested `AGENTS.md` in subdirectories is supported; the harness walks
  upward from the working directory and concatenates in path order
- Plain markdown, no schema
- Content injected into the system prompt via a request middleware

Vendor fallback readers in `context/fallback/`:

- `claude_md.go` reads `CLAUDE.md`
- `cursorrules.go` reads `.cursorrules` and `.cursor/rules/*.mdc`
- `gemini_md.go` reads `GEMINI.md`
- `windsurfrules.go` reads `.windsurfrules`
- `copilot_md.go` reads `.github/copilot-instructions.md`

Each is a separate `ContextSource`. The profile lists which to enable.
Default profile enables AGENTS.md only; `--framework` profile enables
AGENTS.md + CLAUDE.md (because this repo currently has both during
the transition).

### SKILL.md

Read by `skill/skillmd`. Directory layout per the open spec:

```
skill-name/
├── SKILL.md           Required. YAML frontmatter + markdown body.
├── scripts/           Optional. Executable helpers.
├── references/        Optional. Background docs the agent can read.
└── assets/            Optional. Static files.
```

Frontmatter required fields: `name` (≤64 chars, lowercase + hyphens),
`description` (≤1024 chars). Optional fields are passed through.

The three-tier progressive disclosure:

| Tier | What | When loaded |
|---|---|---|
| 1 | name + description | Startup. ~100 tokens per skill. |
| 2 | Body of `SKILL.md` | When the agent invokes the skill or a trigger fires. |
| 3 | Files in `scripts/`, `references/`, `assets/` | On explicit reference from tier 2. |

Activation triggers are declared in frontmatter (`triggers:` —
filename globs, keyword patterns) or invoked explicitly by the user
via `/skill-name`.

Skill search paths (in order, last wins):

1. `framework/harness/skills/` (built-in, ships with the binary)
2. `~/.config/gofastr/harness/skills/` (user-global)
3. `<repo>/.gofastr/harness/skills/` (project-local)

---

## Control plane — engine as a service

The engine is a headless service. It never directly draws to a
terminal, opens a window, or speaks a vendor protocol. Anything that
wants to drive it does so through the **control plane**: a
transport-agnostic protocol with two message kinds.

```go
// From client → engine. Closed sealed union (rule 14).
type Command interface{ isCommand() }
type SendInput        struct { SessionID string; Content []ContentBlock }
type CancelTurn       struct { SessionID string }
type AnswerPermission struct { SessionID string; CallID string; Decision Decision }
type CreateSession    struct { Profile string; Resume *string }
type AttachSession    struct { SessionID string }
type DetachSession    struct { SessionID string }
type SetModel         struct { SessionID string; Model string }
type EnterPlanMode    struct { SessionID string }
type ExitPlanMode     struct { SessionID string; Approve bool }
// Open extension verb for plugin-defined wire commands.
// Plugins use this; the engine routes by Namespace to the
// registered plugin handler.
type CustomCommand    struct {
    SessionID string
    Namespace string         // matches a plugin-claimed slash-command namespace
    Verb      string
    Payload   json.RawMessage
}

// From engine → client. Closed sealed union; plugins use CustomEvent.
type Event interface{ isEvent() }
type CustomEvent      struct { Namespace, Kind string; Payload json.RawMessage }
// see § Extensibility — Event bus for the built-in list
```

### Transports

The same protocol is exposed over multiple transports. The engine
sees a `Client`:

```go
type IdentityClass uint8
const (
    IdentityHuman IdentityClass = iota   // user-at-the-keyboard or a proxy for one
    IdentityAgent                        // an outer agent driving us
)

type Client interface {
    Subscribe(ctx context.Context) <-chan Event
    Send(ctx context.Context, cmd Command) error
    ID() string
    IdentityClass() IdentityClass
}
```

The four transports phase as follows:

| Transport | Phase | Location | When |
|---|---|---|---|
| `inproc` | v0.1 | Go channels | Bundled TUI / web client within the same process |
| `rest` | v0.1 | HTTP + SSE on Unix socket and/or `127.0.0.1:<port>` (LAN opt-in) | Scripts, IDE plugins, other languages |
| `ws` | v0.2 | WebSocket on `127.0.0.1:<port>` (LAN opt-in) | Full-duplex remote clients, multi-attach |
| `mcpserver` | v0.2 stdio / v0.3 streamable HTTP | MCP server | Any MCP-capable agent (Claude Code, Codex, Cursor, custom) drives the harness |

`rest` is the broadest-compat path; `ws` is preferred for interactive
clients (low-latency event push, no SSE buffering quirks); `mcpserver`
is the path for agent-driving-agent — any tool that speaks MCP can
pilot a harness session without writing harness-specific code.

Each transport must pass the `control/conformance/` cross-transport
parity suite before it is considered "supported." The suite walks the
same scenarios (send / cancel / permission / disconnect / reconnect /
multi-attach) against every transport so cross-transport drift gets
caught early.

### REST surface

A handful of resources, JSON bodies, idempotent where possible:

```
GET    /v1/sessions                          List sessions (active + stored)
POST   /v1/sessions                          Create session (profile, optional resume)
GET    /v1/sessions/{id}                     Session meta + last N events
GET    /v1/sessions/{id}/events  (SSE)       Stream events; supports Last-Event-ID resume
POST   /v1/sessions/{id}/input               SendInput
POST   /v1/sessions/{id}/cancel              CancelTurn
POST   /v1/sessions/{id}/permission          AnswerPermission
POST   /v1/sessions/{id}/model               SetModel
POST   /v1/sessions/{id}/plan-mode           EnterPlanMode / ExitPlanMode
DELETE /v1/sessions/{id}                     End session (does not delete log)

GET    /v1/profiles                          List available profiles
GET    /v1/providers                         List providers + their model catalogs
GET    /v1/tools                             List registered tools (with schemas if eager-loaded)
GET    /v1/skills                            List skills (tier-1 metadata)
GET    /v1/mcp/servers                       List connected MCP servers + their status

GET    /v1/health                            Liveness + version
GET    /v1/auth/token                        Issue capability token (when running with --listen)
```

`POST /v1/sessions/{id}/input` accepts either a one-shot synchronous
mode (`?wait=turn`) or the streaming default (returns 202; events
flow via SSE).

### WebSocket surface

A single endpoint:

```
GET /v1/ws?session={id}
```

Bidirectional. Frames are tagged JSON:

```json
{"kind":"command","cmd":"SendInput","sessionId":"…","content":[{"text":"…"}]}
{"kind":"event","event":"TextDelta","sessionId":"…","data":{"text":"…"}}
```

Reconnect resumes from a `lastEventId` query param.

### MCP-server surface

The harness exposes its own engine as an MCP server so any
MCP-capable client (Claude Code, Codex, Cursor, custom agents, the
GoFastr framework profile of the harness itself) can drive a session
without harness-specific bindings. Phasing: stdio in v0.2, streamable
HTTP in v0.3.

- **stdio** (v0.2) — clients spawn the harness as a subprocess via
  `gofastr harness mcp`. The harness exits when stdin closes.
  Capability token passed via env var `GOFASTR_HARNESS_TOKEN` (not
  argv, so it never appears in `ps`). On first run from a new
  parent process (identified by argv0 + binary SHA-256), the
  harness prompts the user out-of-band on their TTY to authorize
  the spawn. Authorizations persist in
  `~/.config/gofastr/harness/mcp-parents.lock`.
- **streamable HTTP** (v0.3) — exposed at `/mcp` on the same
  listener as `rest`/`ws`. Bearer token in `Authorization`; auth
  and TLS rules identical to `ws`.

#### Tools exposed (every Command verb has an MCP tool)

The tool that runs the agent is named honestly to make the
capability visible in MCP UI:

| Tool name | Maps to Command |
|---|---|
| `harness.create_session` | `CreateSession` |
| `harness.list_sessions` | (read) |
| `harness.attach_session` | `AttachSession` |
| `harness.detach_session` | `DetachSession` |
| **`harness.run_agent_with_shell_access`** | `SendInput` — runs the inner agent which can invoke Bash, Read, Write, WebFetch. Outer agent allowlisting this tool is allowlisting RCE-via-LLM. |
| `harness.cancel_turn` | `CancelTurn` |
| `harness.answer_permission` | `AnswerPermission` (rejected if originator's `ID()` matches; agents cannot self-approve) |
| `harness.set_model` | `SetModel` |
| `harness.enter_plan_mode` / `harness.exit_plan_mode` | `EnterPlanMode` / `ExitPlanMode` |
| `harness.end_session` | (DELETE /sessions/{id}) |
| `harness.wait_for_turn` | synchronous helper — sends input + blocks until `TurnEnded` |

A startup banner emitted on every `mcpserver` attach reminds the
outer agent (and the user reading their tool log) that this tool
runs a shell-capable agent. Permission and identity-class rules
from § Multi-client semantics apply: `mcpserver` clients have
`IdentityClass = agent`, so they cannot self-approve.

`run_agent_with_shell_access` has two modes mirroring REST:
synchronous (`wait: "turn"` blocks until the next `TurnEnded` and
returns the final assistant message + tool-call summary) and async
(`wait: "none"` returns immediately; events flow via resource
subscription).

#### Resources exposed (URI-addressable, subscribable per MCP)

| URI | What |
|---|---|
| `harness/v1://sessions` | List of sessions (JSON) |
| `harness/v1://session/{id}` | Session metadata + last N events |
| `harness/v1://session/{id}/events` | Live event stream (MCP subscription) |
| `harness/v1://session/{id}/log` | Full append-only log dump |
| `harness/v1://profiles` | Available profiles |
| `harness/v1://profile/{name}` | Profile spec (skills, MCP servers, tools, permissions) |
| `harness/v1://providers` | Provider catalog |
| `harness/v1://provider/{name}/models` | Model list + pricing |
| `harness/v1://tools` | Registered tool schemas |
| `harness/v1://skills` | Skill tier-1 metadata |
| `harness/v1://skill/{name}` | Skill body |

#### Prompts exposed

Every loaded skill is re-exposed as an MCP prompt. An external MCP
client can call `prompts/get harness/v1://skill/{name}` and inject the
skill body into its own conversation, or trigger `harness.send_input`
with a `skill: <name>` directive to run the skill inside the harness.

#### Why this matters

The interesting capability isn't "another way to attach a UI." It's
**agent-driving-agent**: an outer agent (e.g. Claude Code in a
different repo) can invoke `harness.run_agent_with_shell_access` and
treat a whole harness session as a single capability. Composes
naturally with sub-agents, parallel runs, CI orchestration, and the
self-improvement loop (a harness in `--framework` mode driving
another harness in default mode to test framework changes against a
sample app). The honest tool name and identity-class enforcement are
what make this capability transparent rather than a confused-deputy
hazard.

#### Observability of agent-driven sessions

A session driven by an outer agent has identity-class enforcement
that *blocks the agent from self-approving* — meaning a
`PermissionRequested` event has nowhere to land unless a human
client is attached. The harness handles this proactively:

- **Auto-attach on MCP invocation.** When a session starts via the
  `mcpserver` transport, the harness emits a system notification
  on the user's machine: `"Outer agent <name> started session
  sess_xyz. /sessions:attach sess_xyz to observe."` If `--web` is
  enabled globally, the session auto-opens as a new web tab with
  a banner: *"This session was started by an external agent. You
  are observing as `human`."*
- **Per-parent ack.** First-time invocation from a new parent
  process prompts: *"Always auto-attach sessions from this
  parent? [Y/n]."* Persists in `mcp-parents.lock`.
- **Permission denial on timeout.** If a `PermissionRequested`
  has no `human` client attached and no answer arrives within
  `permission_timeout` (default 60 s), the prompt is denied
  rather than hanging. The inner agent receives a structured
  `Error{Reason: PermissionTimeout}` it can plan around.
- **Cost-in-return-payload.** Synchronous
  `harness.run_agent_with_shell_access` with `wait: "turn"`
  returns the final assistant message **plus** a summary:
  `{cost: 0.043, turns: 7, tools_used: ["Read", "Bash", "Write"]}`.
  The outer agent (and the outer user reading its tool-call log)
  can see what the inner session did.

### Multi-client semantics

All multi-client behavior lives in `control/multiplex/`, not in the
engine. The engine sees `Client.ID()` and `Client.IdentityClass()`;
it never sees how clients are wired.

- A session may have many attached clients simultaneously.
- All clients receive the same event stream (broadcast from
  `control/multiplex`).
- **Total ordering.** `SendInput` from any client is queued at the
  multiplexer with a monotonic timestamp set at queue-arrival.
  Transports never establish ordering directly — they all feed the
  multiplexer.
- **No mid-turn input.** A second `SendInput` arriving while a turn
  is in progress is rejected with
  `Error{Reason: TurnInProgress, OriginatorID: ...}`. The sender
  must `CancelTurn` first.
- **Originator tracking.** The `Client.ID()` of the sender is
  recorded on the turn and surfaces on every event for that turn
  as `OriginatorID`.
- **Permission arbitration.** `PermissionRequested` broadcasts to
  all clients with the `OriginatorID` field set. `AnswerPermission`
  is **rejected** if the answering client's `ID()` equals the
  `OriginatorID` (agents cannot self-approve their own turn) — see
  hard rule 11. By default, at least one client with
  `IdentityClass = human` must answer; multiple human acks for the
  same call after the first are no-ops (first-wins, last-loses for
  audit). `--auto-approve` disables the human-ack requirement and
  is honest about what it does.
- **Detach is non-destructive.** The `EngineRun` continues; events
  for the originator's turn keep streaming to remaining clients.
  If no client remains, the engine continues to completion and the
  log captures everything — re-attach replays from
  `lastEventId`.

### Listening and binding

- **Default:** engine listens only on a Unix socket at
  `~/.local/share/gofastr/harness/control.sock` with mode `0600`.
  Filesystem perms are **not** treated as authentication — see
  Authentication below; a capability token is still required.
- **`--listen 127.0.0.1:PORT`**: bind a loopback port for browser /
  IDE / external-MCP clients. Capability token required.
  Additional defenses against same-origin-policy bypasses (DNS
  rebinding, CSRF):
  - Reject any request whose `Host` header is not exactly
    `localhost:PORT` or `127.0.0.1:PORT`.
  - Reject any request with an `Origin` header not in the explicit
    allowlist.
  - The capability token is required in a custom header
    `X-Harness-Token` (not `Authorization`) so browsers must
    preflight; the preflight fails closed for unauthorized
    origins.
- **`--listen 0.0.0.0:PORT`**: bind LAN. Capability token + TLS
  required; harness refuses to start without `--auth-token-file`
  and `--tls-cert`/`--tls-key`. Token must be bound to specific
  `sessions: [...]` in its claim set.
- **`--no-listen`**: in-process clients only (engine speaks only
  through `inproc`). Useful for tests or pure-CLI use.
- **`gofastr harness mcp`** (v0.2): launches a one-shot stdio MCP
  server bound to a single session; exits when stdin closes.
  Capability token via env var `GOFASTR_HARNESS_TOKEN`; first-run
  parent-process attestation required.

### Authentication

The harness treats every transport as an authenticated channel,
including Unix sockets. The defense-in-depth posture means no single
filesystem permission, network rule, or process-trust assumption is
load-bearing on its own.

#### Token claim set

Tokens are internally-issued (no third-party dep) and carry an
explicit claim set:

```json
{
  "sessions": ["sess_abc", "sess_def"],
  "commands": ["SendInput", "CancelTurn"],
  "identity_class": "agent",
  "exp": 1750000000,
  "can_mint": false,
  "nbf": 1747400000,
  "jti": "tok_xyz"
}
```

- `sessions`: which `SessionID`s the token may attach to (empty
  array = none; absent = all — only allowed for the initial
  bootstrap token).
- `commands`: which `Command` verbs the token can issue.
- `identity_class`: `human` or `agent`. Set at token mint;
  enforces hard rule 11 on the wire.
- `exp`: Unix-second expiry. Default 24 h.
- `can_mint`: whether holding this token allows issuing further
  tokens. Default `false`.
- `nbf`: not-before timestamp; reject if `now < nbf`.
- `jti`: token ID for revocation list.

#### Issuance

`GET /v1/auth/token` on the Unix socket initiates issuance with the
desired claim set. The harness picks a **confirmation channel**
from this priority list, and surfaces in the response which channel
was chosen (so the requester knows where to look):

1. An already-attached `human`-class client (TUI or bundled web) —
   the 6-digit code appears as a modal **inside the existing
   trusted client**. This works when the user is on the web tab
   only (no visible TTY).
2. The harness's own TTY if it has a controlling terminal.
3. A desktop system notification (`osascript` on macOS, `notify-send`
   on Linux).
4. None available → refuse token issuance with the explicit error:
   `"Token issuance requires an interactive confirmation channel.
   For headless/CI: pre-provision a token file. For service
   managers: launch with --auth-channel notify."`

The requester must POST the code back within 60 s to receive the
token. This blocks the simplest "any process running as the user
mints tokens" attack — the attacker needs concurrent visibility of
one of the user's trusted surfaces.

For headless/CI use, a pre-provisioned token file
(`~/.config/gofastr/harness/ci-token.json`) is read at boot; the
file must have mode `0400` and be created out-of-band. The
`approved.lock` for skills/context/hooks is also pre-generated via
`gofastr harness ack --emit-lockfile` and either committed to the
repo or stored as a CI secret.

#### CI bootstrap flow (v0.1)

The full CI setup uses three pre-provisioned artifacts:

1. **`ci-token.json`** (mode `0400`) — capability token with
   claims scoped to whatever the CI job needs to do. Bound to a
   specific session ID for one-shot runs.
2. **`approved.lock`** — generated locally once via `gofastr
   harness ack --emit-lockfile` and committed to the repo (or
   stored as a CI secret). Subsequent CI runs read this and skip
   TOFU prompts.
3. **`GOFASTR_HARNESS_MACHINE_KEY`** — env var holding the
   credstore key, passed as a CI secret. Replaces the passphrase
   prompt for the encrypted credential store. The value must decode
   to exactly 32 bytes; three encodings are accepted: 32 raw bytes,
   64 hex characters, or base64 (standard/URL, padded or not). A
   value that does not decode to 32 bytes is rejected loudly — the
   harness never silently falls back to a weaker secret.

CI engineers verify acks without launching the agent loop using:

```
gofastr harness verify-ack             # exits non-zero on hash drift
```

(`verify-ack` is specified here but not yet wired into `cmd/gofastr` —
today the CLI dispatches only the `mcp` and `creds` subcommands, and
anything else starts an interactive session.)

Drift between the lockfile and live content **fails the CI run
explicitly** with the changed file paths in the error — never
silently approves.

#### Revocation

Revoked `jti`s are stored in `~/.local/state/gofastr/harness/revocations.db`
and checked on every request. Revocation API:
`DELETE /v1/auth/tokens/{jti}`.

#### Per-transport rules

| Transport | Auth | Notes |
|---|---|---|
| `inproc` | trust boundary = process | No token; engine and client share memory |
| `rest` over Unix socket | token + `0600` perms | Token required even on socket — perms are defense in depth, not authentication |
| `rest` / `ws` over TCP loopback | token + `Host`/`Origin` checks + custom header | Defends against DNS rebinding |
| `rest` / `ws` over LAN | token + TLS | Token bound to specific sessions |
| `mcpserver` stdio | token via env var + parent attestation | Token never in argv |
| `mcpserver` HTTP | token in `Authorization` | Same as TCP transport |

Per-session capability tokens are the unit of sharing — handing a
token to a teammate or an IDE plugin grants attach rights to one
session without exposing the rest of the harness.

---

## Protocol versioning & evolution

The control protocol is the **long-term API surface** of the
harness. Every external client (IDE plugin, TS SDK, Python client,
mobile app, agents driving us over MCP) commits to its shape. This
section is normative — implementers MUST follow it, and the
`control/conformance/` suite tests against it.

### Handshake (required before any other command)

Every transport speaks the same `Handshake` envelope before
exchanging `Command`s. REST: `GET /v1/handshake`. WS: first frame.
MCP: extends `initialize`. Stdio: first JSON-RPC message.

```json
{
  "protocol_version":          "0.1.0",
  "canonical_form_version":    1,
  "schema_version_token_claim": 1,
  "schema_version_profile":     1,
  "schema_version_session_log": 1,
  "command_kinds": ["SendInput", "CancelTurn", "AnswerPermission",
                    "CreateSession", "AttachSession", "DetachSession",
                    "SetModel", "EnterPlanMode", "ExitPlanMode",
                    "CustomCommand"],
  "event_kinds":   ["TextDelta", "ThinkingDelta", "ToolCallStarted",
                    "ToolCallProgress", "ToolResult", "TurnStarted",
                    "TurnEnded", "CompactionTriggered",
                    "CostIncremented", "PermissionRequested",
                    "Cancelled", "Error", "StreamGap",
                    "TokenExpiring", "HookTimeout", "SessionEnded",
                    "MCPServerDown", "TurnTiming",
                    "CustomEvent"],
  "features": ["plan_mode", "branching", "delegate", "rest", "ws",
               "mcpserver_stdio", "mcpserver_http", "auto_approve",
               "auto_attach_on_mcp"],
  "resource_uri_scheme": "harness/v1"
}
```

- `protocol_version` is **SemVer**.
- **Minor bumps are additive-only**: new optional `Command`/`Event`
  kinds, new optional fields. Clients on an older minor MUST
  continue to work.
- **Major bumps require client re-implementation**.
- **Deprecation cycle is one minor release minimum**: a feature
  marked deprecated in 0.4 may be removed in 0.5 but not earlier.
- Absence of a feature flag is **normative**: any optional feature
  MUST be flagged to be relied upon. Built-in features still appear
  in the array for explicit detection.

Servers MUST reject pre-handshake `Command`s with
`Error{Reason: HandshakeRequired}`.

### Canonical event envelope

All transports carry the same event JSON. Envelopes are
**framing only**, never the event body.

```json
{
  "id":          12345,                 // uint64, monotonic per SessionID
  "kind":        "TextDelta",           // matches event_kinds in handshake
  "session":     "sess_01H…",
  "originator":  "cli_01H…",
  "ts":          "2026-05-23T19:00:00.123Z",
  "payload":     { ... event-specific ... }
}
```

Transport-specific framing:

| Transport | Envelope |
|---|---|
| SSE | `id: <id>\nevent: <kind>\ndata: <canonical JSON>\n\n` |
| WS | `{"frame":"event","body": <canonical JSON>}` |
| MCP notifications | `notifications/resources/updated` with `params.contents` = canonical JSON |
| inproc (Go channels) | `Event` Go struct with the same field set |

### Unknown-field policy

| Surface | Unknown field rule |
|---|---|
| `Command` body fields | **Ignore unknown** (additive evolution) |
| `Event` body fields | **Ignore unknown** |
| Token claim set | **Reject token if any claim listed in its own `critical_claims: []` is unrecognized; ignore other unknowns.** Fail-closed on capability-restricting claims. |
| Profile TOML | **Warn, do not error** on unknown keys (forward-compat for downgrades). Require explicit `schema_version` bump for breaking renames. |
| MCP resource bodies | **Ignore unknown** |

Field-name reservations:
- `x_*` — vendor / plugin extensions
- `_*` — engine internal, off-the-wire
- No other prefix is reserved.

### Token claim versioning

Token claim set includes a required `ver: <int>` field.

```json
{
  "ver": 1,
  "jti": "tok_01H...",
  "sessions": ["sess_01H..."],
  "commands": ["SendInput", "CancelTurn"],
  "identity_class": "agent",
  "exp": 1750000000,
  "nbf": 1747400000,
  "can_mint": false,
  "critical_claims": ["sessions", "commands"]
}
```

- Verifiers MUST reject any token whose `ver` they do not recognize.
- Adding a non-capability-restricting claim (e.g. `display_name`)
  does **not** bump `ver`.
- Adding any claim whose absence weakens security **does** bump
  `ver`.
- `critical_claims` lists fields whose presence-and-meaning matter
  for security; verifiers must understand all listed claims or
  reject. Newly-minted v2 tokens listing a v2-only claim in
  `critical_claims` are correctly rejected by v1 verifiers.

### Stream resume (unified)

Every transport that delivers events supports resume with the same
semantics:

- Client provides last seen sequence ID via:
  - SSE: `Last-Event-ID` header (HTTP standard)
  - WS: `lastEventId` query param on reconnect
  - MCP: resource subscription `?since=<id>` query param on the
    `harness/v1://session/{id}/events` URI
- Server replies with all events with `id > since` in order, then
  continues live.
- If `since` is **older than the TTL window** (event content has
  expired), server emits a `StreamGap{from, to, reason: "ttl"}`
  event first, then the available range — never silently skip.
- If the `EngineRun` has shut down, re-attach to the `LogID`
  spawns a new `EngineRun`; events with `id > since` replay from
  the log; live stream begins once the new run accepts input.

### Canonical message form versioning

The internal Anthropic-shape canonical message form has its own
`canonical_form_version` (in the handshake). Provider adapters
target an explicit version; the engine asserts they match. When
Anthropic (or any provider) ships a new content block type, the
harness explicitly decides whether to absorb it (bump
`canonical_form_version`) or wrap it opaquely as a
provider-stamped block (no bump).

### SQLite session log: private schema

The on-disk SQLite schema is **private**. External consumers MUST
NOT read `sessions.db` directly. The public path for replay,
export, and external tooling is:

- Live: `GET /v1/sessions/{id}/events` (SSE) or `harness/v1://session/{id}/events` (MCP)
- Offline: `harness sessions export --session <id> --format jsonl` produces a stable JSONL contract whose shape matches the canonical envelope.

The schema may change between any two minor releases (with a
forward-only migration); the JSONL export shape evolves under the
same additive rules as wire events.

### Plugin distribution model

Plugins are **compiled in**. The `Plugin` interface is a clean
internal seam for composition, not a binary-distribution API. Go's
`plugin.Open` is rejected for ABI fragility. Adding a third-party
capability to a running harness uses **MCP server** as the
distribution channel — `ToolSource` consumes it, the harness
adds permissions, the plugin author ships a separate binary
checked by sha256 pinning in the profile.

`docs/harness-clients/` will ship reference SDKs (TS, Python, VS
Code skeleton) that drive the harness via the control protocol —
those are *clients*, not plugins.

### Conformance suite is normative

`control/conformance/` is the source of truth where this document
and an executable test disagree. The suite is runnable by external
implementers:

```
gofastr harness conformance --against http://localhost:8080
gofastr harness conformance --against unix:///path/to/control.sock
gofastr harness conformance --against stdio:///usr/local/bin/some-other-harness
```

(The `conformance` subcommand is specified here but not yet wired into
`cmd/gofastr`; the suite itself lives in `control/conformance/` and runs
via `go test`.)

The suite is versioned alongside `protocol_version`. v0.1's suite
runs against v0.1 servers; v0.2 adds new scenarios behind the
v0.2 feature flags.

#### Test infrastructure (v0.1)

- **In-memory transport for PR-gating.** A `net.Pipe`-backed
  transport adapter runs the conformance scenarios without the
  kernel network stack — fast, deterministic, no port-reuse
  races. PR CI runs this matrix on every change.
- **Real-socket smoke nightly.** One pass per platform
  (macOS / Linux / Windows) of the same scenarios against actual
  Unix sockets / loopback TCP to catch OS-specific bugs (TCP
  backpressure, socket EOF semantics, ephemeral exhaustion).
  Not PR-gating; failures open a tracking issue.
- **Fake clock.** Disconnect/reconnect/timeout scenarios use an
  `internal/clock` interface (stdlib-only — define an interface,
  inject a real or fake implementation). No wall-clock waits in
  PR-gating tests.
- **Flake budget.** Any single conformance test flaking >0.5%
  in a rolling 100-run window is auto-quarantined and assigned a
  tracking issue. No silent retries in CI.

### Backward-compat policy (stated explicitly)

- Pre-1.0: breaking changes allowed at every minor bump
  (0.x → 0.x+1), with one minor of deprecation overlap where
  feasible.
- 1.0+: SemVer-strict. Breaking changes require a major bump.
- A deprecation that lands in v(N) is removable in v(N+1) but not
  earlier, providing one full release cycle of overlap.
- Migration tools ship in the same release that introduces a
  breaking change.

---

## Clients

Clients are independent of the engine and may live in the same
process (bundled) or anywhere (external). Two clients ship in v1.

### TUI (bundled, `inproc`)

Pure stdlib + `golang.org/x/term`. Architecture:

- Raw mode via `term.MakeRaw` (saves/restores termios on exit)
- ANSI sequences written to `os.Stdout` directly
- Input parsing reads bytes from `os.Stdin`, decodes escape sequences
  (cursor keys, function keys, mouse, bracketed paste)
- Elm-arch update loop: `(state, msg) → (state, cmd)`
- Components: scrollback view (virtual list with diff-based redraw),
  input area (multiline with history), status line, modal overlay
  (permission prompts, diff preview at TOFU ack, file picker,
  command palette), sidebar (sessions, skills, MCP, hooks,
  session-policy)
- Resize handled via `SIGWINCH`

**Status line layout** (left to right, fixed columns):

```
sess_01HXX… · framework · openrouter:claude-sonnet-4 · $0.04/turn · $0.31/sess · web: http://localhost:8421
```

- Cost meter units explicit (`$N/turn · $N/sess`); USD with cents
  precision; updates at ≤4 Hz to avoid flicker, ≥1 Hz when
  streaming. `--quiet-cost` hides cost columns.
- When `RoutingProvider` lands (v0.3) and a turn touches multiple
  providers, the cost meter shows the split:
  `$0.02 zai + $0.05 openrouter = $0.07/turn`. Never silently
  unified.
- The `--web` URL is **pinned** in the status line for the
  lifetime of the session. OSC 8 hyperlink markup wraps the URL
  for terminals that support it; raw URL printed alongside as
  fallback (tmux compat varies).

**Ctrl-C semantics.** Two-press exit, mirroring `git rebase`-style
TUIs:
1. First Ctrl-C cancels the active turn (`CancelTurn`).
2. Second Ctrl-C within 2 s exits the harness cleanly.
3. Between presses, the TUI shows a brief banner:
   `"Press Ctrl-C again within 2s to exit."`

**Diff-review UI** (used at TOFU ack and at permission-prompt diff
preview):
- Side-by-side diff (left = approved hash content, right = current
  content); single-pane unified-diff fallback when terminal width
  is too narrow.
- Keys: `n`/`p` next/previous hunk, `a` accept, `d` deny, `v`
  view full file, `s` save to file for offline review.
- For new (never-before-ack'd) files, the left pane shows
  "[new file]" and the right pane shows the full content with
  syntax highlighting.

### Web (bundled, attaches via `inproc` or `ws`)

Built on GoFastr itself. The harness imports `framework` and mounts a
GoFastr `App` on a random local port chosen at boot. Entities:
`Session`, `Turn`, `Message`, `ToolCall`, `Event`. Pages are
server-rendered with island hydration per the GoFastr UI runtime.
Live event push uses SSE (`stream.go`) bridged from the control-plane
event stream. The web client is started opt-in (`--web`); when on, it
runs alongside the TUI.

This is real dogfooding: the framework's UI runtime, entity model,
hooks, and SSE plumbing all exercise themselves through the harness.

### External clients

Anything that can speak HTTP or WebSocket is a first-class client.
Reference implementations to ship in `docs/harness-clients/`:

- **curl recipes**: create a session, send input, stream events.
- **TypeScript client**: thin npm package wrapping `fetch` +
  EventSource / WebSocket for IDE-plugin authors.
- **Python client**: same shape for scripts and notebooks.
- **VS Code extension skeleton**: minimal wiring to attach to a
  running harness from the editor.

External TUIs (a vim plugin, a Slack bridge, a CI bot, a remote SSH
TUI) all use the same protocol. No engine changes needed.

---

## Persistence

### Sessions

SQLite append-only event log at
`~/.local/share/gofastr/harness/sessions.db`. **User-scoped, not
worktree-scoped**: each worktree gets its own `EngineRun` but shares
the log store. One row per `Event` emitted on the bus. Indexed by
`(LogID, turn, time, jti)` plus a covering index for the
"events since `id`" replay query. Schema is private (see §
Protocol versioning → SQLite session log).

### Migration ledger (mandatory from v0.1)

```sql
CREATE TABLE schema_migrations (
  version    INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL,
  sha256     TEXT NOT NULL  -- of the migration SQL file
);
```

Migrations live as numbered SQL files under
`framework/harness/session/sqlite/migrations/*.sql`. Forward-only.
A harness binary refuses to open a DB whose `max(version)` exceeds
its known max (so downgrades fail loud). Migrations on the
encrypted page-level DB hold an explicit lock; the doc presents a
"Database is upgrading…" message in the UI during the wait.

### Tool-call intent / outcome ledger (mandatory from v0.1)

To survive crashes mid-tool-call without re-executing destructive
operations on resume:

```sql
CREATE TABLE tool_call_intents (
  call_id     TEXT PRIMARY KEY,        -- call_01H…
  log_id      TEXT NOT NULL,
  tool_name   TEXT NOT NULL,
  args_hash   TEXT NOT NULL,
  is_mutating BOOLEAN NOT NULL,        -- declared per-tool (Read=false, Bash=true)
  started_at  TEXT NOT NULL
);

CREATE TABLE tool_call_outcomes (
  call_id      TEXT PRIMARY KEY REFERENCES tool_call_intents(call_id),
  outcome      TEXT NOT NULL,          -- "ok" | "error" | "cancelled" | "timeout"
  completed_at TEXT NOT NULL,
  result_ref   TEXT                    -- pointer into events table
);
```

- `tool_call_intents` is written **before** the tool process spawns
  (with `fsync` for mutating tools).
- `tool_call_outcomes` is written **after** the tool returns
  (`fsync` for mutating tools).
- On resume, the engine queries
  `SELECT * FROM tool_call_intents WHERE call_id NOT IN (SELECT call_id FROM tool_call_outcomes)`.
- For each orphan: if `is_mutating = false`, default to "retry."
  Otherwise **halt the resume** and present a structured prompt:
  `"Tool call X (Bash 'rm -rf ./build') started at T but did not
  finish. Mark as: [Failed | Succeeded with note | Abort session]."`
- Read-only tool sources (`Read`, `Glob`, `Grep`, `Ls`,
  `WebFetch`) declare `is_mutating=false`; everything else
  declares `true`.

The cost is one extra fsync per mutating tool call (~1ms on SSD).
The benefit is no destructive re-execution on resume.

### Storage policy

**At rest.** The database file is encrypted with a per-machine key
stored in the OS keychain (encrypted-file fallback as in §
Credentials). The user's hot keychain unlock is required to read
the log; a stolen laptop with the screen locked cannot read it
without the user's password.

**Redaction middleware.** A `session/redact.go` middleware on the
event-write path matches secret regexes against `ToolResult.Content`,
`ToolCallStarted.Args` (the argv of Bash commands often contains
tokens), and `TextDelta` (pasted secrets). Patterns: AWS access
keys (`AKIA…`), GitHub PATs (`ghp_…`, `github_pat_…`),
`-----BEGIN .* KEY-----`, `Bearer [A-Za-z0-9-_\.]+`, JWT-shaped
tokens, common cloud-provider key prefixes. Matched substrings are
replaced with `«redacted:KIND»` markers and the original is
discarded — not stored anywhere.

**TTL.** Full-content events (`TextDelta`, `ToolResult` body,
`ToolCallStarted.Args`) are retained for 30 days by default. After
expiry, metadata only (event kind, timestamp, originator, token
counts, costs) survives in the row; the content column is replaced
with `«ttl-expired»`. Configurable per-profile.

- **Pin to disable expiry.** `/sessions:pin <id>` marks a session
  as no-TTL, no-idle-shutdown. Pinned sessions are listed with
  `★` in `/sessions:list`.
- **Pre-expiry banner.** At boot, if any sessions expire within 7
  days, the TUI / web shows a banner: `"3 sessions expire this
  week: sess_…, sess_…, sess_… — /sessions:pin or /sessions:export
  to keep them."` The banner is dismissable per-session-set.

**Operations:**

- **Resume.** Open an existing `LogID` and create a new `EngineRun`
  bound to it. Conversation is reconstructed from events up to the
  last compaction boundary or `TurnEnded`, whichever is later.
- **Branch.** Copy events up to a `TurnEnded` boundary into a new
  `LogID`. Branching mid-turn is **not supported**; the UI must
  pick a `TurnEnded` event as the cut point. Tool_use IDs in the
  copied events are rewritten so the resumed engine can issue new
  tool_use blocks without colliding with the original conversation
  if both branches are later compared.
- **Replay = step-through.** Drives a surface from the log for
  debugging UIs. **Does not re-execute** — provider calls are not
  re-issued. Cache state and live MCP connections are
  non-replayable; replay shows what happened, not "what would
  happen again." Re-execution under different conditions is a
  separate concept and not in scope for v0.x.
- **Export.** JSONL (event-by-event) or markdown (rendered
  transcript). The redaction pass and TTL apply equally to
  exports.

### Memory

Markdown files with YAML frontmatter; one file per memory; indexed by
a flat `MEMORY.md` (same shape as the user's existing auto-memory).
Four types: `user`, `feedback`, `project`, `reference`. Read by a
request middleware that selects relevant entries per turn (tag match +
description fuzzy match).

### Credentials

**Encrypted-file is the primary store.** AES-GCM with a key derived
from either a user passphrase (interactive use) or a machine-bound
key (CI / headless). Implementation lives in `provider/credstore/`.

OS keychain integrations are **opt-in plugins**, build-tagged per
platform:

- macOS (`keychain_darwin.go`): shells out to `security` (the CLI),
  not CGO. Avoids the CGO+keychain entanglement on first run.
- Linux (`keychain_linux.go`): D-Bus client speaking
  `org.freedesktop.secrets`. Falls back to encrypted-file if no
  Secret Service available.
- Windows (`keychain_windows.go`): `CredRead`/`CredWrite` via
  `golang.org/x/sys/windows`.

Provider tokens are held in a **credential-helper subprocess**
(`provider/helper/`) — a separate process that holds the unlocked
secrets and signs provider requests on the harness's behalf. The
agent process never sees the raw token, and the agent's Bash tool
cannot exfiltrate it from the harness's own memory.

Per § Threat model, the default Bash permission preset **blocks**
`security`, `secret-tool`, `keyctl`, and `kwalletcli` to prevent
the obvious credential-exfiltration paths via the agent's own tool
surface.

#### First-run setup — `gofastr harness creds`

On first run, provider API keys must be stored in the credstore
before starting the harness. The `gofastr harness creds` subcommand
manages credentials without booting the full harness:

```
gofastr harness creds add <provider> <account> <secret>
gofastr harness creds list
gofastr harness creds delete <provider> <account>
```

**Examples:**

```sh
# Store an OpenRouter API key
gofastr harness creds add openrouter default sk-or-v1-...

# Store a ZAI API key
gofastr harness creds add zai default <api-key>

# List stored providers (no secrets shown)
gofastr harness creds list

# Remove a stored key
gofastr harness creds delete openrouter default
```

**Key resolution** (same priority order as `gofastr harness`):
1. `GOFASTR_HARNESS_MACHINE_KEY` env var — 32-byte key in raw, hex,
   or base64 encoding. Used for CI/headless where no passphrase prompt
   is possible.
2. `GOFASTR_HARNESS_PASSPHRASE` env var — derives a key via
   PBKDF2-SHA256 with a per-install salt at
   `~/.config/gofastr/harness/salt`.
3. A built-in dev passphrase (warns loudly; suitable for local
   experimentation only).

The credstore file is at `~/.config/gofastr/harness/creds.enc`.
`XDG_CONFIG_HOME` overrides the `~/.config` base when set.

**Note on env-var vs credstore:** The harness also reads API keys
from `OPENROUTER_API_KEY` and `ZAI_API_KEY` environment variables
(and from `.harness-secrets/env`). Use whichever is more convenient.
The credstore is the recommended path for long-lived developer
machines; env vars suit ephemeral CI environments.

### Config

XDG layout:

- `~/.config/gofastr/harness/config.toml` — global defaults
- `~/.config/gofastr/harness/approved.lock` — TOFU file hashes
- `~/.config/gofastr/harness/mcp-parents.lock` — TOFU parent-process
  hashes for stdio MCP-server transport
- `~/.local/share/gofastr/harness/sessions.db` — session store (encrypted)
- `~/.local/share/gofastr/harness/memory/` — auto-memory
- `~/.local/share/gofastr/harness/control.sock` — Unix socket
- `~/.local/state/gofastr/harness/log/` — harness's own logs
  (one file per day: `harness-YYYYMMDD.log`)
- `~/.local/state/gofastr/harness/revocations.db` — revoked token jtis
- `<repo>/.gofastr/harness/` — project-local overrides (skills,
  hooks, project profile)

---

## Profiles

A profile is a TOML file that lists which plugins to activate and
how to configure them. Two preset profiles ship:

```toml
# profile/framework.toml — working on GoFastr itself
schema_version = 1                                      # see § Protocol versioning
name = "framework"
default_model = "openrouter:anthropic/claude-sonnet-4"   # Copilot lands in v0.2; not the default until 30d stable

prompt_header = """
You are working on the GoFastr framework. Read core-ui/ARCHITECTURE.md
and framework/ARCHITECTURE.md before touching their domains. The hard
rules in CLAUDE.md are still load-bearing for this profile during the
AGENTS.md transition.
"""

# Project-instruction sources processed in order, concatenated into the system prompt.
# Each entry resolved by context/reader.go; no per-source plugin.
context_sources = ["AGENTS.md", "CLAUDE.md"]

skill_packs = ["builtin", "gofastr-framework"]

# MCP server binaries pinned by sha256 (hard rule, security/threat-model).
# Missing or mismatched hash refuses to spawn the server.
mcp_servers = [
  { name = "gofastr-introspection",
    cmd = "gofastr", args = ["mcp"],
    sha256 = "abc123...def",
    discovery = "lazy" },
]

tool_packs = ["fs", "git", "web", "gofastr"]

permissions = "preset/framework.toml"

# Project-local hooks are off by default. Enabling requires
# --allow-project-hooks AND a TOFU ack per hook.
allow_project_hooks = false
```

```toml
# profile/default.toml — working with GoFastr (downstream apps)
schema_version = 1
name = "default"
default_model = "zai:glm-4.6"

prompt_header = """
You are helping the user build an application with the GoFastr
framework. Prefer kiln for scaffolding entities and pages; consult
docs/ for framework features.
"""

context_sources = ["AGENTS.md"]

skill_packs = ["builtin", "gofastr-apps", "kiln"]

mcp_servers = [
  { name = "kiln",
    cmd = "kiln", args = ["mcp"],
    sha256 = "def456...abc",
    discovery = "eager" },
]

tool_packs = ["fs", "git", "web"]

permissions = "preset/default.toml"

allow_project_hooks = false
```

Selection: `gofastr harness --framework` or `gofastr harness` (default).
Override: `--profile <path>`.

### Mid-session profile switching

`/profiles:framework` (or any other profile) **does not morph the
running session** — that would produce a model that's mid-thought
in one system prompt and mid-tool-schema in another. Instead, the
command:

1. Creates a new `EngineRun` against the chosen profile.
2. Prompts the user: "Import the last N turns into the new
   session?" (default `N=5`). On accept, those turns are copied
   into the new session's first-turn context as a summary (not a
   full replay).
3. Pauses the old `EngineRun` (becomes resumable via
   `/sessions:resume <id>`). User can return to the old context.
4. Visible message in the new session:
   `"Started new session sess_xyz on profile 'framework'. Old
   session sess_abc is paused; /sessions:resume sess_abc to return."`

`/profiles:framework --in-place` exists for power users who
explicitly want to morph the running session; the harness warns
that the model may be incoherent across the boundary.

### Trust gates on profile-loaded content

- **MCP server binaries** — `sha256` is required when the profile is
  loaded from project scope; optional but recommended in user/global
  profiles. Refusal to spawn on mismatch.
- **Skills** — every `SKILL.md` is SHA-256 hashed at first load (rule
  13); changes prompt a diff and a re-ack.
- **Context sources** — every `AGENTS.md` / `CLAUDE.md` / etc. is
  also TOFU'd. Nested `AGENTS.md` files discovered during walk-up
  are individually ack'd.
- **Hooks** — `~/.config/gofastr/harness/hooks/*` are user-owned and
  trusted. `<repo>/.gofastr/harness/hooks/*` are off unless
  `--allow-project-hooks` is set AND each hook has been ack'd via
  TOFU. Hook commands appear in the diff at ack time.

---

## Lifecycle / boot

1. CLI parses flags, picks profile, resolves XDG paths.
2. Profile loads plugins; each plugin's `Register(h *Harness)` runs.
   Plugins attach middleware, subscribe to events, register
   backends, claim slash-command namespaces.
3. Context reader processes the profile's `context_sources` list
   (`AGENTS.md` walk-upward; `CLAUDE.md`/etc. if listed). **Every
   file goes through TOFU** (rule 13) — new/changed files block the
   boot with an interactive ack.
4. Skill registry scans the three skill search paths; tier-1
   metadata loaded. **TOFU** on every `SKILL.md`. If `--auto-approve`
   is set on a non-interactive launch, all unknown files default to
   *deny*; the harness logs and exits non-zero. Auto-approving
   new skill content is never silent.
5. `ToolSource`s register tools. MCP servers spawn after sha256
   verification against the profile's `mcp_servers[].sha256` —
   mismatch refuses to spawn. Discovery proceeds per declared mode
   (eager/lazy).
6. Credential helper subprocess starts; the agent process never
   holds raw provider tokens.
7. Control plane starts. Unix socket always (mode `0600`); TCP/WS
   only if `--listen`. If `--listen` is set, no token is auto-issued —
   the first client uses `GET /v1/auth/token` on the Unix socket
   and completes the TTY 6-digit confirmation.
8. `mcpserver` stdio mode (v0.2+, `gofastr harness mcp`) reads the
   capability token from `GOFASTR_HARNESS_TOKEN` env var. On first
   spawn from an unknown parent (argv0 + binary SHA-256), the
   harness pauses and prompts the user out-of-band to authorize.
9. Bundled clients start (TUI takes over the terminal; web client,
   if `--web`, prints its URL). Both attach as `inproc` clients
   with `IdentityClass = human`.
10. Engine waits for input from any attached client; first
    `SendInput` fires the loop.

Shutdown is graceful via `framework/lifecycle` (the same shutdown
contract the rest of GoFastr uses). Attached clients receive a
`SessionEnded` event; the control plane stops accepting new
commands; MCP servers receive `shutdown` messages; the credential
helper exits; the TUI restores the terminal; SQLite is flushed and
re-encrypted; revocation list is persisted; in-memory tokens are
zeroed.

### Concurrent-worktree deployment model

Multiple `gofastr harness` invocations across worktrees share the
user-scoped log + Unix socket path. v0.1 commits to
**one-engine-many-clients**:

- The first invocation in a user session binds the Unix socket and
  becomes the engine.
- Subsequent `gofastr harness` invocations detect the existing
  socket and **run as `inproc` clients** of the existing engine —
  no second engine process, no SQLite WAL contention, no
  duplicate credential helpers.
- Sessions are tagged with `working_dir`. `/sessions:list` shows
  the working dir column so the user can pick the right one.
- The TUI on attach offers: "Attach to last session in this
  directory, create new, or pick from list."

Tradeoff: a panic in one worktree's turn kills the engine
process and therefore other worktrees' sessions. The mitigations
are (a) the agent loop has a panic-recovery boundary per turn, so
panics produce an `Error` event and the engine continues; (b) the
shared SQLite log means re-attach reconstructs every session
quickly after any crash.

### Idle-shutdown and resource lifecycle

Detach is non-destructive, but the engine doesn't hold sessions
forever:

| Resource | Idle policy |
|---|---|
| `EngineRun` with no attached clients **and** no turn in progress | `session.idle_timeout` default 30 min → `SessionEnded{reason: idle}`. Re-attach spawns a new `EngineRun` against the same `LogID`. `/sessions:pin <id>` opts out. |
| MCP server process | Refcounted by sessions referencing its tools. Last session ends → server gets `shutdown` (5s grace) → SIGTERM → SIGKILL. Respawn on next reference. |
| In-memory caches (AGENTS.md, skill bodies, model catalogs, MCP schemas, activated-skill tier-2 bodies) | LRU bounded at **32 MB total** across the harness; evict on overflow; per-cache size + eviction counters readable at `harness/v1://runtime`. Each cache declares its weight via a single shared budget so no individual cache can dominate. |
| Provider HTTP connection pools | `IdleConnTimeout` set to 90 s explicitly; no unbounded `keep-alive`. |

A long-running `--no-listen` daemon may run for weeks without
unbounded growth.

### Credential-helper supervision

The credential-helper subprocess (`provider/helper/`) holds
unlocked provider tokens. If it crashes, every subsequent provider
call fails. Supervisor contract:

- Liveness check: heartbeat via Unix-socket EOF detection (helper
  writes a `\x00` byte every 5 s; engine treats missed heartbeat
  as crash).
- On crash: auto-respawn once per session. Engine pauses any
  in-flight provider calls and re-issues after respawn.
- On repeated crash (≥2 within 60 s): emit `Error{Reason:
  CredentialHelperFailed}`, escalate to `/health`, do **not**
  loop-respawn.
- `/health` slash command shows helper state along with engine,
  MCP servers, control plane listeners, credstore, session DB.

### MCP server supervision

```go
type MCPServerSupervisor struct {
    MaxRestarts          int           // default 3
    RestartWindow        time.Duration // default 60s
    GiveUpAfter          int           // default 5 consecutive failures
    GiveUpCooldown       time.Duration // default 1h
}
```

On child-process exit:
- Within `RestartWindow`, restart up to `MaxRestarts` with
  exponential backoff (1s, 2s, 4s).
- On give-up, emit `MCPServerDown{name, reason, attempts}`.
- Tools registered by the down server enter **degraded** state in
  the registry — still listed (so the model doesn't hallucinate
  their absence), but `Run` returns a structured
  `«mcp-server-unavailable»` ToolResult that the model can plan
  around.
- `/mcp:restart <name>` manually retriggers.

### Hook timeouts

Hooks are shell commands with explicit deadlines:

| Hook event | Default timeout |
|---|---|
| `SessionStart` | 5 s |
| `UserPromptSubmit` | 5 s |
| `PreToolUse` | 30 s |
| `PostToolUse` | 30 s |
| `Compact` | 60 s |
| `Stop` | 5 s |

Override per-hook in the TOML: `{ event = "PreToolUse", cmd =
"…", timeout_ms = 60000 }`. At deadline: SIGTERM, then SIGKILL
at deadline+5 s. Hook stdout/stderr capped at 64 KB; truncated
output is logged with a marker. A hook exiting non-zero emits
`HookTimeout` (when killed) or `HookError{exit_code}` (when it
exited on its own).

### Crash-mid-turn recovery (cross-reference)

See § Persistence → Tool-call intent / outcome ledger for the
specific recovery path. On any abnormal exit, on next boot the
engine queries orphan intents and presents the user with a
structured choice rather than re-executing.

### Token expiry mid-turn

- Tokens carry `TokenExpiring` event 5 min before `exp`.
- In-flight subscriptions are allowed to drain for up to **60
  seconds past `exp`** before the server cuts them.
- `Command`s received after `exp` are rejected with
  `Error{Reason: TokenExpired}`; clients refresh via
  `GET /v1/auth/token`.

---

## Future extension shapes (none requires touching the loop)

The design is sized so the following ideas slot in as a plugin, a
middleware, a `Provider` composition, or a backend swap. Each is
labeled with the seam it uses so we don't pretend a feature is free
when it isn't.

- **Multi-model per turn — `RoutingProvider` composition, NOT
  middleware.** A `Provider` that wraps `{router, executors[]}`.
  Routing decisions stay inside the composition; cache-control,
  thinking-block provider-binding, token counting, and
  `CostIncremented` attribution remain per-underlying-provider.
  Tried as middleware first; middleware is monomorphic
  (one-request-to-one-provider) and broke on every cross-provider
  concern.
- **Parallel tool calls** — ToolMiddleware that fans out to N
  concurrent dispatchers and aggregates. Engine sees one
  `ToolResult` per `ToolCall` either way.
- **Cost budgets** — event subscriber on `CostIncremented` plus a
  RequestMiddleware that aborts when the cap is exceeded.
- **Distributed sessions** — v0.2+ via `ws` and v0.3+ via
  `mcpserver` HTTP. Laptop client attaches to a server-hosted
  engine by URL + capability token; engine is unchanged.
- **Mobile / phone client** — a thin React Native app speaking the
  same `ws` protocol; no engine work.
- **IDE plugins** (VS Code, JetBrains, Neovim) — each is a `Client`
  implementation on top of `rest`/`ws`/`mcpserver`.
- **Collaborative editing** — already a capability the moment
  multi-attach lands; the only new code is a UI affordance showing
  "who's here" using existing `OriginatorID`.
- **Headless CI mode** — pre-provision a token file, attach a
  script client, drive a fixed prompt, capture exit from
  `TurnEnded`. No new transport.
- **Self-modifying skills** — a built-in tool that writes SKILL.md
  files plus an event subscriber that re-runs the skill registry
  scan. New skills go through the TOFU gate before activation.
- **Container-sandboxed execution** — ToolMiddleware that wraps
  Bash calls in `docker`/`podman`/`bwrap`/`sandbox-exec`.
- **Pi-style cheap-model delegation** — a `delegate` built-in tool
  that instantiates a **child `EngineRun`** with a cheaper
  `Provider`. **Scoped sync-only in v0.x**: the tool blocks the
  parent turn; child events are not surfaced to parent clients
  (the child has its own log + attach surface if observation is
  needed). A general parent/child `EngineGraph` model (cancellation
  propagation, event fan-in, multi-session ownership) is a v1+
  topic — calling out the limit now keeps `delegate` honest as a
  middleware-shaped extension.
- **Replay / time-travel debugging UI** — a `Client` implementation
  that drives from a session log instead of a live engine. Renders
  the same events; sends no commands.
- **Vision / audio** — `Provider` extension; canonical form already
  supports image content blocks. Tool middleware handles base64
  packaging.

---

## Slash commands

Slash commands are parsed at the **client**, not the engine. The
client recognizes a `/`-prefixed input, converts it to a typed
`Command`, and sends it. The engine never sees raw `/`-prefixed
strings as input.

The namespace convention is **pi-style**:

| Form | Meaning |
|---|---|
| `/foo`, `/help`, `/clear`, `/compact`, `/cost`, `/model`, `/profile`, `/resume`, `/quit` | Reserved for **built-in** commands shipped with the harness. |
| `/skills:name` | Invoke a registered skill by name (tier-2 disclosure activates). |
| `/agents:type` | Spawn a sub-agent of the given type. |
| `/profiles:name` | Switch the active profile. |
| `/sessions:action` | Session-management (`/sessions:list`, `/sessions:branch`, `/sessions:export`). |
| `/mcp:server` | MCP-server-scoped actions (`/mcp:kiln status`). |
| `/custom:foo`, `/super-skill:bar`, `/<your-namespace>:…` | **User- or plugin-registered** namespaces. Plugins claim a namespace via `Plugin.Register`; conflicts are rejected at boot. |

Discovery is via the control plane:

```
GET /v1/slash-commands     → list of {namespace, name, description, args_schema}
```

The TUI uses this to populate the command palette; external clients
(IDE plugins, web client) use the same endpoint.

Bare `/` (unprefixed) is **reserved for built-ins only**. This keeps
"what does this command do" predictable: if it's prefix-less, it's
shipped with the harness; if it's namespaced, the namespace tells
you where to look.

### Tab-completion contract

The TUI input layer (and any client implementing autocomplete)
honors this contract:

- `/sk<TAB>` → `/skills:` (prefix expansion)
- `/skills:gofa<TAB>` → `/skills:gofastr-ui` (or fuzzy-match list
  if multiple)
- `/<TAB>` (empty after slash) → list of all top-level namespaces
  + built-ins
- Skill / agent / profile listings come from `GET /v1/slash-commands`
  cached for the session; the cache invalidates on `SkillRegistryChanged`
  (emitted when TOFU acks a new skill or `/skills:reload` runs).

### User-defined aliases

`~/.config/gofastr/harness/aliases.toml` carries short forms:

```toml
[aliases]
gu = "/skills:gofastr-ui"
cb = "/skills:component-build"
verify = "/skills:verify-before-claim"
```

Aliases are themselves TOFU-tracked (a malicious alias is a
code-exec hazard — `evil = "/skills:exfiltrate"`). Aliases never
expand to built-in commands (no shadowing); conflicts at load time
are rejected with a clear error.

### Built-in commands shipped in v0.1

| Command | Effect |
|---|---|
| `/help` | List all commands grouped by namespace |
| `/clear` | Clear scrollback (does not affect log) |
| `/compact` | Trigger history compaction now |
| `/cost` | Detailed cost breakdown for the current session (input tokens, output tokens, cache hits, per-provider rates) |
| `/model <name>` | `SetModel` for the current session |
| `/profile` | Show active profile + skills/MCP/tools loaded |
| `/profiles:<name>` | Switch profile (see § Profiles → Mid-session profile switching) |
| `/sessions:list` | Table with `status · id · age · profile · model · turns · branch-of · attached-by · working-dir` |
| `/sessions:resume <id>` | Re-attach to a paused or detached session |
| `/sessions:pin <id>` | Opt out of TTL + idle-shutdown for a session |
| `/sessions:branch <id> [at <turn>]` | Branch from a `TurnEnded` boundary (default = latest) |
| `/sessions:export <id> --format jsonl\|markdown` | Export session content (post-redaction) |
| `/skills:<name>` | Invoke a registered skill |
| `/skills:reload` | Re-scan skill search paths; TOFU prompts for any change |
| `/agents:<type>` | Spawn a subagent |
| `/mcp:<server>` | Server-scoped action; `/mcp:<server> status`, `/mcp:<server> restart` |
| `/permissions` | Show active session-scoped allow rules |
| `/permissions:promote <rule>` | Promote a session-scoped rule to profile-level (v0.2) |
| `/health` | Status of every subsystem: engine, credential helper, MCP servers, control plane listeners, credstore, session DB |
| `/web` | Show the `--web` URL (with OSC 8 hyperlink); copy to clipboard via `pbcopy`/`xclip` |
| `/quit` | Two-step exit (see § TUI Ctrl-C semantics) |

---

## User-facing errors

The control protocol carries structured `Error` events with stable
`Reason` codes. Clients translate them to human strings; the
harness ships the following canonical strings, which TUI and web
both use verbatim. Reason codes are the wire contract; strings are
the user contract. Both are stable within a minor release.

| `Reason` | TUI / web string |
|---|---|
| `HandshakeRequired` | `Send /v1/handshake before any command. See § Protocol versioning.` |
| `TurnInProgress` | `Another client is sending input to this session (started by <originator> at <ts>). Wait, or send /cancel to interrupt.` |
| `PermissionDenied` | `Permission denied for <tool> by <client>. To allow: re-prompt and pick 'Allow once / argv-glob / tool / session'.` |
| `PermissionTimeout` | `Permission for <tool> not answered within <timeout>s. Tool call cancelled.` |
| `TokenExpired` | `Capability token expired at <exp>. Mint a new one: gofastr harness token --session <id> --identity-class <class>.` |
| `TokenRevoked` | `Token <jti> was revoked at <ts>. Mint a new one via /v1/auth/token.` |
| `MCPServerSHA256Mismatch` | `MCP server '<name>' refused to start: the binary at <path> changed. Expected sha256 <expected>, found <actual>. To approve the new binary, run /mcp:approve <name> or edit <profile_path> to update the pin.` |
| `MCPServerUnavailable` | `MCP server '<name>' is down after <attempts> restart attempts. Its tools are in degraded state. /mcp:restart <name> to retry, or check $XDG_STATE_HOME/gofastr/harness/log/.` |
| `HookHashChanged` | `<path> changed since you last approved it. Review the diff (TUI: 'v' opens the diff viewer). Re-ack to continue.` |
| `HookTimeout` | `Hook '<event>' command '<cmd>' exceeded its <timeout>s deadline. Killed.` |
| `RateLimited` | `Provider <name> rate-limited the request. Retrying in <retry-after>s… (Ctrl-C to cancel, /model to switch).` |
| `BashCancelledMidCommand` | `Cancelled mid-Bash. The command ran for <duration> and may have modified files. Files possibly affected: <list>. Review /sessions:diff <id> to see what changed.` |
| `NonInteractiveAckRefused` | `Refusing to silently approve an unseen <kind> in non-interactive mode. Run gofastr harness ack --all once interactively, or pre-populate approved.lock from CI.` |
| `HandshakeVersionMismatch` | `Client speaks protocol <client_ver>; server speaks <server_ver>. Upgrade <client_or_server>.` |

Plugin-introduced errors use `Reason: Custom:<namespace>:<code>`
with a `string` payload field the client surfaces verbatim.

Two of the remediation strings above reference `gofastr harness token`
and `gofastr harness ack` — specified subcommands that are not yet
wired into `cmd/gofastr` (which today dispatches only `mcp` and
`creds`).

---

## Logging (the harness's own logs)

Distinct from the session event log (which is per-conversation,
encrypted, redacted). The harness emits its own operational logs to:

- `~/.local/state/gofastr/harness/log/harness-YYYYMMDD.log` —
  rolling daily file, plaintext, structured (one JSON object per
  line: `{ts, level, component, msg, fields…}`).
- stderr when `--log-to-stderr` is set.

Components: `engine`, `multiplex`, `provider.<name>`,
`transport.<name>`, `mcpclient`, `mcpserver`, `skill`, `hook`,
`auth`. Default level `info`; per-component overrides via
`--log-level engine=debug,provider.copilot=trace`.

The raw-provider-request debug mode is a per-session toggle:
`harness debug --raw` enables full request/response dumps to a
session-scoped file at `~/.local/state/gofastr/harness/debug/<SessionID>.jsonl`.
Dumps are written **before** the redaction middleware on the way in
and **after** it on the way out, so the debug file contains
unredacted traffic — protected by `0600` mode and the same keychain
encryption as the session log.

---

## Things to read before touching specific areas

| Touching | Read first |
|---|---|
| `engine/` | This doc, top-to-bottom |
| `provider/` | This doc § Providers + the provider's API reference |
| `tool/` | This doc § Tool system + `framework/ARCHITECTURE.md` § Hooks |
| `mcpclient/` | The MCP wire protocol spec |
| `skill/` | https://agentskills.io/specification |
| `context/` | https://agents.md/ |
| `control/` | This doc § Control plane — especially auth, multi-client, Threat model |
| `control/multiplex/` | This doc § Multi-client semantics |
| `control/mcpserver/` | MCP spec **plus** this doc § MCP-server surface — note the renamed tool and identity-class rules |
| `control/auth/` | This doc § Threat model and § Authentication |
| `client/tui/` | This doc § TUI; `golang.org/x/term` package docs |
| `client/web/` | `core-ui/ARCHITECTURE.md` (the UI runtime is what the web client dogfoods) |
| `session/` | This doc § Persistence — particularly redaction and TTL |
| `profile/` | This doc § Profiles — particularly trust gates |
| `hook/` | This doc § Profiles — trust gates and `--allow-project-hooks` |

---

## End

This document is the harness contract — every system it describes is
implemented in `framework/harness/` and exercised by tests. There is
no separate roadmap; future capabilities live in this doc or they do
not exist.

Updates require a PR that references the section being changed.
Breaking changes follow § Protocol versioning & evolution →
Backward-compat policy.
