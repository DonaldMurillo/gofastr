# Process modules

A **process module** is a third-party extension that runs **out of
process**, isolated from the host by a purpose-built JSON-RPC-over-stdio
protocol (`core/moduleproto`), not MCP. The host supervises one child per
module per replica; a crash, hostile publisher, or bad upgrade in the child
can never take the host down or read its memory.

The value is operational independence: a process module can be
**installed, upgraded, crashed, and revoked without touching the host
binary**. The host owns every schema and every registration; the child
holds zero database credentials and zero signing keys. This doc is the
operator-facing contract. The full design rationale (transport, capability
model, sandbox, migration, lifecycle, UI) lives in the `#37` design note.

## What a process module is

A process module is defined by a **content-addressed, operator-approved
descriptor** — a Go value (`ProcessModuleDescriptor`) whose fields are
authoritative at runtime. The running child only ever *cross-checks*
digests at handshake; it never supplies values. A mismatch on digest,
identity, or an extra grant is terminal (the module is quarantined to
`Failed`, never silently restarted).

```go
app.RegisterProcessModule(framework.ProcessModuleDescriptor{
    Name:            "billing",
    Version:         "1.2.0",
    ArtifactPath:    "/opt/gofastr-modules/billing-1.2.0",
    ArtifactSHA256:  "ab12…",   // SHA-256 of the executable, verified before exec
    SurfaceSHA256:   "cd34…",   // digest of the canonical surface (routes+tools+grants)
    TrustTier:       framework.TrustUntrusted,
    Routes: []framework.RouteDeclaration{
        {ID: "invoice", Method: "GET", Path: "/billing/invoice/{id}"},
    },
    Tools: []framework.ToolDigest{
        {ID: "total", SHA256: "ef56…"}, // optional MCP tool surface
    },
    RequestedGrants: []access.Permission{"invoices:read"},
    MigrationGroup:  "billing",
}, framework.ApprovedGrants{"invoices:read"})
```

The fields:

- **Routes** — the HTTP routes, host-registered behind the existing module
  route gate. The child cannot add, rename, or reshape routes.
- **Tools** — the optional MCP tools the module declares (see below).
  Digests are byte-compared against `module.tool.list` at handshake.
- **RequestedGrants** — the verbatim `resource:verb` list the operator
  reviews at install. Effective grants = requested ∩ approved.
- **TrustTier** — selects the runner: `TrustedTrusted` (crash isolation
  only, for dev or in-house modules) or `TrustUntrusted` (requires a
  probe-passing sandbox, fail-closed otherwise).
- **MigrationGroup** — the `#33` migration group the module owns.

## Install + operator approval

Installation is **out of band**: the operator places the approved artifact
on disk and hands the descriptor + the approved-grant subset to
`RegisterProcessModule`. There is no registry, marketplace, or dependency
solver. The v1 trust anchor is a **content-addressed digest an operator
explicitly approves after reviewing the verbatim `resource:verb` list** —
not a signature/PKI (no signing authority, key distribution, or revocation
is defined yet).

Process modules default to **disabled** at install. The operator enables
one via `app.ProcessModules().Enable(ctx, "billing")`; disable, grant
revoke, and upgrade (artifact change) are all live and propagate across
replicas through the SQL-backed `ProcessModuleStore`.

## The trust boundary

- **Zero database credentials in the child.** The host brokers all data
  and runs all DDL. The child's reverse `host.*` calls (entity query/
  create/update/delete, search, event emit) are re-dispatched through the
  **same CRUD chokepoint** as live HTTP — owner/tenant/permission +
  token-scope re-run on the re-attached caller identity.
- **Capability model = module-grant ∩ caller-authority.** The required
  permission is derived from the trusted method + canonical host resource,
  never from a child-supplied string (the confused-deputy control).
- **`CrossOwnerRead` is non-grantable to a module.** A descriptor
  requesting it (or a wildcard broad enough to subsume it) is rejected at
  install; belt-and-suspenders, the broker strips it on both the
  module-grant and delegated-caller paths. A module never brokers data in
  a cross-owner/cross-tenant frame.
- **Delegation is an in-memory, replica-local handle**, not a signed
  token. The host mints it for an inbound call, the child echoes it on
  reverse calls, and the host re-attaches that request's caller context.
  An exfiltrated handle is meaningless on another replica.

## The two-layer 404 / 503 gate

A disabled module is indistinguishable from uninstalled: its routes **404**
and its MCP tools are omitted + refused. An enabled-but-down module
(crashed, starting, draining, lease-failing) is a **retryable temporary
outage**: routes return `503 + Retry-After`, tools are listed but return a
retryable-unavailable error. This split is load-bearing — the router gate
can only 404, so the Ready layer lives in the proxy handler and the tool
handler. Upgrades exploit it: a module stays Enabled (no 404) but not-Ready
(`DrainingUpgrade`) while the old generation drains and the new one starts.

## Sandbox trust tiers (the honest limit)

A bare subprocess is **not** a security boundary. `TrustUntrusted` requires
a `SandboxRunner` whose backend passes a P1–P7 conformance probe (distinct
OS principal, no inherited secret/fd, no network egress, filesystem
confinement, resource limits, no privilege re-escalation). **An untrusted
module with no probe-passing backend does not run** — there is no silent
downgrade to the trusted runner.

The backends are per-OS wrapper commands (Linux: `bwrap` + cgroup v2;
macOS: `sandbox-exec`; Windows: AppContainer + Job Object). Some probe
properties are **structurally unreachable on a stock host** — distinct-uid
on macOS, network-egress denial on Windows. So: **an untrusted module may
be unrunnable on stock macOS/Windows/some-Linux until the operator installs
a conforming backend or provisions the missing rule.** Fail-closed refusal
is the correct, honest enforcement, not a gap to paper over.

## Migrations (per-module schema + role)

The host runs all DDL under a restricted **per-module Postgres schema +
role**, keyed to the module's migration group:

```sql
CREATE SCHEMA IF NOT EXISTS module_billing;
CREATE ROLE module_billing_role LOGIN PASSWORD '…'
    NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
GRANT USAGE, CREATE ON SCHEMA module_billing TO module_billing_role;
ALTER ROLE module_billing_role SET search_path = module_billing;
REVOKE ALL ON SCHEMA public FROM module_billing_role;
```

Three load-bearing points:

1. **`search_path` is a convenience, NOT the fence.** It is session-mutable;
   the `REVOKE` on `public` is the real boundary — the role holds no
   privileges outside its own schema regardless of `search_path`.
2. **The DDL session authenticates AS `module_billing_role`** (a separate
   login role, member of nothing), not an elevated session that `SET ROLE`s
   down. Only then is `RESET ROLE` a no-op and `SET ROLE <an elevated role>`
   refused.
3. **The tracking table is schema-local** (`module_billing._migrations`),
   so the runner's advisory-lock + checksum-integrity + single-transaction
   atomicity are preserved with zero runner changes (`core/migrate` is fed,
   never forked).

The short-lived **migration coordinator** (`App.NewModuleMigrationCoordinator`)
loads approved SQL from the digest-verified artifact, validates it against
the group rules (every migration's group == the descriptor's group; default-
group migrations are rejected; duplicate `(group, version)` and digest
mismatches are rejected), runs a non-authoritative **lint** (flags anything
beyond plain `CREATE TABLE` / `ALTER TABLE ADD COLUMN` / `CREATE INDEX` for
review — the role is the real boundary, there is no SQL parse-allowlist),
then provisions the schema+role, runs `Up` under the advisory lock
authenticated as the role, and stamps `MigrationsAppliedAt` so the
supervisor lets the module reach Ready.

```go
coord, _ := app.NewModuleMigrationCoordinator(
    framework.WithCoordinatorAdminDSN(postgresURL),
)
coord.Apply(ctx, desc, []framework.ApprovedMigration{
    {Version: 1, Name: "init", Up: "CREATE TABLE invoices (id int)", SHA256: "…"},
})
```

**SQLite is not a third-party DDL boundary** — it has no roles/`GRANT`/
schemas. The coordinator rejects an **untrusted** module's migrations on
SQLite, loud (fail-closed). Trusted/dev-only modules may run on SQLite.
Group names do not make SQLite DDL safe; groups are bookkeeping.

**No auto table-drop.** Disable leaves schema + rows intact; uninstall
removes registration but drops nothing. A destructive
`DROP SCHEMA module_M CASCADE` is a separate, privileged control-plane
action, never an uninstall side effect.

## MCP tools

A module may declare AI-agent-callable **tools** in its
descriptor. The host — not the child — registers each tool into its
existing `core/mcp.Server` under a namespaced id:

```
module.<name>.<tool>
```

so two modules cannot collide and every call is attributable. At handshake
the host fetches `module.tool.list` and requires **byte-equality** with the
descriptor digests; a child that adds, renames, or reshapes a tool is
quarantined. A tool invocation forwards `module.tool.call` to the live
child through the **same capability broker** as `module.http` — the calling
agent's authority is resolved and delegated identically, and the tool's
reverse `host.*` calls are checked as module-grant ∩ caller-authority
(including the `CrossOwnerRead` carve-out). A tool can do nothing an HTTP
route with the same grants couldn't; there is no separate tool-permission
vocabulary.

## UI rendering

A module owns *screen logic* only. It returns a bounded, declarative
`ui.node.v1` node tree — a closed, host-owned component enum with typed
scalar props — which the host validates, maps to design-system components,
renders, and hydrates. The module never emits raw HTML/CSS/JS or
`data-fui-*` attributes; action references resolve to installed routes,
which the host maps to the real runtime RPC URLs.

The closed validator lives in [`core-ui/uinodev1`](../../core-ui/uinodev1)
(`uinodev1.Validate`): it enforces the whole-tree caps (depth ≈ 32, nodes ≈
500, per-prop strings ≈ 4 KiB), the closed component enum, typed scalar
props (no `id`/`class`/`style`/`data-*` passthrough — `data-fui-*` and `on*`
are *unrepresentable*, not merely denied), host-relative-only URL schemes,
and action_ref shape. A forged tree is whole-tree rejected.

The proxy renders a validated tree through
[`framework/uihost/uinoderender`](../../uihost/uinoderender): each component
maps to a `framework/ui` / `core-ui/html` primitive with the host assigning
every id, class, ARIA attribute, and `data-fui-rpc` URL — the module supplies
none. A node's `action_ref` resolves against the module's own declared
routes; a ref naming no declared route fails the render **closed** (a buffered
503), and any validation or render error is likewise fail-safe — the forged
or malformed content never reaches the wire. The gate test
`TestGate_UIContainment` proves both halves end to end: a clean tree renders
to real design-system markup (200 `text/html`), a forged tree is rejected
(503, no leaked attribute).

## Building a module

A module child is a plain Go binary that speaks moduleproto over stdio. It
depends only on [`core/moduleproto`](../../core/moduleproto) plus the
standard library — no `framework/*`, no MCP, no DB driver. The canonical,
runnable example is
[`examples/processmodule-demo/main.go`](../../examples/processmodule-demo/main.go),
which is also the child the go/no-go gate suite
(`framework/processmodule_gate_test.go`) drives end to end. Read it alongside
this section.

The shape is fixed:

1. Open a `moduleproto.Codec` over `os.Stdin`/`os.Stdout` and construct a
   `moduleproto.Peer` in the `RoleChild` role.
2. Register handlers for the host → module methods you serve:
   `module.handshake` (echo the host's expected `instance_id` +
   `desired_generation` + `surface_sha256` — a mismatch is terminal),
   `module.ready` (warmup gate), `module.health`, `module.http` (your
   routes), `module.drain`, and optionally `module.tool.list` /
   `module.tool.call` (`module.cancel` is built into the Peer — do not
   re-register it).
3. Call `peer.Start()`, then block on `<-peer.Done()` (clean EOF on stdin).

```go
codec, _ := moduleproto.NewCodec(os.Stdin, os.Stdout, moduleproto.DefaultMaxFrameBytes)
peer := moduleproto.NewPeer(codec, moduleproto.RoleChild)
peer.Handle(moduleproto.MethodHandshake, func(_ context.Context, p json.RawMessage) (any, error) {
    var hp moduleproto.HandshakeParams
    _ = json.Unmarshal(p, &hp)
    return moduleproto.HandshakeResult{
        Proto:          moduleproto.ProtoRange{Min: 1, Max: 1},
        Identity: moduleproto.Identity{
            Name: hp.Expected.Name, Version: hp.Expected.Version,
            InstanceID: hp.Expected.InstanceID,
            DesiredGeneration: hp.Expected.DesiredGeneration,
        },
        SurfaceSHA256: hp.Expected.SurfaceSHA256,
    }, nil
})
// …module.ready / module.health / module.http (by RouteID) / module.drain…
peer.Start()
<-peer.Done()
```

Three contracts the child MUST honor (the host enforces them; violation is
terminal `Failed` or a per-call 503):

- **The descriptor is authoritative.** Routes, tools, requested grants, and
  the digest fields come from the operator-approved
  `ProcessModuleDescriptor`, never from the child. `module.handshake` only
  *echoes* `surface_sha256`; `module.tool.list` must be byte-equal to the
  descriptor's tool digests. A child that adds a route/tool or reshapes a
  tool is quarantined.
- **Reverse data access is brokered.** The child holds zero DB credentials.
  To read host data it issues `host.entity.query` (and create/update/delete/
  search/event) via `peer.Call`, echoing the `Caller` block the host attached
  to the inbound `module.http`/`module.tool.call` so the host re-attaches the
  caller's context. The host derives the required permission from the trusted
  method + entity name (never a child-supplied string), checks it against
  `module-grant ∩ caller-authority`, and re-dispatches through the same CRUD
  chokepoint as live HTTP. The demo's `/items` route is a worked example.
- **Bodies are fully buffered.** A `module.http` response is one
  `HTTPResponseResult` with a `json`, `text`, or `ui.node.v1` body. There is
  no streaming in v1 — the host buffers the whole response before committing
  headers, so a child that dies mid-call yields a buffered 503, never a
  truncated 200.

To install a built child, compute the digest of its routes and tools
(`framework.ComputeSurfaceSHA256`) and tool digests
(`framework.ModuleToolDigest`) from the same routes and tools the child
serves, pin the executable's SHA-256, and `RegisterProcessModule` the
descriptor; the gate test's `demoDescriptor` helper shows the exact
bookkeeping.

## Common mistakes

- **Treating `search_path` as the isolation fence.** It is session-mutable
  and the publisher can `SET search_path TO public, module_M` freely. The
  fence is the `REVOKE` — the role holds no privileges outside its own
  schema. Claiming otherwise is the common, wrong take on fixed `search_path`.
- **Forgetting to run the migration coordinator.** A module with a
  declared migration group never reaches Ready until the coordinator stamps
  `MigrationsAppliedAt`; it looks "stuck disabled" with proxy 503s.
- **Expecting an untrusted module to run on stock macOS / Windows / some
  Linux.** Sandbox backends need a probe-passing conforming wrapper; until
  the operator provisions one, fail-closed refusal is correct, not a bug.
- **Assuming group names make SQLite DDL safe.** SQLite has no roles/
  `GRANT`/schemas; an untrusted module's migrations are rejected on SQLite
  loud (fail-closed). Groups are bookkeeping, not a boundary.
- **Putting the Ready check in the router gate.** The router gate can only
  404; Ready lives in the proxy handler (503 + Retry-After) and the tool
  handler (retryable), so an enabled-but-down module is retryable, not
  uninstalled-looking.
- **Expecting disable / uninstall to drop tables.** Disable leaves schema
  + rows intact; uninstall removes registration only. `DROP SCHEMA …
  CASCADE` is a separate privileged action.
