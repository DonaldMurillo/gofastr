package framework

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// ProcessState is the supervisor's per-module state (design §8). The state
// machine is per module, per replica. External HTTP surface sees only the
// 404-vs-503 split (Enabled → 404 when disabled; enabled-but-not-Ready →
// 503 + Retry-After); operator introspection distinguishes the rest.
type ProcessState int

const (
	// StateAbsent: the module is not registered with this supervisor.
	StateAbsent ProcessState = iota

	// StateInstalledDisabled: registered, desired disabled. The route gate
	// 404s; no child is running.
	StateInstalledDisabled

	// StateStarting: spawn in progress (child exec'd, not yet
	// handshaked). Proxy returns 503 + Retry-After.
	StateStarting

	// StateHandshaking: handshake in progress. Proxy returns 503.
	StateHandshaking

	// StateReady: child is live and ready; proxy forwards module.http.
	StateReady

	// StateCrashed: unexpected exit observed while desired-enabled. The
	// supervisor will charge the restart circuit and transition to
	// Backoff. Proxy returns 503.
	StateCrashed

	// StateBackoff: waiting for the backoff delay before the next restart
	// attempt. Proxy returns 503.
	StateBackoff

	// StateDrainingDisable: desired disabled mid-call; the supervisor is
	// finishing in-flight leases (deadline-bounded) before killing the
	// child and transitioning to InstalledDisabled. The route gate still
	// 404s new requests (the cache flipped first).
	StateDrainingDisable

	// StateDrainingUpgrade: desired generation advanced mid-call; the
	// supervisor is draining the OLD child before spawning the new one.
	// Stays Enabled (no 404); proxy returns 503 + Retry-After.
	StateDrainingUpgrade

	// StateFailed: terminal. An integrity fault (SHA mismatch, handshake
	// digest mismatch, protocol negotiation failure), a restart cannot
	// fix a bad artifact. The module stays 404 (disabled) or 503 (enabled)
	// until the operator explicitly bumps generation with a corrected
	// artifact. The restart circuit is NOT charged.
	StateFailed
)

// String makes ProcessState log-friendly and operator-readable.
func (s ProcessState) String() string {
	switch s {
	case StateAbsent:
		return "Absent"
	case StateInstalledDisabled:
		return "InstalledDisabled"
	case StateStarting:
		return "Starting"
	case StateHandshaking:
		return "Handshaking"
	case StateReady:
		return "Ready"
	case StateCrashed:
		return "Crashed"
	case StateBackoff:
		return "Backoff"
	case StateDrainingDisable:
		return "DrainingDisable"
	case StateDrainingUpgrade:
		return "DrainingUpgrade"
	case StateFailed:
		return "Failed"
	default:
		return fmt.Sprintf("ProcessState(%d)", int(s))
	}
}

// SupervisorConfig tunes the supervisor. Every field has a v1 default; tests
// override the durations and the clock to avoid real sleeps and to compress
// the 60s circuit window.
type SupervisorConfig struct {
	// Store is the durable coordination substrate. Required.
	Store ProcessModuleStore

	// Runner spawns children for TrustTrusted modules. Defaults to
	// [TrustedProcessRunner] when nil (tests usually inject a fake).
	Runner Runner

	// Sandbox spawns TrustUntrusted modules. Nil is the fail-closed
	// state: every TrustUntrusted descriptor fails Register with
	// [UntrustedNoSandboxError] (design §6, never a silent downgrade
	// to Runner). A non-nil SandboxRunner whose backend does not pass
	// the P1–P7 conformance probe is equivalent to nil. SelectRunner
	// consults Sandbox.Conforms() at Register.
	Sandbox *SandboxRunner

	// Broker is the reverse-request capability broker. Defaults to
	// [NopBroker] (deny-all) when nil, the real broker lands in a later
	// wave.
	Broker ReverseBroker

	// ToolRegistrar installs a module's MCP tool surface into the host
	// *mcp.Server after handshake verifies it (design §5.1 / §4.7 step 5).
	// Nil means the supervisor still verifies digests at handshake (a
	// mismatch quarantines the module) but does not register tools into
	// any MCP server, tests that do not exercise the tool surface leave
	// it nil. The host wiring sets it to a [ModuleToolRegistry].
	ToolRegistrar ToolRegistrar

	// ToolListTimeout bounds the module.tool.list round-trip at handshake.
	// Default 2s (sub-budget of SpawnDeadline). The deadline on the call
	// is min(remaining spawn budget, this).
	ToolListTimeout time.Duration

	// ReplicaID identifies this supervisor in heartbeat rows. Defaults to
	// a random hex string when empty.
	ReplicaID string

	// SpawnDeadline bounds the spawn sequence (spawn + handshake + ready
	// poll). Default 5s (design §8).
	SpawnDeadline time.Duration

	// PollInterval is the periodic reconcile tick (cross-replica
	// generation convergence upper bound). Default 2s (design §8 ~2s
	// target).
	PollInterval time.Duration

	// HeartbeatInterval is how often the supervisor writes a heartbeat
	// row for each Ready module. Default 1s.
	HeartbeatInterval time.Duration

	// LeaseTTL is the fail-closed bound: if the store cannot be re-read
	// for this long, children are drained and routes 503/404. Default 5s
	// (≈ 3× HeartbeatInterval, design §8). This INVERTS in-process
	// handleRemoteToggle's fail-open, documented at
	// [SQLProcessModuleStore].
	LeaseTTL time.Duration

	// DrainPerModule bounds a single child's graceful drain. The shared
	// shutdown budget is min(DrainPerModule, remaining shared budget).
	// Default 30s (design §8).
	DrainPerModule time.Duration

	// BackoffMin / BackoffMax bound the restart backoff (design §8:
	// 250ms → 30s + jitter).
	BackoffMin time.Duration
	BackoffMax time.Duration

	// CircuitThreshold is the crash count in CircuitWindow that opens the
	// circuit (design §8: 5). When open, the supervisor stops restarting
	// until a generation bump resets it.
	CircuitThreshold int

	// CircuitWindow is the rolling window for crash counts (design §8:
	// 60s).
	CircuitWindow time.Duration

	// Now is the clock the supervisor reads. Defaults to [time.Now] when
	// nil, tests inject a tunable clock so the 60s circuit window does
	// not need real sleeps.
	Now func() time.Time

	// Logf receives diagnostic lines (state transitions, crash
	// classifications). Optional; nil = silent.
	Logf func(format string, args ...any)
}

// applyDefaults fills zero fields with the v1 defaults.
func (c *SupervisorConfig) applyDefaults() {
	if c.Runner == nil {
		c.Runner = &TrustedProcessRunner{}
	}
	if c.Broker == nil {
		c.Broker = NopBroker{}
	}
	if c.ReplicaID == "" {
		c.ReplicaID = randomReplicaID()
	}
	if c.SpawnDeadline <= 0 {
		c.SpawnDeadline = 5 * time.Second
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 1 * time.Second
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = 5 * time.Second
	}
	if c.DrainPerModule <= 0 {
		c.DrainPerModule = 30 * time.Second
	}
	if c.BackoffMin <= 0 {
		c.BackoffMin = 250 * time.Millisecond
	}
	if c.BackoffMax <= 0 {
		c.BackoffMax = 30 * time.Second
	}
	if c.CircuitThreshold <= 0 {
		c.CircuitThreshold = 5
	}
	if c.CircuitWindow <= 0 {
		c.CircuitWindow = 60 * time.Second
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}
	if c.ToolListTimeout <= 0 {
		c.ToolListTimeout = 2 * time.Second
	}
}

// ProcessModuleSupervisor owns the per-module-per-replica state machine
// (design §8). One supervisor per App; multiple supervisors (one per
// replica) share the same [ProcessModuleStore] for cross-replica
// convergence.
type ProcessModuleSupervisor struct {
	cfg     SupervisorConfig
	store   ProcessModuleStore
	runner  Runner
	sandbox *SandboxRunner
	broker  ReverseBroker
	tools   ToolRegistrar

	now  func() time.Time
	logf func(string, ...any)

	mu      sync.Mutex
	slots   map[string]*moduleSlot
	closed  atomic.Bool
	closeCh chan struct{}
	wg      sync.WaitGroup // supervise + heartbeat loops

	// loopsStarted is set by StartLoops. Before it, Enable records the
	// desired state and leaves the loop to StartLoops; after it, Enable
	// starts a loop for a module registered late.
	loopsStarted atomic.Bool

	// lease is the fail-closed bound (design §8). Last successful
	// GetDesired refresh per module. Guarded by leaseMu (separate from mu
	// so the proxy path does not contend on slot construction).
	leaseMu sync.Mutex
	lease   map[string]time.Time
}

// moduleSlot is the per-module state. The supervise goroutine owns writes
// to every field except where noted; the proxy path takes RLock to read a
// consistent snapshot.
type moduleSlot struct {
	name   string
	desc   ProcessModuleDescriptor
	sup    *ProcessModuleSupervisor
	runner Runner

	// wake signals the supervise loop to reconcile NOW (in addition to
	// the periodic tick). Buffered cap 1 so the first wake after a long
	// spawn is not lost.
	wake chan struct{}
	done chan struct{} // closed when supervise loop exits

	mu sync.RWMutex // guards the fields below

	state        ProcessState
	desiredGen   uint64
	enabled      bool
	instanceID   string
	child        RunningChild
	peer         *moduleproto.Peer
	codec        *moduleproto.Codec
	stderr       *moduleproto.RingSink
	restarts     []time.Time // crash timestamps within CircuitWindow
	circuitOpen  bool
	lastExit     string
	leaseFailing bool // fail-closed: lease expired, drain in progress
	loopStarted  bool // the supervise + heartbeat goroutines are running

	// liveGrants is the grant set the running child's broker view was
	// built with. reverseClosed is that child's serve gate: once set, its
	// host peer refuses every host.* reverse call. A disable drain sets
	// it, and so does an upgrade drain whose new grant set no longer
	// covers liveGrants (a revoke): the draining child keeps the time to
	// finish its work but loses the revoked capability at once. Upgrade
	// and shutdown drains that take nothing away leave it open.
	liveGrants    []access.Permission
	reverseClosed *atomic.Bool

	// spawnSeq names the one spawn allowed to install into this slot.
	// spawnAsync bumps it and keeps the spawn's cancel in spawnCancel; a
	// drain bumps it again and cancels, so a spawn still starting when
	// its generation is superseded installs nothing, writes no state, and
	// never runs beside its replacement.
	spawnSeq    uint64
	spawnCancel context.CancelFunc
}

// snapshot is an RLock'd read of the slot's mutable state, for proxy /
// introspection.
type snapshot struct {
	state        ProcessState
	desiredGen   uint64
	enabled      bool
	instanceID   string
	peer         *moduleproto.Peer
	restartCnt   int
	circuitOpen  bool
	lastExit     string
	leaseFailing bool
}

func (s *moduleSlot) snapshot() snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return snapshot{
		state:        s.state,
		desiredGen:   s.desiredGen,
		enabled:      s.enabled,
		instanceID:   s.instanceID,
		peer:         s.peer,
		restartCnt:   len(s.restarts),
		circuitOpen:  s.circuitOpen,
		lastExit:     s.lastExit,
		leaseFailing: s.leaseFailing,
	}
}

// NewProcessModuleSupervisor constructs a supervisor. It does NOT call
// EnsureSchema, that is the app wiring's job (so the schema-creation
// error surfaces where the operator expects it). The supervisor launches
// its per-module loops lazily as modules are registered.
func NewProcessModuleSupervisor(cfg SupervisorConfig) (*ProcessModuleSupervisor, error) {
	if cfg.Store == nil {
		return nil, errors.New("processmodule: NewProcessModuleSupervisor: nil Store")
	}
	cfg.applyDefaults()
	s := &ProcessModuleSupervisor{
		cfg:     cfg,
		store:   cfg.Store,
		runner:  cfg.Runner,
		sandbox: cfg.Sandbox,
		broker:  cfg.Broker,
		tools:   cfg.ToolRegistrar,
		now:     cfg.Now,
		logf:    cfg.Logf,
		slots:   make(map[string]*moduleSlot),
		lease:   make(map[string]time.Time),
		closeCh: make(chan struct{}),
	}
	return s, nil
}

// Closed reports whether Close has been called.
func (s *ProcessModuleSupervisor) Closed() bool { return s.closed.Load() }

// Close stops every supervise loop, waits for the loops to exit, and kills
// every child still running. Idempotent, and a no-op after [Drain], which
// closes the supervisor itself. It does NOT close the store (the store is
// shared across the app and closed elsewhere).
func (s *ProcessModuleSupervisor) Close(ctx context.Context) error {
	slots := s.stopLoops(ctx)
	// Best-effort kill any child that survived.
	for _, sl := range slots {
		sl.mu.Lock()
		if sl.child != nil {
			_ = sl.child.Kill()
		}
		sl.mu.Unlock()
	}
	return nil
}

// stopLoops closes the supervisor, stops every supervise and heartbeat
// loop, and waits for them to exit or ctx to end. A spawn still in flight
// sees the closed supervisor and kills its child (spawnOnce). It returns
// the slots, or nil when the supervisor was already closed.
func (s *ProcessModuleSupervisor) stopLoops(ctx context.Context) []*moduleSlot {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(s.closeCh)
	s.mu.Lock()
	slots := make([]*moduleSlot, 0, len(s.slots))
	for _, sl := range s.slots {
		slots = append(slots, sl)
	}
	s.mu.Unlock()
	for _, sl := range slots {
		close(sl.done)
	}
	waitCh := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(waitCh)
	}()
	select {
	case <-waitCh:
	case <-ctx.Done():
	}
	return slots
}

// Drain is the shutdown entry point: App.Shutdown calls it after the HTTP
// drain and before batteries stop (design §8), so a child's in-flight
// reverse calls still reach them. It closes the supervisor first, so no
// loop restarts a drained child, then drains every child concurrently
// (a single shared budget across the app, so a per-child drain applied
// serially to N children would blow it). Each child gets
// min(DrainPerModule, remaining shared budget). A second call, or a call
// after [Close], does nothing.
func (s *ProcessModuleSupervisor) Drain(ctx context.Context) error {
	slots := s.stopLoops(ctx)

	deadline, ok := ctx.Deadline()
	remaining := s.cfg.DrainPerModule
	if ok {
		// min(perModule, remaining shared budget)
		if r := time.Until(deadline); r < remaining {
			remaining = r
		}
	}
	if remaining < 0 {
		remaining = 0
	}

	var wg sync.WaitGroup
	for _, sl := range slots {
		wg.Go(func() {
			s.drainOneForShutdown(sl, remaining)
		})
	}
	wg.Wait()
	return nil
}

// drainOneForShutdown performs the §4.6 teardown for one child during
// shutdown: send module.drain, close stdin (EOF), and Kill + Wait once
// the budget, counted from the drain call, runs out. It is best-effort
// and never panics.
func (s *ProcessModuleSupervisor) drainOneForShutdown(sl *moduleSlot, budget time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			s.logf("processmodule: drain %s panic: %v\n%s", sl.name, textsafe.Recovered(r), debug.Stack())
		}
	}()
	sl.mu.Lock()
	child := sl.child
	peer := sl.peer
	sl.mu.Unlock()
	if child == nil {
		return
	}
	// The peer stays open: the child's in-flight reverse calls still
	// reach the queue and the DB until it exits.
	s.drainChild(sl.name, child, peer, budget, "shutdown", false)
	// drainChild returns once the child has exited. Settle the slot here,
	// as transitionDrain does, rather than leave it to the exit watcher:
	// App.Shutdown stops the batteries next, and the watcher may not have
	// run yet. The watcher then finds no child and leaves the slot alone.
	// The supervisor is closed, so no replacement can be in the slot: a
	// spawn that finishes now kills its own child (spawnOnce).
	sl.mu.Lock()
	sl.child = nil
	sl.peer = nil
	sl.codec = nil
	sl.lastExit = exitLabel(nil, true)
	sl.mu.Unlock()
}

// drainChild is the §4.6 teardown both drains share: module.drain, EOF on
// stdin, and Kill + Wait once budget, counted from the drain call, runs
// out. closePeer closes the peer once the drain call is done, which
// cancels the child's in-flight reverse calls.
//
// The budget runs from the drain call, not from CloseStdin: the
// module.drain frame write cannot be abandoned, and a child that stopped
// reading stdin holds it on a full pipe. Only the kill, which takes the
// pipe's reader away, fails that write. The drain call gets at most half
// the budget, so a child that never answers it still has the rest to
// exit at EOF.
func (s *ProcessModuleSupervisor) drainChild(name string, child RunningChild, peer *moduleproto.Peer, budget time.Duration, reason string, closePeer bool) {
	waitCh := make(chan error, 1)
	go func() { waitCh <- child.Wait() }()
	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	if peer != nil {
		// Best-effort module.drain notification (child may be mid-crash).
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			defer func() {
				if r := recover(); r != nil {
					s.logf("processmodule: drain %s panic: %v\n%s", name, textsafe.Recovered(r), debug.Stack())
				}
			}()
			drainCtx, cancel := context.WithTimeout(context.Background(), minDur(budget/2, 2*time.Second))
			defer cancel()
			_, _ = peer.Call(drainCtx, moduleproto.MethodDrain, moduleproto.DrainParams{
				Reason:     reason,
				DeadlineMs: int(budget.Milliseconds()),
			})
		}()
		select {
		case <-drained:
		case <-deadline.C:
			_ = child.Kill()
			<-waitCh
			if closePeer {
				_ = peer.Close()
			}
			return
		}
		if closePeer {
			_ = peer.Close()
		}
	}
	_ = child.CloseStdin()
	select {
	case <-waitCh:
	case <-deadline.C:
		_ = child.Kill()
		<-waitCh
	}
}

// wake sends a reconcile signal to the named module's supervise loop. No-op
// if the module is not registered or the supervisor is closed.
func (s *ProcessModuleSupervisor) wake(name string) {
	s.mu.Lock()
	sl := s.slots[name]
	s.mu.Unlock()
	if sl == nil {
		return
	}
	select {
	case sl.wake <- struct{}{}:
	default:
	}
}

// randomReplicaID mints a hex suffix for the default replica id.
func randomReplicaID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "replica-" + hex.EncodeToString(b[:])
}

// mintInstanceID mints a per-spawn liveness nonce (design §4.7 step 1). It
// is random and never persisted, its only job is to reject a stale or
// duplicate connection from a prior spawn of the same module.
func mintInstanceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// minDur returns the smaller of a, b (time.Duration's math.Min is float-based).
func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// errClosedSup is returned when an operation hits a closed supervisor.
var errClosedSup = errors.New("processmodule: supervisor closed")

// Compile-time assertions on the broker interface shape.
var _ ReverseBroker = NopBroker{}

// RegisterResult is the return value of [ProcessModuleSupervisor.Register]:
// the validated descriptor, the computed effective grants, the stored
// desired-generation, and what the store did with an existing row.
type RegisterResult struct {
	Descriptor      ProcessModuleDescriptor
	EffectiveGrants []access.Permission
	Generation      uint64
	// Outcome is the store's [InstallOutcome]: created on a first
	// install, otherwise how an existing row was reconciled.
	Outcome InstallOutcome
	// PreviousGrants is the stored grant set a grants change replaced;
	// nil otherwise.
	PreviousGrants []access.Permission
}

// Register validates the descriptor, installs or reconciles its
// desired-state row in the store, and registers the module for
// supervision. A row from an earlier boot or another replica is
// reconciled by [ProcessModuleStore.InstallOrUpdate], so an app restarts
// against a persistent database. Registering the same name twice on one
// supervisor returns [ErrModuleInstalled] and leaves the row alone. The
// supervisor's per-module loop is started lazily by
// [ProcessModuleSupervisor.StartLoops] or implicitly by
// [ProcessModuleSupervisor.Enable].
//
// The caller is responsible for route registration (attributing routes to
// the module name so the existing route gate 404s when disabled). The
// supervisor exposes [ProcessModuleSupervisor.Routes] for the host wiring
// to iterate when it calls the router.
func (s *ProcessModuleSupervisor) Register(ctx context.Context, d ProcessModuleDescriptor, approved ApprovedGrants) (RegisterResult, error) {
	if s.closed.Load() {
		return RegisterResult{}, errClosedSup
	}
	effective, err := ValidateProcessModuleDescriptor(d, approved)
	if err != nil {
		return RegisterResult{}, err
	}
	// Trust-tier → runner selection (design §6 decision C, fail-closed).
	// This is the inversion of the wave-2a site that unconditionally
	// errored on TrustUntrusted: now the supervisor consults
	// [SelectRunner], which maps trusted → s.runner and untrusted →
	// s.sandbox (or ErrSandboxUnavailable when the host has no
	// probe-passing backend). ErrSandboxUnavailable is wrapped as
	// [UntrustedNoSandboxError] so the existing error contract holds.
	s.mu.Lock()
	sandbox := s.sandbox
	s.mu.Unlock()
	selectedRunner, err := SelectRunner(d.TrustTier, s.runner, sandbox)
	if err != nil {
		return RegisterResult{}, &UntrustedNoSandboxError{Module: d.Name, cause: err}
	}
	// Create the slot and claim the name before touching the store: a
	// second Register of a name in this process is a wiring mistake and
	// must not rewrite the row the first one supervises. The supervise
	// loop is NOT started yet, it starts (lazily) when StartLoops is
	// called by the app wiring, or the first time the module is Enabled.
	slot := &moduleSlot{
		name:   d.Name,
		desc:   d,
		sup:    s,
		runner: selectedRunner,
		wake:   make(chan struct{}, 1),
		done:   make(chan struct{}),
		state:  StateInstalledDisabled,
	}
	s.mu.Lock()
	if s.slots[d.Name] != nil {
		s.mu.Unlock()
		return RegisterResult{}, fmt.Errorf("processmodule: %w: %q", ErrModuleInstalled, d.Name)
	}
	s.slots[d.Name] = slot
	s.mu.Unlock()
	// Install the desired-state row at generation 1, or reconcile the
	// row an earlier boot or another replica wrote: same descriptor keeps
	// its enabled flag and generation, a new artifact keeps it enabled,
	// a grant change disables it (see [InstallOutcome]).
	inst, err := s.store.InstallOrUpdate(ctx, DesiredState{
		Module:            d.Name,
		DesiredGeneration: 1,
		Enabled:           false,
		ArtifactSHA256:    d.ArtifactSHA256,
		EffectiveGrants:   effective,
	})
	if err != nil {
		s.mu.Lock()
		delete(s.slots, d.Name)
		s.mu.Unlock()
		return RegisterResult{}, err
	}
	// Seed the cached flag from the row, so the route gate reads the
	// stored state from the first request rather than a false default
	// until the first reconcile.
	slot.mu.Lock()
	slot.enabled = inst.Row.Enabled
	slot.mu.Unlock()
	// A new row holds the descriptor's grants; an existing one holds them
	// less its stored revokes.
	effective = inst.Row.EffectiveGrants
	if inst.Outcome == InstallGrantsChanged {
		s.logf("processmodule: %q grants changed (%v -> %v): disabled until an operator re-enables it", d.Name, inst.PreviousGrants, effective)
	}
	s.logf("processmodule: registered %q %s gen=%d grants=%d", d.Name, inst.Outcome, inst.Row.DesiredGeneration, len(effective))
	return RegisterResult{
		Descriptor:      d,
		EffectiveGrants: effective,
		Generation:      inst.Row.DesiredGeneration,
		Outcome:         inst.Outcome,
		PreviousGrants:  inst.PreviousGrants,
	}, nil
}

// setSandbox installs the runner TrustUntrusted modules spawn under. The
// App calls it before the first untrusted Register (the supervisor is
// built lazily, often for a trusted module first). Slots registered
// earlier keep the runner they selected.
func (s *ProcessModuleSupervisor) setSandbox(r *SandboxRunner) {
	s.mu.Lock()
	s.sandbox = r
	s.mu.Unlock()
}

// UntrustedNoSandboxError is returned by Register when a descriptor with
// [TrustUntrusted] is registered and no probe-passing sandbox backend is
// available on this host (design §6 fail-closed). The module stays
// un-Registered and never reaches Ready. The wrapped [cause] explains why
// (no backend configured, backend unavailable, or probe failure) and is
// exposed via [errors.Unwrap] so callers can [errors.Is] against
// [ErrSandboxUnavailable].
type UntrustedNoSandboxError struct {
	Module string
	// cause is the underlying selection failure (typically
	// [ErrSandboxUnavailable], possibly wrapping the probe report).
	// Exposed via Unwrap so errors.Is(err, ErrSandboxUnavailable)
	// works.
	cause error
}

// Unwrap allows errors.Is(err, ErrSandboxUnavailable) and errors.As on
// the wrapped probe/construction failure.
func (e *UntrustedNoSandboxError) Unwrap() error { return e.cause }

func (e *UntrustedNoSandboxError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("processmodule: %q declares TrustUntrusted but no probe-passing SandboxRunner backend is available (design §6 fail-closed; never a silent downgrade): %v", e.Module, e.cause)
	}
	return fmt.Sprintf("processmodule: %q declares TrustUntrusted but no probe-passing SandboxRunner backend is available (design §6 fail-closed; never a silent downgrade)", e.Module)
}

// Slot returns the live slot for name, or nil if unregistered. Used by the
// host wiring to attribute routes / consult state.
func (s *ProcessModuleSupervisor) Slot(name string) *moduleSlot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.slots[name]
}

// StartLoops launches every registered module's supervise loop and the
// global periodic poll. Idempotent; called by the app wiring at Start. After
// StartLoops, a subsequently-Registered module has its loop started by
// Enable (which itself wakes the slot).
func (s *ProcessModuleSupervisor) StartLoops() {
	s.loopsStarted.Store(true)
	s.mu.Lock()
	slots := make([]*moduleSlot, 0, len(s.slots))
	for _, sl := range s.slots {
		slots = append(slots, sl)
	}
	s.mu.Unlock()
	for _, sl := range slots {
		s.startLoop(sl)
	}
}

// startLoop spawns the supervise + heartbeat goroutines for one slot,
// once: the slot's loopStarted flag makes a second call (StartLoops run
// twice, or racing Enable) a no-op.
func (s *ProcessModuleSupervisor) startLoop(sl *moduleSlot) {
	sl.mu.Lock()
	started := sl.loopStarted
	sl.loopStarted = true
	sl.mu.Unlock()
	if started {
		return
	}
	s.wg.Add(2)
	go sl.supervise()
	go sl.heartbeat()
}

// Enable marks a module desired-enabled in the store and wakes its
// supervise loop. Returns [ErrNoDesiredRow] (wrapped) if the module is not
// registered.
func (s *ProcessModuleSupervisor) Enable(ctx context.Context, name string) error {
	if err := s.setDesiredEnabled(ctx, name, true); err != nil {
		return err
	}
	s.startLoopIfAbsent(name)
	s.wake(name)
	return nil
}

// Disable marks a module desired-disabled in the store and wakes its
// supervise loop (which drains the child).
func (s *ProcessModuleSupervisor) Disable(ctx context.Context, name string) error {
	if err := s.setDesiredEnabled(ctx, name, false); err != nil {
		return err
	}
	s.wake(name)
	return nil
}

// setDesiredEnabled writes the desired state to the store and, once the
// write lands, to the slot's cached copy. The route gate and the
// module-tool gate read that copy ([ModuleManager.Enabled],
// [ModuleToolRegistry.GateForModule]), so a toggle closes or opens both
// before this returns rather than on the next reconcile; reconcile then
// keeps it in step with the store for toggles made on other replicas.
func (s *ProcessModuleSupervisor) setDesiredEnabled(ctx context.Context, name string, enabled bool) error {
	if s.closed.Load() {
		return errClosedSup
	}
	sl := s.Slot(name)
	if sl == nil {
		return fmt.Errorf("processmodule: %w: %q", ErrNoDesiredRow, name)
	}
	if err := s.store.SetEnabled(ctx, name, enabled); err != nil {
		return err
	}
	sl.mu.Lock()
	sl.enabled = enabled
	sl.mu.Unlock()
	return nil
}

// RevokeGrants takes grants out of the module's effective grants, keeping
// the rest, AND bumps generation; every replica's next reconcile observes
// the higher generation and restarts the child with the narrowed view
// (design §5 "binding + revocation"). It refuses an empty grants. Returns
// the new generation.
func (s *ProcessModuleSupervisor) RevokeGrants(ctx context.Context, name string, grants []access.Permission) (uint64, error) {
	if s.closed.Load() {
		return 0, errClosedSup
	}
	if s.Slot(name) == nil {
		return 0, fmt.Errorf("processmodule: %w: %q", ErrNoDesiredRow, name)
	}
	gen, err := s.store.RemoveEffectiveGrants(ctx, name, grants)
	if err != nil {
		return 0, err
	}
	s.wake(name)
	return gen, nil
}

// BumpGeneration is the upgrade lever (design §8): increments the desired
// generation and wakes every replica's reconcile loop. The supervisor will
// drain the old child and spawn a fresh one for the new generation.
func (s *ProcessModuleSupervisor) BumpGeneration(ctx context.Context, name string) (uint64, error) {
	if s.closed.Load() {
		return 0, errClosedSup
	}
	if s.Slot(name) == nil {
		return 0, fmt.Errorf("processmodule: %w: %q", ErrNoDesiredRow, name)
	}
	gen, err := s.store.BumpGeneration(ctx, name)
	if err != nil {
		return 0, err
	}
	s.wake(name)
	return gen, nil
}

// startLoopIfAbsent starts the supervise loop for name if it has not been
// started yet. Used by Enable so a module Registered after StartLoops
// comes online. Before StartLoops it does nothing: StartLoops starts every
// slot, and an app does not spawn children before App.Start.
func (s *ProcessModuleSupervisor) startLoopIfAbsent(name string) {
	if !s.loopsStarted.Load() {
		return
	}
	s.mu.Lock()
	sl := s.slots[name]
	s.mu.Unlock()
	if sl == nil {
		return
	}
	s.startLoop(sl)
}

// Reconcile pokes the named module's supervise loop. It is the single
// entry point the three reconcile sources funnel into (local enable/
// disable, handleRemoteToggle after its store re-read, periodic generation
// poll, design §8). Safe to call for unknown names (no-op).
func (s *ProcessModuleSupervisor) Reconcile(name string) {
	s.wake(name)
}

// ----- supervise loop -----

// supervise is the per-module state-machine loop. It exits when done is
// closed (Close / shutdown) or the supervisor is closed.
func (sl *moduleSlot) supervise() {
	defer sl.sup.wg.Done()
	for {
		select {
		case <-sl.done:
			return
		case <-sl.sup.closeCh:
			return
		case <-sl.wake:
		case <-time.After(sl.sup.cfg.PollInterval):
		}
		sl.reconcile()
	}
}

// heartbeat writes a heartbeat row every HeartbeatInterval while the slot
// is Ready (or draining). Stale rows are read-side TTL'd out by
// [SQLProcessModuleStore.LiveReplicas]; on shutdown the supervisor calls
// DeleteHeartbeat.
func (sl *moduleSlot) heartbeat() {
	defer sl.sup.wg.Done()
	t := time.NewTicker(sl.sup.cfg.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-sl.done:
			return
		case <-sl.sup.closeCh:
			return
		case <-t.C:
		}
		sl.mu.RLock()
		gen := sl.desiredGen
		state := sl.state
		sl.mu.RUnlock()
		if state == StateAbsent {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		sl.recordHeartbeatSafely(ctx, gen, state)
		cancel()
	}
}

// recordHeartbeatSafely writes one store heartbeat under a recover guard:
// the store is a swappable implementation running on the heartbeat
// goroutine, which has no per-request net — a panicking store surfaces in
// the supervisor log instead of killing the process.
func (sl *moduleSlot) recordHeartbeatSafely(ctx context.Context, gen uint64, state ProcessState) {
	defer func() {
		if r := recover(); r != nil {
			sl.sup.logf("processmodule: %s heartbeat panic: %v\n%s", sl.name, textsafe.Recovered(r), debug.Stack())
		}
	}()
	_ = sl.sup.store.RecordHeartbeat(ctx, sl.name, sl.sup.cfg.ReplicaID, gen, state.String())
}

// reconcile drives one pass of the state machine. It is the only place
// state transitions happen (apart from the proxy path which is read-only).
func (sl *moduleSlot) reconcile() {
	defer func() {
		if r := recover(); r != nil {
			sl.sup.logf("processmodule: reconcile %s panic: %v\n%s", sl.name, textsafe.Recovered(r), debug.Stack())
		}
	}()
	desired, err := sl.refreshDesired()
	if err != nil {
		// Lease enforcement: if the store has been unreadable past
		// LeaseTTL, fail CLOSED (drain children). This is the deliberate
		// inversion of handleRemoteToggle's fail-open, documented at
		// [SQLProcessModuleStore].
		if errors.Is(err, errLeaseExpired) {
			sl.handleLeaseExpired()
			return
		}
		// A transient read error (store hiccup) is logged; the lease
		// timer continues to age. The next reconcile may succeed.
		sl.sup.logf("processmodule: reconcile %s store read: %v", sl.name, err)
		return
	}

	sl.mu.Lock()
	// Update desired-derived fields.
	oldGen := sl.desiredGen
	genAdvanced := desired.DesiredGeneration > oldGen
	sl.desiredGen = desired.DesiredGeneration
	sl.enabled = desired.Enabled
	state := sl.state
	childLive := sl.child != nil
	if sl.leaseFailing {
		// The lease is healthy again: refreshDesired succeeded, so the
		// fail-closed drain is over. Clear the flag so the proxy/tools
		// gates and Info() stop reporting the module as failing; the
		// childless-Ready case below re-arms the spawn path. Without
		// this clear, leaseFailing has no write site besides `= true`
		// and one GetDesired outage past LeaseTTL 503s the module
		// forever (the drain is an availability guard, not a one-way
		// kill).
		sl.leaseFailing = false
		sl.sup.logf("processmodule: %s lease recovered: re-arming", sl.name)
	}
	sl.mu.Unlock()

	// Generation advanced ⇒ upgrade path (drain old, spawn new). Reset
	// the circuit: a generation bump is the operator's recovery lever
	// (design §8).
	if genAdvanced && oldGen != 0 {
		sl.resetCircuit()
	}

	// Decide action.
	switch {
	case !desired.Enabled:
		// Desired disabled. Drain any running child.
		if state == StateReady || state == StateStarting ||
			state == StateHandshaking || state == StateCrashed ||
			state == StateBackoff {
			sl.transitionDrain(StateDrainingDisable, desired.DesiredGeneration, nil)
		} else if state == StateDrainingDisable || state == StateInstalledDisabled ||
			state == StateAbsent || state == StateFailed {
			// Already drained or never started.
			sl.setState(StateInstalledDisabled)
		}
	case desired.Enabled && migrationsPending(sl.desc, desired):
		// Migrations pending (design §7 / §8): the module declares a
		// migration group whose DDL the coordinator has not yet applied
		// (MigrationsAppliedAt is nil). Refuse Ready, do NOT spawn. Hold
		// in InstalledDisabled so the proxy returns 503 and module tools
		// return retryable-unavailable until the operator runs the
		// coordinator, which stamps MigrationsAppliedAt and (via a
		// generation bump) re-arms reconcile. A running child is drained
		// first: the stamp clears under a Ready module when another
		// replica boots a new artifact, and the child must not keep
		// serving against the schema it no longer matches. setState
		// no-ops when already InstalledDisabled, so this does not spam
		// the periodic poll.
		if state == StateReady || state == StateStarting ||
			state == StateHandshaking || state == StateCrashed ||
			state == StateBackoff {
			sl.transitionDrain(StateDrainingDisable, desired.DesiredGeneration, nil)
		} else if state != StateInstalledDisabled {
			sl.setState(StateInstalledDisabled)
		}
	case desired.Enabled && otherArtifact(sl.desc, desired):
		// Another replica registered a different artifact: the row's
		// generation and grants are that executable's, and this replica's
		// descriptor names the old one. Drain and hold, as for pending
		// migrations, until this replica's own deploy registers the
		// artifact the row names. The log fires on the transition only.
		if state == StateReady || state == StateStarting ||
			state == StateHandshaking || state == StateCrashed ||
			state == StateBackoff {
			sl.sup.logf("processmodule: %s row names artifact %s, this replica runs %s: holding", sl.name, shortHash(desired.ArtifactSHA256), shortHash(sl.desc.ArtifactSHA256))
			sl.transitionDrain(StateDrainingDisable, desired.DesiredGeneration, nil)
		} else if state != StateInstalledDisabled {
			sl.setState(StateInstalledDisabled)
		}
	case desired.Enabled && (state == StateInstalledDisabled || state == StateAbsent):
		// Desired enabled, currently down. Spawn.
		sl.spawnAsync(desired)
	case desired.Enabled && genAdvanced &&
		(state == StateReady || state == StateStarting || state == StateHandshaking):
		// Upgrade: drain then spawn.
		sl.transitionDrain(StateDrainingUpgrade, desired.DesiredGeneration, desired.EffectiveGrants)
	case desired.Enabled && state == StateReady && !childLive:
		// Recovery from a lease-expired drain: handleLeaseExpired nils
		// the child but keeps state=Ready, so the plain Ready arm below
		// would treat the drained slot as healthy forever. The lease is
		// healthy again (refreshDesired succeeded and cleared
		// leaseFailing above): re-arm the spawn path with a fresh
		// instance. StateReady implies a live child everywhere else
		// (spawnOnce sets Ready only alongside installing the child),
		// so a childless Ready slot is uniquely this recovery case.
		sl.spawnAsync(desired)
	case desired.Enabled && state == StateCrashed:
		// Crashed: charge circuit, decide restart vs Failed.
		sl.handleCrashed(desired)
	case desired.Enabled && state == StateBackoff:
		// Backoff elapsed: try again.
		sl.spawnAsync(desired)
	case desired.Enabled && state == StateFailed:
		// Terminal until the generation advances, the operator's recovery
		// lever (design §8): the bump re-arms the slot with a fresh spawn,
		// which verifies the artifact and handshake digests again, so an
		// unfixed integrity fault lands back in Failed.
		if genAdvanced {
			sl.spawnAsync(desired)
		}
	case desired.Enabled && state == StateReady:
		// Healthy; nothing to do.
	default:
		// Spawn in progress; nothing to do.
	}
}

// refreshDesired reads the desired state from the store and manages the
// lease. On success it refreshes the lease timestamp. On store error it
// leaves the lease timestamp alone; if now-lastRefresh > LeaseTTL it
// returns errLeaseExpired so reconcile drains the children.
func (sl *moduleSlot) refreshDesired() (DesiredState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	d, err := sl.getDesiredSafely(ctx)
	if err == nil {
		sl.sup.leaseMu.Lock()
		sl.sup.lease[sl.name] = sl.sup.now()
		sl.sup.leaseMu.Unlock()
		return d, nil
	}
	// Store error. Check lease.
	sl.sup.leaseMu.Lock()
	last, ok := sl.sup.lease[sl.name]
	sl.sup.leaseMu.Unlock()
	if !ok {
		// Never read successfully; treat as expired immediately so we
		// never serve an unaudited module.
		return DesiredState{}, errLeaseExpired
	}
	if sl.sup.now().Sub(last) > sl.sup.cfg.LeaseTTL {
		return DesiredState{}, errLeaseExpired
	}
	return DesiredState{}, err
}

// getDesiredSafely reads desired state under a recover guard: the store
// is a swappable implementation on the reconcile path, and a panic is
// reported as a store error so refreshDesired's existing lease logic
// decides fail-closed drain versus retry — the same decision a plain
// store error gets.
func (sl *moduleSlot) getDesiredSafely(ctx context.Context) (d DesiredState, err error) {
	defer func() {
		if r := recover(); r != nil {
			d, err = DesiredState{}, fmt.Errorf("store GetDesired panic: %v\n%s", textsafe.Recovered(r), debug.Stack())
		}
	}()
	return sl.sup.store.GetDesired(ctx, sl.name)
}

// errLeaseExpired is the sentinel refreshDesired returns to drive the
// fail-closed path. Distinct from a transient store error.
var errLeaseExpired = errors.New("processmodule: state lease expired")

// handleLeaseExpired drains every running child of this slot and marks it
// fail-closed. The route gate (Enabled cache) is left as-is; the proxy
// handler returns 503 because state != Ready. The module stays in this
// state until the lease can be refreshed (which re-enters reconcile).
func (sl *moduleSlot) handleLeaseExpired() {
	sl.mu.Lock()
	sl.leaseFailing = true
	child := sl.child
	peer := sl.peer
	state := sl.state
	sl.mu.Unlock()
	sl.sup.logf("processmodule: %s lease expired: draining (fail-closed)", sl.name)
	if child == nil {
		return
	}
	// Quick drain, no module.drain notification (the child may be fine,
	// but we cannot trust the desired state). Kill after a short grace.
	if peer != nil {
		_ = peer.Close()
	}
	_ = child.CloseStdin()
	waitCh := make(chan error, 1)
	go func() { waitCh <- child.Wait() }()
	select {
	case <-waitCh:
	case <-time.After(sl.sup.cfg.BackoffMin):
		_ = child.Kill()
		<-waitCh
	}
	sl.mu.Lock()
	sl.child = nil
	sl.peer = nil
	sl.codec = nil
	sl.state = state // keep state (e.g. Ready); proxy will see not-Ready via leaseFailing
	sl.lastExit = "lease-expired-drain"
	sl.mu.Unlock()
}

// setState is the single state-writer (under slot.mu). It logs the
// transition for diagnostics.
func (sl *moduleSlot) setState(next ProcessState) {
	sl.mu.Lock()
	prev := sl.state
	sl.state = next
	sl.mu.Unlock()
	if prev != next {
		sl.sup.logf("processmodule: %s %s → %s", sl.name, prev, next)
	}
}

// transitionDrain begins a graceful drain of the running child. It runs
// synchronously inside reconcile (acceptable: the proxy has already
// snapshot the peer for in-flight calls; new proxies see state != Ready and
// return 503). On completion it transitions to targetAfterDrain.
func (sl *moduleSlot) transitionDrain(drainState ProcessState, gen uint64, next []access.Permission) {
	sl.setState(drainState)
	sl.mu.Lock()
	// A spawn still starting belongs to the state being drained: cancel
	// it before finishDrain may start the replacement.
	sl.supersedeSpawnLocked()
	child := sl.child
	peer := sl.peer
	closed, held := sl.reverseClosed, sl.liveGrants
	if closed != nil && (drainState == StateDrainingDisable || !grantsCover(next, held)) {
		closed.Store(true)
	}
	sl.mu.Unlock()
	if child == nil {
		// Nothing running.
		sl.finishDrain(drainState, gen)
		return
	}
	// reconcile is blocked in this drain, so a revoke or disable stored
	// while it runs would reach the child only after it exits: watch for
	// one and close the gate the moment it lands.
	stopWatch := sl.watchDrainRevoke(closed, held)
	// Notify the child; it may finish in-flight work and exit cleanly.
	sl.sup.drainChild(sl.name, child, peer, sl.sup.cfg.DrainPerModule, drainReason(drainState), true)
	stopWatch()
	sl.mu.Lock()
	sl.child = nil
	sl.peer = nil
	sl.codec = nil
	sl.lastExit = drainState.String()
	sl.mu.Unlock()
	sl.finishDrain(drainState, gen)
}

// watchDrainRevoke re-reads the module's desired state every poll interval
// while a drain that left the serve gate open runs, and closes the gate
// once the row is disabled or its grants stop covering held. The returned
// stop ends the watch and waits for it. A read error leaves the gate as it
// is: the drain is already bounded by DrainPerModule, and the lease logic
// on the next reconcile decides a failing store.
func (sl *moduleSlot) watchDrainRevoke(closed *atomic.Bool, held []access.Permission) (stop func()) {
	if closed == nil || closed.Load() {
		return func() {}
	}
	interval := sl.sup.cfg.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	done, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			d, err := sl.getDesiredSafely(ctx)
			cancel()
			if err == nil && (!d.Enabled || !grantsCover(d.EffectiveGrants, held)) {
				closed.Store(true)
				sl.sup.logf("processmodule: %s revoked or disabled mid-drain: reverse calls closed", sl.name)
				return
			}
		}
	}()
	return func() { close(done); <-finished }
}

// grantsCover reports whether every grant in held is still granted by
// next. A grant next widens (x:read to x:*) stays covered; one it drops or
// narrows does not.
func grantsCover(next, held []access.Permission) bool {
	for _, g := range held {
		if !access.ScopeMatch(next, g) {
			return false
		}
	}
	return true
}

// newHostPeer builds the host side of a child's connection, with the
// serve gate a revoke or disable drain closes.
func (sl *moduleSlot) newHostPeer(codec *moduleproto.Codec) (*moduleproto.Peer, *atomic.Bool) {
	closed := new(atomic.Bool)
	return moduleproto.NewPeer(codec, moduleproto.RoleHost,
		moduleproto.WithMaxInflight(sl.maxInflight()),
		moduleproto.WithMaxServeInflight(sl.maxInflight()),
		moduleproto.WithServeGate(reverseGate(closed)),
	), closed
}

// reverseGate is the host peer's serve gate: once closed is set it refuses
// every host.* reverse call with CodeCapabilityDenied. Protocol methods
// (module.*) still pass, so the drain itself completes.
func reverseGate(closed *atomic.Bool) func(string) *moduleproto.Error {
	return func(method string) *moduleproto.Error {
		if closed.Load() && strings.HasPrefix(method, "host.") {
			return &moduleproto.Error{Code: moduleproto.CodeCapabilityDenied, Message: "moduleproto broker: module is draining after a revoke or disable"}
		}
		return nil
	}
}

// finishDrain transitions to the post-drain state. On an upgrade drain it
// re-reads the AUTHORITATIVE desired state from the store before respawning:
// a revoke/upgrade bumped the generation precisely because the effective
// grant set (or the enabled flag) changed, so the new child must be built
// from the store's current EffectiveGrants, never the descriptor's original
// RequestedGrants, which would keep serving a just-revoked capability across
// the respawn. If the store cannot be read, it fails closed (stays disabled)
// rather than respawn a possibly-revoked module, the same posture as the
// state lease.
func (sl *moduleSlot) finishDrain(drainState ProcessState, gen uint64) {
	switch drainState {
	case StateDrainingDisable:
		sl.setState(StateInstalledDisabled)
	case StateDrainingUpgrade:
		desired, err := sl.refreshDesired()
		if err != nil {
			sl.sup.logf("processmodule: %s upgrade-drain desired re-read failed (%v): staying disabled (fail-closed)", sl.name, err)
			sl.setState(StateInstalledDisabled)
			return
		}
		// Preserve the generation we converged to if the store read lags it
		// (the poll that triggered this drain saw >= gen).
		if desired.DesiredGeneration < gen {
			desired.DesiredGeneration = gen
		}
		// The stamp may have cleared while the old child drained (a new
		// artifact's migrations are pending): hold, as reconcile does.
		if desired.Enabled && !migrationsPending(sl.desc, desired) && !otherArtifact(sl.desc, desired) {
			sl.spawnAsync(desired)
		} else {
			sl.setState(StateInstalledDisabled)
		}
	}
}

// drainReason maps the drain state to a [moduleproto.DrainParams.Reason] string.
func drainReason(s ProcessState) string {
	switch s {
	case StateDrainingDisable:
		return "disable"
	case StateDrainingUpgrade:
		return "upgrade"
	default:
		return "shutdown"
	}
}

// ----- spawn -----

// spawnAsync runs the §4.7 startup sequence in a goroutine. It updates
// state to Starting → Handshaking → Ready (or Crashed / Failed) on
// completion. The proxy consults state under RLock and returns 503 while
// not Ready.
func (sl *moduleSlot) spawnAsync(desired DesiredState) {
	ctx, cancel := context.WithCancel(context.Background())
	sl.mu.Lock()
	sl.supersedeSpawnLocked()
	seq := sl.spawnSeq
	sl.spawnCancel = cancel
	sl.mu.Unlock()
	sl.setState(StateStarting)
	go func() {
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				sl.sup.logf("processmodule: spawn %s panic: %v\n%s", sl.name, textsafe.Recovered(r), debug.Stack())
				sl.failSpawn(seq, "spawn panic: "+textsafe.Recovered(r), false)
			}
		}()
		err := sl.spawnOnce(ctx, seq, desired)
		if err == nil {
			// spawnOnce already set state = Ready and started the
			// exit-watcher goroutine.
			sl.mu.Lock()
			if sl.spawnSeq == seq {
				sl.spawnCancel = nil
			}
			sl.mu.Unlock()
			return
		}
		// Integrity faults → terminal Failed; other errors → Crashed.
		if !sl.failSpawn(seq, err.Error(), isIntegrityFault(err)) {
			// A drain or a newer spawn took the slot over: its state is
			// theirs to write, not this spawn's failure.
			sl.sup.logf("processmodule: %s superseded spawn ended: %v", sl.name, err)
		}
	}()
}

// failSpawn records spawn seq's failure, Failed when terminal and Crashed
// otherwise, only while seq is still the slot's current spawn. The check
// and the write share one critical section: a newer spawn that takes the
// slot between them and reaches Ready would otherwise be marked Crashed,
// and the restart that follows would start a child beside the live one.
// It reports whether it wrote.
func (sl *moduleSlot) failSpawn(seq uint64, reason string, terminal bool) bool {
	sl.mu.Lock()
	if sl.spawnSeq != seq {
		sl.mu.Unlock()
		return false
	}
	sl.spawnCancel = nil
	if terminal {
		sl.markFailedLocked(reason)
	} else {
		sl.markCrashedLocked(reason)
	}
	sl.mu.Unlock()
	if terminal {
		sl.reportFailed(reason)
	} else {
		sl.reportCrashed(reason)
	}
	return true
}

// supersedeSpawnLocked retires the spawn in flight, if any: it cancels
// that spawn's context and moves spawnSeq past it, so the spawn installs
// nothing when it returns. sl.mu must be held.
func (sl *moduleSlot) supersedeSpawnLocked() {
	if sl.spawnCancel != nil {
		sl.spawnCancel()
		sl.spawnCancel = nil
	}
	sl.spawnSeq++
}

// errSpawnSuperseded is spawnOnce's abort when a drain or a newer spawn
// took the slot over while this one was starting.
var errSpawnSuperseded = errors.New("spawn aborted: superseded by a drain or a newer spawn")

// spawnOnce runs the §4.7 startup sequence: mint instance_id, spawn,
// handshake, poll ready. Returns nil on success (state already = Ready).
// seq is the spawn's token from spawnAsync: every write into the slot
// checks it under the lock, so a superseded spawn tears its own child
// down instead of installing it. ctx is cancelled when the spawn is
// superseded.
func (sl *moduleSlot) spawnOnce(ctx context.Context, seq uint64, desired DesiredState) error {
	instanceID := mintInstanceID()
	stderr := moduleproto.NewRingSink(moduleproto.DefaultRingSinkBytes)
	spec := ChildSpec{
		Descriptor:      sl.desc,
		InstanceID:      instanceID,
		EffectiveGrants: desired.EffectiveGrants,
		Generation:      desired.DesiredGeneration,
		Stderr:          stderr,
	}
	spawnCtx, cancel := context.WithTimeout(ctx, sl.sup.cfg.SpawnDeadline)
	defer cancel()

	child, err := sl.startChildSafely(spawnCtx, spec)
	if err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	// State → Handshaking under lock.
	sl.mu.Lock()
	if sl.spawnSeq != seq {
		// A drain or a newer spawn owns the slot now; installing would
		// run this child beside its replacement under a stale view.
		sl.mu.Unlock()
		_ = child.Kill()
		_ = child.Wait()
		return errSpawnSuperseded
	}
	if sl.state == StateFailed || sl.state == StateDrainingDisable || sl.sup.closed.Load() {
		// State changed under us (operator disabled, integrity fault), or
		// the supervisor closed: its kill pass reads sl.child after
		// setting closed, so a child installed now would outlive it.
		sl.mu.Unlock()
		_ = child.Kill()
		_ = child.Wait()
		return errors.New("spawn aborted: state changed during spawn")
	}
	sl.state = StateHandshaking
	sl.instanceID = instanceID
	sl.child = child
	sl.codec = child.Codec()
	sl.stderr = stderr
	sl.mu.Unlock()

	// Build the host-side Peer over the child's codec. Install reverse
	// broker handlers BEFORE Start so the child's reverse calls are
	// served from the first frame.
	hostPeer, reverseClosed := sl.newHostPeer(child.Codec())
	view := ModuleGrantView{
		Name:       sl.name,
		Grants:     desired.EffectiveGrants,
		Generation: desired.DesiredGeneration,
	}
	if err := sl.installHandlersSafely(hostPeer, view); err != nil {
		teardownChild(child, hostPeer, sl.sup.cfg.BackoffMin)
		return fmt.Errorf("install handlers: %w", err)
	}
	hostPeer.Start()

	// Stash the peer so the proxy can reach it, unless a drain already
	// took this spawn's child out of the slot (and a newer spawn may have
	// put its own peer there).
	sl.mu.Lock()
	if sl.spawnSeq != seq {
		sl.mu.Unlock()
		teardownChild(child, hostPeer, sl.sup.cfg.BackoffMin)
		return errSpawnSuperseded
	}
	sl.peer = hostPeer
	sl.liveGrants = desired.EffectiveGrants
	sl.reverseClosed = reverseClosed
	sl.mu.Unlock()

	// Handshake (moduleproto.Handshake: cross-check identity + digests +
	// negotiate proto). The 5s deadline is on spawnCtx.
	_, err = moduleproto.Handshake(spawnCtx, hostPeer, moduleproto.HandshakeConfig{
		Expected: moduleproto.HandshakeExpected{
			Name:              sl.desc.Name,
			Version:           sl.desc.Version,
			ArtifactSHA256:    sl.desc.ArtifactSHA256,
			SurfaceSHA256:     sl.desc.SurfaceSHA256,
			DesiredGeneration: desired.DesiredGeneration,
			InstanceID:        instanceID,
		},
		Grants: grantsAsStrings(desired.EffectiveGrants),
		Limits: moduleproto.Limits{
			FrameBytes: sl.frameBytes(),
			DeadlineMs: int(sl.callDeadline().Milliseconds()),
			Inflight:   sl.maxInflight(),
		},
		HostProto:    moduleproto.DefaultProtoRange,
		HostFeatures: nil,
		HostCritical: nil,
	})
	if err != nil {
		// Integrity fault (digest mismatch, negotiation failure).
		teardownChild(child, hostPeer, sl.sup.cfg.BackoffMin)
		return fmt.Errorf("handshake: %w", err)
	}

	// Poll ready (§4.7 step 4).
	if err := moduleproto.WaitForReady(spawnCtx, hostPeer, 50*time.Millisecond); err != nil {
		teardownChild(child, hostPeer, sl.sup.cfg.BackoffMin)
		return fmt.Errorf("ready: %w", err)
	}

	// §4.7 step 5: verify the optional MCP tool surface. If the
	// descriptor declares tools, fetch module.tool.list and require
	// byte-equality with the descriptor digests; a mismatch (extra tool,
	// missing tool, renamed/reshaped tool) is a terminal integrity fault.
	// The child cannot add, rename, or reshape a tool at runtime (design
	// §5.1). Then install the verified tools into the host MCP server via
	// the registrar; the registrar's handlers dispatch dynamically, so
	// this is idempotent across respawns.
	if len(sl.desc.Tools) > 0 {
		if err := sl.verifyToolSurface(spawnCtx, hostPeer); err != nil {
			teardownChild(child, hostPeer, sl.sup.cfg.BackoffMin)
			return fmt.Errorf("tool surface: %w", err)
		}
	}
	// Success. Transition to Ready and start watching the child's exit.
	sl.mu.Lock()
	if sl.spawnSeq != seq {
		sl.mu.Unlock()
		teardownChild(child, hostPeer, sl.sup.cfg.BackoffMin)
		return errSpawnSuperseded
	}
	if sl.state == StateDrainingDisable || sl.state == StateFailed {
		sl.mu.Unlock()
		teardownChild(child, hostPeer, sl.sup.cfg.BackoffMin)
		return errors.New("spawn aborted: disabled/failed during ready")
	}
	sl.state = StateReady
	sl.mu.Unlock()

	go sl.watchExit(child, instanceID)
	go sl.watchFault(hostPeer, child, instanceID)
	return nil
}

// watchExit waits for child and records its exit: a crash charges the
// circuit and moves the slot to Crashed, and the supervise loop then
// drives Backoff → Starting. A slot that no longer holds child was
// already settled by a drain, which recorded the exit and may have
// started a replacement; this late watcher leaves that slot alone. The
// slot is matched by instanceID, minted per spawn, not by comparing
// children: a runner may return an uncomparable RunningChild value.
func (sl *moduleSlot) watchExit(child RunningChild, instanceID string) {
	sup := sl.sup
	waitErr := child.Wait()
	sl.mu.Lock()
	if sl.child == nil || sl.instanceID != instanceID {
		sl.mu.Unlock()
		return
	}
	// leaseFailing: handleLeaseExpired killed this child on purpose
	// and keeps state=Ready for the recovery arm. Its exit is a
	// drain, not a crash: charging the circuit for it let a few
	// store outages wedge a healthy module in Crashed.
	leaseDrain := sl.leaseFailing
	// A closed supervisor drained or killed the child itself: shutdown,
	// not a crash.
	expected := sup.closed.Load() ||
		sl.state == StateDrainingDisable ||
		sl.state == StateDrainingUpgrade ||
		sl.state == StateInstalledDisabled ||
		sl.state == StateFailed ||
		leaseDrain
	curState := sl.state
	// Clear runtime handles; they are no longer valid.
	sl.child = nil
	sl.peer = nil
	sl.codec = nil
	sl.lastExit = exitLabel(waitErr, expected)
	if leaseDrain {
		sl.lastExit = "lease-expired-drain"
	}
	if !expected {
		// Crashed, in the same critical section that clears the child: a
		// childless Ready in between is the lease-drain recovery case,
		// and the supervise loop would spawn a replacement this watcher
		// then marked Crashed.
		sl.state = StateCrashed
	}
	sl.mu.Unlock()
	if expected {
		// Drained exit: no restart, no charge.
		sup.logf("processmodule: %s expected exit: %v", sl.name, waitErr)
		return
	}
	sup.logf("processmodule: %s UNEXPECTED exit in %s: %v", sl.name, curState, waitErr)
	// The supervise loop picks the crash up.
	select {
	case sl.wake <- struct{}{}:
	default:
	}
}

// watchFault kills child once its peer records a terminal protocol fault
// (a malformed or unsolicited frame, a stalled transport). A faulted peer
// refuses every call while the child keeps running, so without this the
// module stayed Ready and failing until something else killed it. The
// kill hands the exit to watchExit, which records the crash and lets the
// supervise loop restart the module. The watcher ends without a kill when
// the read loop ends cleanly, and skips a child the slot no longer holds:
// that child was already reaped, and its process group id may name
// another process by now.
func (sl *moduleSlot) watchFault(peer *moduleproto.Peer, child RunningChild, instanceID string) {
	select {
	case <-peer.FatalDone():
	case <-peer.Done():
		if peer.FatalError() == nil {
			return
		}
	}
	sl.mu.Lock()
	current := sl.child != nil && sl.instanceID == instanceID
	sl.mu.Unlock()
	if !current {
		return
	}
	reason := textsafe.Truncate(textsafe.ScrubControlBytes(peer.FatalError().Error()), 200)
	sl.sup.logf("processmodule: %s peer fault, killing the child: %s", sl.name, reason)
	_ = child.Kill()
}

// verifyToolSurface fetches module.tool.list and requires byte-equality
// between each returned tool's canonical digest and the matching
// [ProcessModuleDescriptor.Tools] digest (design §5.1 / §4.7 step 5).
// Any divergence, an extra tool the descriptor did not approve, a
// missing tool, a duplicate id, or a reshaped tool whose digest no longer
// matches, is a terminal integrity fault surfaced as a
// [*moduleproto.HandshakeMismatchError] so [isIntegrityFault] quarantines
// the module to Failed rather than retrying. On success the verified
// tools are installed into the host MCP server via the registrar (the
// registrar's handlers dispatch dynamically, so this is idempotent across
// respawns and safe to call every spawn).
func (sl *moduleSlot) verifyToolSurface(ctx context.Context, peer *moduleproto.Peer) error {
	listCtx, cancel := context.WithTimeout(ctx, sl.sup.cfg.ToolListTimeout)
	defer cancel()
	raw, err := peer.Call(listCtx, moduleproto.MethodToolList, nil)
	if err != nil {
		return err
	}
	var res moduleproto.ToolListResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("decode tool.list: %w", err)
	}
	// Index the descriptor's approved digests by tool id.
	want := make(map[string]string, len(sl.desc.Tools))
	for _, td := range sl.desc.Tools {
		want[td.ID] = td.SHA256
	}
	seen := make(map[string]struct{}, len(res.Tools))
	for _, t := range res.Tools {
		if _, dup := seen[t.ID]; dup {
			return &moduleproto.HandshakeMismatchError{
				Field: "module.tool.list",
				Want:  "unique tool ids",
				Got:   "duplicate " + t.ID,
			}
		}
		seen[t.ID] = struct{}{}
		expected, ok := want[t.ID]
		if !ok {
			// The child exposed a tool the descriptor did not approve.
			return &moduleproto.HandshakeMismatchError{
				Field: "module.tool.list",
				Want:  "only descriptor-approved tools",
				Got:   "extra tool " + t.ID,
			}
		}
		if got := ModuleToolDigest(t); got != expected {
			return &moduleproto.HandshakeMismatchError{
				Field: "tools[" + t.ID + "].sha256",
				Want:  expected,
				Got:   got,
			}
		}
	}
	// Every descriptor-approved tool must be present.
	for id := range want {
		if _, ok := seen[id]; !ok {
			return &moduleproto.HandshakeMismatchError{
				Field: "module.tool.list",
				Want:  "tool " + id,
				Got:   "missing",
			}
		}
	}
	if sl.sup.tools != nil {
		if err := sl.registerToolsSafely(sl.name, res.Tools); err != nil {
			return err
		}
	}
	return nil
}

// startChildSafely spawns through the configured Runner under a recover
// guard: the Runner is swappable backend code (trusted vs sandboxed), and
// a panic routes to spawnOnce's error path so the module is marked
// crashed and backed off instead of the spawn goroutine's escape killing
// the process.
func (sl *moduleSlot) startChildSafely(ctx context.Context, spec ChildSpec) (child RunningChild, err error) {
	defer func() {
		if r := recover(); r != nil {
			child = nil
			err = fmt.Errorf("runner Start panic: %v\n%s", textsafe.Recovered(r), debug.Stack())
		}
	}()
	return sl.runner.Start(ctx, spec)
}

// installHandlersSafely installs the broker's reverse-call handlers under
// a recover guard: the broker is host-side code on the spawn path, and a
// panic routes to spawnOnce's error path (child torn down, module marked
// crashed) instead of escaping the spawn goroutine.
func (sl *moduleSlot) installHandlersSafely(peer *moduleproto.Peer, view ModuleGrantView) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("broker InstallHandlers panic: %v\n%s", textsafe.Recovered(r), debug.Stack())
		}
	}()
	sl.sup.broker.InstallHandlers(peer, view)
	return nil
}

// registerToolsSafely installs the verified module tools under a recover
// guard: the registrar is host-side code on the spawn path, and a panic
// surfaces as a tool-surface spawn error instead of escaping the spawn
// goroutine.
func (sl *moduleSlot) registerToolsSafely(name string, tools []moduleproto.Tool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("tools RegisterTools panic: %v\n%s", textsafe.Recovered(r), debug.Stack())
		}
	}()
	return sl.sup.tools.RegisterTools(name, tools)
}

// migrationsPending reports whether a module that declared a migration
// group is still waiting for the migration coordinator to run (design §7
// + §8: the supervisor refuses to spawn a module to Ready while its
// migrations are unapplied. MigrationsAppliedAt is nil until the
// short-lived coordinator provisions the per-module schema/role, runs
// Up, and stamps the timestamp). A module with no migration group is
// never pending.
func migrationsPending(desc ProcessModuleDescriptor, desired DesiredState) bool {
	return desc.MigrationGroup != "" && desired.MigrationsAppliedAt == nil
}

// otherArtifact reports whether the store's row names a different
// executable than this replica's descriptor: another replica booted a
// newer (or older) build and moved the row. Digests are hex, compared
// without regard to case; an empty row digest names nothing.
func otherArtifact(desc ProcessModuleDescriptor, desired DesiredState) bool {
	return desired.ArtifactSHA256 != "" && asciicase.Lower(desired.ArtifactSHA256) != asciicase.Lower(desc.ArtifactSHA256)
}

// handleCrashed charges the restart circuit and transitions to Backoff (or
// leaves the circuit open, in which case state stays Crashed and proxy
// returns 503 indefinitely until the operator bumps generation).
func (sl *moduleSlot) handleCrashed(desired DesiredState) {
	sl.chargeCircuit()
	open := sl.isCircuitOpen()
	if open {
		sl.sup.logf("processmodule: %s circuit OPEN: staying Crashed until generation bump", sl.name)
		// State stays Crashed; proxy returns 503.
		return
	}
	// Compute backoff with jitter and sleep before re-spawn.
	backoff := sl.nextBackoff()
	sl.setState(StateBackoff)
	go func() {
		time.Sleep(backoff)
		select {
		case <-sl.done:
			return
		case <-sl.sup.closeCh:
			return
		default:
		}
		// Re-enter spawn via wake; the supervise loop will call reconcile,
		// which (in StateBackoff + enabled) calls spawnAsync.
		select {
		case sl.wake <- struct{}{}:
		default:
		}
	}()
}

// nextBackoff returns the exponential backoff with jitter, bounded by
// [SupervisorConfig.BackoffMin] and [SupervisorConfig.BackoffMax].
func (sl *moduleSlot) nextBackoff() time.Duration {
	sl.mu.RLock()
	n := len(sl.restarts)
	sl.mu.RUnlock()
	min := sl.sup.cfg.BackoffMin
	max := sl.sup.cfg.BackoffMax
	// 2^n * min, capped at max.
	d := min << n
	if d <= 0 || d > max {
		d = max
	}
	if d < min {
		d = min
	}
	// Add up to 50% jitter.
	var b [4]byte
	_, _ = rand.Read(b[:])
	jitter := time.Duration(int64(d) * int64(b[0]) / 512)
	return d + jitter
}

// chargeCircuit appends the current timestamp and trims to CircuitWindow.
// Called on every unexpected exit while desired-enabled.
func (sl *moduleSlot) chargeCircuit() {
	now := sl.sup.now()
	cutoff := now.Add(-sl.sup.cfg.CircuitWindow)
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.restarts = append(sl.restarts, now)
	// Trim old entries.
	kept := sl.restarts[:0]
	for _, t := range sl.restarts {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	sl.restarts = kept
	if len(sl.restarts) >= sl.sup.cfg.CircuitThreshold {
		sl.circuitOpen = true
	}
}

// isCircuitOpen reports whether the restart circuit is currently open.
func (sl *moduleSlot) isCircuitOpen() bool {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.circuitOpen
}

// resetCircuit clears the crash history and reopens the circuit. Called on
// every generation bump (design §8).
func (sl *moduleSlot) resetCircuit() {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.restarts = nil
	sl.circuitOpen = false
}

// markCrashedLocked transitions to Crashed with a diagnostic; the
// supervise loop picks up restart-or-Failed on its next tick (failSpawn
// wakes it through reportCrashed). sl.mu must be held.
func (sl *moduleSlot) markCrashedLocked(reason string) {
	sl.state = StateCrashed
	sl.lastExit = reason
}

// reportCrashed logs a Crashed transition and wakes the supervise loop.
// Called with sl.mu released.
func (sl *moduleSlot) reportCrashed(reason string) {
	sl.sup.logf("processmodule: %s spawn failed: %s", sl.name, reason)
	select {
	case sl.wake <- struct{}{}:
	default:
	}
}

// markFailedLocked transitions to terminal Failed. Used for integrity
// faults (SHA mismatch, handshake digest mismatch, negotiation failure),
// a restart cannot fix a bad artifact (design §8). sl.mu must be held.
func (sl *moduleSlot) markFailedLocked(reason string) {
	sl.state = StateFailed
	sl.lastExit = "failed: " + reason
}

// reportFailed logs a Failed transition. Called with sl.mu released.
func (sl *moduleSlot) reportFailed(reason string) {
	sl.sup.logf("processmodule: %s FAILED (terminal): %s", sl.name, reason)
}

// maxInflight returns the descriptor-negotiated inflight cap (default 32).
func (sl *moduleSlot) maxInflight() int {
	if n := sl.desc.Limits.Inflight; n > 0 {
		return n
	}
	return moduleproto.DefaultMaxInflight
}

// frameBytes returns the descriptor-negotiated frame cap.
func (sl *moduleSlot) frameBytes() int {
	if n := sl.desc.Limits.FrameBytes; n > 0 {
		return n
	}
	return moduleproto.DefaultMaxFrameBytes
}

// callDeadline returns the per-call deadline ceiling.
func (sl *moduleSlot) callDeadline() time.Duration {
	if d := sl.desc.Limits.Deadline; d > 0 {
		return d
	}
	return maxModuleCallDeadline
}

// grantsAsStrings converts a permission slice to the string slice the
// handshake params carry.
func grantsAsStrings(grants []access.Permission) []string {
	out := make([]string, len(grants))
	for i, g := range grants {
		out[i] = string(g)
	}
	return out
}

// isIntegrityFault reports whether err is a terminal integrity fault that
// warrants Failed rather than a Crashed → restart cycle. Includes the
// moduleproto handshake mismatch, negotiation failure, and the runner's
// SHA mismatch.
func isIntegrityFault(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := errors.AsType[*moduleproto.HandshakeMismatchError](err); ok {
		return true
	}
	if errors.Is(err, moduleproto.ErrNegotiation) {
		return true
	}
	if errors.Is(err, moduleproto.ErrCriticalFeature) {
		return true
	}
	if _, ok := errors.AsType[*ExecutableSHAMismatchError](err); ok {
		return true
	}
	// A child that dies while the handshake is in flight surfaces as a
	// transport failure wrapped in "handshake:" (the pipe closed, EOF,
	// EPIPE, the spawn deadline). That is a crash, the same crash the
	// exit watcher would have reported a few milliseconds later, and it
	// restarts under backoff like one; a terminal verdict here let a
	// crash-looping module escape its circuit depending on which side of
	// the handshake write it died. Checked before the stage rule below.
	if isTransportFailure(err) {
		return false
	}
	// A handshake-stage error (round-trip mismatch surfaced as a wrapped
	// error) is integrity; the spawn-stage error (exec failed) is not.
	if errStr := err.Error(); strings.Contains(errStr, "handshake:") {
		return true
	}
	return false
}

// isTransportFailure reports whether err is the peer going away, the
// spawn budget running out, or a handshake call the child never answered
// (moduleproto marks those, deadline included), as opposed to a verdict
// the child answered.
func isTransportFailure(err error) bool {
	return errors.Is(err, moduleproto.ErrHandshakeUnanswered) ||
		errors.Is(err, moduleproto.ErrClosed) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.ErrClosedPipe) || errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, os.ErrClosed) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// teardownChild performs the §4.6 lift on a failed-spawn child: close
// stdin → wait the budget → kill → wait.
func teardownChild(child RunningChild, peer *moduleproto.Peer, budget time.Duration) {
	if peer != nil {
		_ = peer.Close()
	}
	_ = child.CloseStdin()
	waitCh := make(chan error, 1)
	go func() { waitCh <- child.Wait() }()
	select {
	case <-waitCh:
	case <-time.After(budget):
		_ = child.Kill()
		<-waitCh
	}
}

// exitLabel classifies a child exit for diagnostics.
func exitLabel(waitErr error, expected bool) string {
	if expected {
		return "drained"
	}
	if waitErr == nil {
		return "exited-cleanly-unexpected"
	}
	return "crashed: " + waitErr.Error()
}
