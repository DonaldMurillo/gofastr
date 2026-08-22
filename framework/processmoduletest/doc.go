// Package processmoduletest holds the process-module supervisor and gate
// e2e suites that used to live in the framework root test package.
//
// Why it is its own package (issue #208): the CI race gate runs the
// framework root under -race, and TestSupervisor_* was observed flaking
// under -race while passing repeatedly in isolation. These suites spawn
// real child processes against deliberately compressed supervisor timings
// (50 ms polls, 500 ms leases, 3 s spawn deadlines) that do not reliably
// absorb race instrumentation, so they live here where the race gate does
// not reach them.
//
// What is NOT here, and why: every test that reaches unexported supervisor
// internals stayed in the framework root, because exporting test seams
// would be public API that exists only for tests. That set is
// TestSupervisor_KillMidCallBuffered503 (Slot's child handle),
// TestSupervisor_StoreUnreachableDrains (the store's raw db),
// TestSupervisor_RemoteToggleCrossReplica and
// TestGate_ConvergenceAndRevoke (moduleSlot construction), plus the whole
// sandbox selection suite (SandboxRunner's unexported report fields) and
// the sandbox conformance probes (unexported probe-child dispatch).
//
// This directory holds no production code; the suites drive the framework
// root package through its exported API only.
package processmoduletest
