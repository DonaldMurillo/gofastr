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
descriptor**: a Go value (`ProcessModuleDescriptor`) whose fields are
authoritative at runtime. The running child only ever *cross-checks*
digests at handshake; it never supplies values. A mismatch on digest,
identity, or an extra grant is terminal (the module is quarantined to
`Failed`, never silently restarted). A generation bump is the way back:
`BumpGeneration`, a grant revoke or an artifact change re-arms a
`Failed` module, and the new child goes through the same digest checks.

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

- **Routes**: the HTTP routes, host-registered behind the existing module
  route gate. The child cannot add, rename, or reshape routes.
- **Tools**: the optional MCP tools the module declares (see below).
  Digests are byte-compared against `module.tool.list` at handshake.
- **RequestedGrants**: the verbatim `resource:verb` list the operator
  reviews at install. Effective grants = requested ∩ approved.
- **TrustTier**: selects the runner: `TrustTrusted` (crash isolation
  only, for dev or in-house modules) or `TrustUntrusted` (requires a
  probe-passing sandbox, fail-closed otherwise).
- **MigrationGroup**: the `#33` migration group the module owns.

## Install + operator approval

Installation is **out of band**: the operator places the approved artifact
on disk and hands the descriptor + the approved-grant subset to
`RegisterProcessModule`. There is no registry, marketplace, or dependency
solver. The v1 trust anchor is a **content-addressed digest an operator
explicitly approves after reviewing the verbatim `resource:verb` list**,
not a signature/PKI (no signing authority, key distribution, or revocation
is defined yet).

Process modules default to **disabled** at install, and a never-enabled
module's routes answer 404. The operator enables one via
`app.ProcessModules().Enable(ctx, "billing")`; `app.Modules().Enable`, the
`app_module_enable` / `app_module_disable` MCP tools and the supervisor all
drive the one stored state, and a toggle takes effect before the call
returns. Enabling after `App.Start` spawns the child. Disable, grant
revoke, and upgrade (artifact change) are all live and propagate across
replicas through the SQL-backed `ProcessModuleStore`.
`RevokeGrants(ctx, name, grants)` takes the named grants out of the
module's effective set, keeps the rest and bumps the generation; it
refuses an empty `grants`. The store writes it as a compare-and-swap on
the generation it read (`ProcessModuleStore.RemoveEffectiveGrants`), so
two revokes at once both stick. A request body over 1 MiB, or one whose
base64-encoded `module.http` frame would not fit the module's
`Limits.FrameBytes` (about three quarters of it), answers 413 rather than
reaching the child truncated or failing as a retryable 503.

Every boot calls `RegisterProcessModule` again, and so does every
replica sharing the store. It compares the descriptor with the stored
row (`ProcessModuleStore.InstallOrUpdate`, whose `InstallOutcome` it
reports in `RegisterResult.Outcome`):

- **Same artifact and grants** (order and repeats ignored): nothing is
  written, and the module keeps its enabled state and generation.
- **New artifact, same grants**: the digest is updated and the
  generation bumped, and the module stays enabled. A replica restarts
  its child only once its own descriptor names the new digest: during a
  rolling deploy, a replica still on the old build drains its child and
  holds the module in `InstalledDisabled` (its routes answer 503) until
  its deploy registers the new artifact.
- **Any grant change**, wider or narrower: the grants and digest are
  updated, the generation bumped, and the module **disabled** until an
  operator enables it again, with a Warn naming the previous and new
  grants (`RegisterResult.PreviousGrants`). A runtime `RevokeGrants`
  is not remembered across this: the descriptor's grants are written
  back, and the module stays disabled until someone re-enables it.

The routes reflect the stored enabled state from the first request after
boot. Registering one name twice in the same process is still refused
with `ErrModuleInstalled` and leaves the row alone.

## The trust boundary

- **Zero database credentials in the child.** The host brokers all data
  and runs all DDL. The child's reverse `host.*` calls (entity query/
  create/update/delete, search, event emit) are re-dispatched through the
  **same CRUD chokepoint** as live HTTP, so owner/tenant/permission +
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
- **Reverse calls are bounded both ways.** The host serves at most
  `Limits.Inflight` of a child's reverse calls at once and answers the
  overflow with an inflight-cap error. It also holds at most that many
  answers waiting behind a blocked write: a child that keeps sending
  calls but stops reading stdin faults its peer with
  `moduleproto.ErrStalled`, after which the peer serves nothing more.
  An answer stops counting when its write starts, so a child at its cap
  that reads every answer never trips it.
- **A protocol fault restarts the module.** When the host peer records a
  terminal fault (a malformed, over-cap or unsolicited frame, or a
  stalled transport), the supervisor kills the child and counts the exit
  as a crash: the module goes through `Crashed` → `Backoff` → `Starting`
  like any other crash, and `CircuitThreshold` faults inside
  `CircuitWindow` open its restart circuit. The fault cancels the
  context of every reverse call the host peer is serving, as `Close`
  and the end of the transport do. A module must read stdin for
  as long as it sends requests.

## The two-layer 404 / 503 gate

A disabled module is indistinguishable from uninstalled: its routes **404**
and its MCP tools are omitted + refused. An enabled-but-down module
(crashed, starting, draining, lease-failing) is a **retryable temporary
outage**: routes return `503 + Retry-After`, tools are listed but return a
retryable-unavailable error. This split is load-bearing: the router gate
can only 404, so the Ready layer lives in the proxy handler and the tool
handler. Upgrades exploit it: a module stays Enabled (no 404) but not-Ready
(`DrainingUpgrade`) while the old generation drains and the new one starts.
A disable or upgrade drain runs the same steps as the shutdown drain below,
under the same `DrainPerModule` budget, and closes the module's peer once
the `module.drain` call is done, which cancels its in-flight reverse
calls. A drain also cancels a spawn still starting for the old state: that
child is killed instead of installed, so a revoke or disable that lands
mid-start never leaves a second child serving under the old grants.

## Shutdown

`App.Shutdown` drains process modules after the HTTP server has drained
and before batteries stop, so a child's in-flight reverse calls still reach
the queue and the DB. Every child drains at once: it gets `module.drain`,
then EOF on stdin, and is killed when its budget runs out
(`SupervisorConfig.DrainPerModule`, 30 s by default, capped by the
Shutdown context's deadline). The budget counts from `module.drain`, and
the drain call waits at most half of it (2 s at most) for an answer. A
child that stopped reading stdin, so the drain frame cannot be written,
is still killed on time. However the child ends (it exits at EOF, is
killed at the deadline, or crashes), the host then kills what is left of
its process group on Unix, so a helper it started there does not outlive
it. The drain closes the supervisor first, so no
module restarts during or after shutdown, and a child still starting when
it closes is killed instead of installed. `ProcessModuleSupervisor.Close`
stops the loops and kills the children without the drain; after a drain it
does nothing.

## Sandbox trust tiers (the honest limit)

A bare subprocess is **not** a security boundary. `TrustUntrusted` requires
a `SandboxRunner` whose backend passes a P1–P7 conformance probe (distinct
OS principal, no inherited secret/fd, no network egress, filesystem
confinement, resource limits, no privilege re-escalation). **An untrusted
module with no probe-passing backend does not run**; there is no silent
downgrade to the trusted runner.

The backends are per-OS wrapper commands (Linux: `bwrap` + cgroup v2;
macOS: `sandbox-exec`; Windows: AppContainer + Job Object). Some probe
properties are **structurally unreachable on a stock host**: distinct-uid
on macOS, network-egress denial on Windows. So: **an untrusted module may
be unrunnable on stock macOS/Windows/some-Linux until the operator installs
a conforming backend or provisions the missing rule.** Fail-closed refusal
is the correct, honest enforcement, not a gap to paper over.

A probe passes only when it proves the property, never because a test
could not run. P3 looks for the planted fd by device and inode, so the
child's own descriptors do not count. P4 dials loopback and LAN listeners
the runner opened and reached itself before the spawn, and with no such
control it reports unreachable. P6 holds every fd and process open at
once and counts only `EMFILE` and `EAGAIN` as the cap (a missing binary
or a timeout is unreachable), and an allocating sub-child must be killed
or refused short of twice the memory limit. CPU throttling is not probed.

Probe output replayed to the operator (a `ProbeResult.Detail`, e.g.
inside the conformance report `NewSandboxRunner` embeds in
`ErrSandboxUnavailable`) is scrubbed of terminal control bytes — C0
except newline and tab, plus DEL — the same contract the codegen
extension path applies to child stderr, so a sandbox wrapper's or child's
output cannot drive the operator's terminal from inside an error string.

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
   the `REVOKE` on `public` is the real boundary: the role holds no
   privileges outside its own schema regardless of `search_path`.
2. **The DDL session authenticates AS `module_billing_role`** (a separate
   login role, member of nothing), not an elevated session that `SET ROLE`s
   down. Only then is `RESET ROLE` a no-op and `SET ROLE <an elevated role>`
   refused.
3. **The tracking table is schema-local** (`module_billing._migrations`),
   so the runner's advisory-lock + checksum-integrity + single-transaction
   atomicity are preserved with zero runner changes (`core/migrate` is fed,
   never forked).
4. **Distinct module names derive distinct schemas.** The schema and role
   are `module_<name>` for a name made of lowercase letters and digits.
   A name the identifier sanitizer would fold (`billing-1`, `billing_1`,
   `Widget`) carries a 12-hex digest of the original name
   (`module_billing_1_<digest>`), so two operator-approved modules can
   never share one schema behind one role, where the `REVOKE` fence
   would bound each to the other's objects.

The short-lived **migration coordinator** (`App.NewModuleMigrationCoordinator`)
loads approved SQL from the digest-verified artifact, validates it against
the group rules (every migration's group == the descriptor's group; default-
group migrations are rejected; duplicate `(group, version)` and digest
mismatches are rejected), runs a non-authoritative **lint** (flags anything
beyond plain `CREATE TABLE` / `ALTER TABLE ADD COLUMN` / `CREATE INDEX` for
review; the role is the real boundary, there is no SQL parse-allowlist),
then provisions the schema+role, runs `Up` under the advisory lock
authenticated as the role, and stamps `MigrationsAppliedAt` so the
supervisor lets the module reach Ready.

The stamp belongs to one artifact. When a replica boots a module whose
artifact digest differs from the stored one, `InstallOrUpdate` clears
`MigrationsAppliedAt` with the generation bump, so the new version waits
for its own migrations: a supervisor whose descriptor declares a
migration group drains its running child and holds the module in
`InstalledDisabled` until the coordinator runs again with the new
descriptor. A grants-only change keeps the stamp. `Apply` stamps only
while the row still names the descriptor's `ArtifactSHA256`, so a
coordinator run for the old artifact that finishes after the upgrade
returns an error without stamping (its DDL has already run).

```go
coord, _ := app.NewModuleMigrationCoordinator(
    framework.WithCoordinatorAdminDSN(postgresURL),
)
coord.Apply(ctx, desc, []framework.ApprovedMigration{
    {Version: 1, Name: "init", Up: "CREATE TABLE invoices (id int)", SHA256: "…"},
})
```

The coordinator runs `Up` over a DSN built from the admin DSN with the
module role's login in place of the admin's. Of the admin DSN's query
it keeps only the keys that say where the server is and how to reach
it: `host`, `hostaddr`, `port`, `dbname`, the `ssl*` settings,
`connect_timeout`, `target_session_attrs`, `load_balance_hosts`,
`application_name`, `fallback_application_name`, `krbsrvname` and
`krbspn`. Everything else is dropped: a `user`, `password`, `passfile`
or `service` would name a login, and `options`, `search_path` or any
other startup setting would override what provisioning set on the role.

**SQLite is not a third-party DDL boundary**: it has no roles/`GRANT`/
schemas. The coordinator rejects an **untrusted** module's migrations on
SQLite, loud (fail-closed). Trusted/dev-only modules may run on SQLite.
Group names do not make SQLite DDL safe; groups are bookkeeping.

**No auto table-drop.** Disable leaves schema + rows intact; uninstall
removes registration but drops nothing. A destructive
`DROP SCHEMA module_M CASCADE` is a separate, privileged control-plane
action, never an uninstall side effect.

## MCP tools

A module may declare AI-agent-callable **tools** in its
descriptor. The host, not the child, registers each tool into its
existing `core/mcp.Server` under a namespaced id:

```
module.<name>.<tool>
```

so two modules cannot collide and every call is attributable. At handshake
the host fetches `module.tool.list` and requires **byte-equality** with the
descriptor digests; a child that adds, renames, or reshapes a tool is
quarantined. A tool invocation forwards `module.tool.call` to the live
child through the **same capability broker** as `module.http`, but the
calling agent's credentials are not forwarded: the tool's reverse `host.*`
calls run as an anonymous caller within the module's grants, so
owner-scoped reads are refused by construction. Re-attaching the agent's
roles there would let a tool read across owners. A tool can do nothing an HTTP
route with the same grants couldn't; there is no separate tool-permission
vocabulary.

## UI rendering

A module owns *screen logic* only. It returns a bounded, declarative
`ui.node.v1` node tree, a closed, host-owned component enum with typed
scalar props, which the host validates, maps to design-system components,
renders, and hydrates. The module never emits raw HTML/CSS/JS or
`data-cui-*` attributes; action references resolve to installed routes,
which the host maps to the real runtime RPC URLs.

The closed validator lives in [`core-ui/uinodev1`](../../../core-ui/uinodev1)
(`uinodev1.Validate`): it enforces the whole-tree caps (depth ≈ 32, nodes ≈
500, per-prop strings ≈ 4 KiB), the closed component enum, typed scalar
props (no `id`/`class`/`style`/`data-*` passthrough: `data-cui-*` and `on*`
are *unrepresentable*, not merely denied), host-relative-only URL schemes,
and action_ref shape. A forged tree is whole-tree rejected.

The proxy renders a validated tree through
[`framework/uihost/uinoderender`](../../uihost/uinoderender): each component
maps to a `framework/ui` / `core-ui/html` primitive with the host assigning
every id, class, ARIA attribute, and `data-cui-rpc` URL; the module supplies
none. A node's `action_ref` resolves against the module's own declared
routes; a ref naming no declared route fails the render **closed** (a buffered
503), and any validation or render error is likewise fail-safe; the forged
or malformed content never reaches the wire. The gate test
`TestGate_UIContainment` proves both halves end to end: a clean tree renders
to real design-system markup (200 `text/html`), a forged tree is rejected
(503, no leaked attribute).

## Building a module

A module child is a plain Go binary that speaks moduleproto over stdio. It
depends only on [`core/moduleproto`](../../../core/moduleproto) plus the
standard library: no `framework/*`, no MCP, no DB driver. The canonical,
runnable example is
[`examples/processmodule-demo/main.go`](../../../examples/processmodule-demo/main.go),
which is also the child the go/no-go gate suite
(`framework/processmodule_gate_test.go`) drives end to end. Read it alongside
this section.

The shape is fixed:

1. Open a `moduleproto.Codec` over `os.Stdin`/`os.Stdout` and construct a
   `moduleproto.Peer` in the `RoleChild` role.
2. Register handlers for the host → module methods you serve:
   `module.handshake` (echo the host's expected `instance_id` +
   `desired_generation` + `surface_sha256`; a mismatch is terminal),
   `module.ready` (warmup gate), `module.health`, `module.http` (your
   routes), `module.drain`, and optionally `module.tool.list` /
   `module.tool.call` (`module.cancel` is built into the Peer; do not
   re-register it).
3. Call `peer.Start()`, then block until `peer.Done()` (clean EOF on
   stdin) or `peer.FatalDone()`, and exit non-zero on a fault. A faulted
   peer answers nothing more while its read loop keeps draining stdin, so a
   child that stays up leaves every host call to wait out its deadline;
   once it exits, the supervisor restarts the module.

### Cancelling an in-flight call

`module.cancel` aborts one inbound request's context. `CancelParams.RequestID`
is **the canonical decimal string of the inbound frame id** — the value the
host's `Call` assigned to the frame it is cancelling, formatted with no
padding or sign. Exactly one spelling is accepted: `"7"` cancels frame 7,
while `"07"`, `" 7"` and `"notes-7"` are silently ignored, so a
near-miss can never abort a neighbouring request.

The host sends it for both forwarded call kinds: a proxied `module.http`
request and an MCP `module.tool.call`, once the caller's context ends
(client gone or the module's call deadline passed).

Note that this is *not* `HTTPRequestParams.RequestID`, which is a
diagnostics-only `<module>-<counter>` label and cancels nothing. The two
fields were documented as the same value and were not, which is why
`module.cancel` never fired on the proxy path.

Hosts get the frame id from `Peer.CallWithID`, which is `Call` plus a
callback:

```go
raw, err := peer.CallWithID(ctx, moduleproto.MethodHTTP, params, func(frameID uint64) {
    go watchCancel(peer, frameID, ctx) // arm the watcher for THIS frame
})
```

The callback fires synchronously, after the pending entry is registered and
before the frame is written, so a watcher armed inside it cannot miss a
response. It must not block — the write is waiting on it. It is not called
when no id was allocated (the peer is closed, the method is empty, the
context has already ended, or the in-flight cap is reached). Plain `Call` is unchanged and still correct
wherever the caller has nothing to cancel.

Either call returns once its context ends, even while its request frame
is still waiting to be written to a peer that stopped reading. The write
carries on in the background and holds the call's in-flight slot until
it returns, so stuck writes never exceed the cap. A call whose context
had already ended writes nothing. `Peer.Notify` (how `module.cancel` is
sent) works the same way, with its own cap: at most `Limits.Inflight`
notification writes wait on a peer that stopped reading, and a `Notify`
past that returns `moduleproto.ErrInflightCap` without writing.

A call that gave up still expects its answer: the peer remembers the id
and drops the late response instead of faulting on it. It remembers up to
1,024 ids still owed an answer; an answer that arrives frees its slot, so
a long-running handler that ignores `module.cancel` keeps its slot however
many other calls are given up and answered meanwhile. A response naming an
id the peer never sent, or one already answered, is still a fault.

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
select {
case <-peer.Done():
case <-peer.FatalDone():
}
if err := peer.FatalError(); err != nil {
    fmt.Fprintln(os.Stderr, "peer fault:", err)
    os.Exit(1)
}
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
  no streaming in v1: the host buffers the whole response before committing
  headers, so a child that dies mid-call yields a buffered 503, never a
  truncated 200. A status outside 200–599 is answered the same 503.
- **The host owns the response headers.** The response is served on the
  host's origin, so only `Cache-Control`, `Content-Language`, `ETag`,
  `Expires`, `Last-Modified` and `Retry-After` cross from the child, and its
  `Vary` adds to the host's. Everything else stays the host's:
  `Set-Cookie`, the security headers, CORS, `Location` and `Refresh` are
  dropped. `Content-Type` is always the body kind's
  (`application/json`, `text/plain`, or `text/html` for a validated
  `ui.node.v1` tree), with `X-Content-Type-Options: nosniff`, so a `json` or
  `text` body never runs as markup.

To install a built child, compute the digest of its routes and tools
(`framework.ComputeSurfaceSHA256`) and tool digests
(`framework.ModuleToolDigest`) from the same routes and tools the child
serves, pin the executable's SHA-256, and `RegisterProcessModule` the
descriptor; the gate test's `demoDescriptor` helper shows the exact
bookkeeping.

## Common mistakes

- **Treating `search_path` as the isolation fence.** It is session-mutable
  and the publisher can `SET search_path TO public, module_M` freely. The
  fence is the `REVOKE`: the role holds no privileges outside its own
  schema. Claiming otherwise is the common, wrong take on fixed `search_path`.
- **Forgetting to run the migration coordinator.** A module with a
  declared migration group never reaches Ready until the coordinator stamps
  `MigrationsAppliedAt`; it looks "stuck disabled" with proxy 503s. The
  same holds after every artifact upgrade: the new artifact clears the
  stamp, so run the coordinator as part of the deploy.
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
