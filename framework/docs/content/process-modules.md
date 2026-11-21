# Process modules (sandboxed sidecars)

Process modules run untrusted or polyglot extensions as isolated OS child processes communicating with GoFastr over a typed protocol instead of sharing the Go server's address space.

## When to use a process module

Use an ordinary Go `Plugin`, `Battery`, or `Module` for trusted first-party code. Reach for a process module when:
- You want hard memory/crash isolation (a panic or OOM in the module cannot kill the Go host).
- You are running a non-Go binary or third-party extension that should not see the host's database credentials, environment secrets, or full filesystem.

## Lifecycle and isolation

When registered on `framework.App`, the host manages the child process lifecycle:
- Starts the binary with an allowlisted environment (no ambient `GOFASTR_SECRET`, `DATABASE_URL`, or cloud credentials).
- Places each child in its own OS process group so `App.Shutdown` drains and terminates the entire child tree cleanly without leaving orphaned background processes.
- Enforces per-call deadlines, message size caps, and restart backoff on crash.
- Exposes module routes/tools behind the same module enable/disable gate as in-process modules (see [Modules](modules.md)).

## macOS sandbox posture

On macOS, `TrustUntrusted` process modules run inside a `sandbox-exec` profile, and registration **refuses to proceed (panics with `*UntrustedNoSandboxError`)** if the sandbox is unavailable or fails conformance — it never silently falls back to an unsandboxed child.

Understand the explicit boundaries of the macOS profile (`framework/processmodule_sandbox_darwin.go`):
- Denies writes outside the module's granted paths and denies network egress unless explicitly permitted.
- **Known read gaps:** to let macOS binaries and runtimes execute, the profile allows reads under standard system and temp paths (`/usr`, `/bin`, `/sbin`, `/System`, `/Library`, `/dev`, `/etc`, `/private/etc`, `/tmp`, `/private/tmp`, `/var/folders`, `/private/var/folders`, `/var/tmp`, `/private/var/tmp`). As a result, it does **not** block **P2 (host secrets in temporary directories)** or **P5 (host files such as `/etc/passwd`)** — do not rely on the macOS profile to hide secrets placed in `/tmp` or readable files under `/etc`.
- Treat OS sandboxing as defense-in-depth alongside narrow protocol permissions, strict input validation, and minimal environment variables — for hostile multi-tenant code execution, combine with container/VM isolation (Linux namespaces/seccomp or microVMs).

## Common mistakes

- **Inheriting `os.Environ()` into a child process:** leaks `DATABASE_URL`, `GOFASTR_SECRET`, and API keys to the module. Pass only explicit allowlisted config values.
- **Skipping process-group teardown:** killing only the direct child PID leaves grandchild shells running after shutdown or crash.
- **Giving a process module unrestricted host filesystem mounts:** grant only a dedicated scratch/data directory.
- **Assuming `sandbox-exec` alone replaces container isolation for untrusted multi-tenant workloads:** use Linux containers/microVMs for hostile multi-tenant execution.
