package framework

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
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
	// digest mismatch, protocol negotiation failure) — a restart cannot
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
	// [UntrustedNoSandboxError] (design §6 — never a silent downgrade
	// to Runner). A non-nil SandboxRunner whose backend does not pass
	// the P1–P7 conformance probe is equivalent to nil — SelectRunner
	// consults Sandbox.Conforms() at Register.
	Sandbox *SandboxRunner

	// Broker is the reverse-request capability broker. Defaults to
	// [NopBroker] (deny-all) when nil — the real broker lands in a later
	// wave.
	Broker ReverseBroker

	// ToolRegistrar installs a module's MCP tool surface into the host
	// *mcp.Server after handshake verifies it (design §5.1 / §4.7 step 5).
	// Nil means the supervisor still verifies digests at handshake (a
	// mismatch quarantines the module) but does not register tools into
	// any MCP server — tests that do not exercise the tool surface leave
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
	// handleRemoteToggle's fail-open — documented at
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
	// nil — tests inject a tunable clock so the 60s circuit window does
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
}

// snapshot is an RLock'd read of the slot's mutable state, for proxy /
// introspection.
type snapshot struct {
	state       ProcessState
	desiredGen  uint64
	enabled     bool
	instanceID  string
	peer        *moduleproto.Peer
	restartCnt  int
	circuitOpen bool
	lastExit    string
}

func (s *moduleSlot) snapshot() snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return snapshot{
		state:       s.state,
		desiredGen:  s.desiredGen,
		enabled:     s.enabled,
		instanceID:  s.instanceID,
		peer:        s.peer,
		restartCnt:  len(s.restarts),
		circuitOpen: s.circuitOpen,
		lastExit:    s.lastExit,
	}
}

// NewProcessModuleSupervisor constructs a supervisor. It does NOT call
// EnsureSchema — that is the app wiring's job (so the schema-creation
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

// Close stops every supervise loop, drains every child with a short
// deadline, and waits for the loops to exit. Idempotent. It does NOT close
// the store (the store is shared across the app and closed elsewhere).
func (s *ProcessModuleSupervisor) Close(ctx context.Context) error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(s.closeCh)

	// Stop the supervise loops. Each slot's drain runs as part of its own
	// supervise exit path.
	s.mu.Lock()
	slots := make([]*moduleSlot, 0, len(s.slots))
	for _, sl := range s.slots {
		slots = append(slots, sl)
	}
	s.mu.Unlock()
	for _, sl := range slots {
		close(sl.done)
	}
	// Wait for loops to exit (they each take one final reconcile pass).
	waitCh := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(waitCh)
	}()
	select {
	case <-waitCh:
	case <-ctx.Done():
	}
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

// Drain is the lifecycle.Drainer entry point. It drains every child
// concurrently (design §8: a single shared 30s budget across the app, so a
// per-child 30s drain applied serially to N children would blow the budget).
// Each child gets min(DrainPerModule, remaining shared budget).
func (s *ProcessModuleSupervisor) Drain(ctx context.Context) error {
	s.mu.Lock()
	slots := make([]*moduleSlot, 0, len(s.slots))
	for _, sl := range s.slots {
		slots = append(slots, sl)
	}
	s.mu.Unlock()

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
		sl := sl
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.drainOneForShutdown(sl, remaining)
		}()
	}
	wg.Wait()
	return nil
}

// drainOneForShutdown performs the §4.6 teardown for one child during
// shutdown: send module.drain, close stdin (EOF), wait the remaining
// budget, then Kill + Wait. It is best-effort and never panics.
func (s *ProcessModuleSupervisor) drainOneForShutdown(sl *moduleSlot, budget time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			s.logf("processmodule: drain %s panic: %v\n%s", sl.name, r, debug.Stack())
		}
	}()
	sl.mu.Lock()
	child := sl.child
	peer := sl.peer
	sl.mu.Unlock()
	if child == nil {
		return
	}
	// Best-effort module.drain notification (child may be mid-crash).
	if peer != nil {
		drainCtx, cancel := context.WithTimeout(context.Background(), minDur(budget, 2*time.Second))
		_, _ = peer.Call(drainCtx, moduleproto.MethodDrain, moduleproto.DrainParams{
			Reason:     "shutdown",
			DeadlineMs: int(budget.Milliseconds()),
		})
		cancel()
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
// is random and never persisted — its only job is to reject a stale or
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
// the validated descriptor, the computed effective grants, and the initial
// desired-generation the store row was written at.
type RegisterResult struct {
	Descriptor      ProcessModuleDescriptor
	EffectiveGrants []access.Permission
	Generation      uint64
}

// Register validates the descriptor, installs a desired-state row in the
// store, and registers the module for supervision. It is idempotent on the
// descriptor — calling it twice for the same name returns
// [ErrModuleInstalled] (the store row already exists). The supervisor's
// per-module loop is started lazily by [ProcessModuleSupervisor.StartLoops]
// or implicitly by [ProcessModuleSupervisor.Enable].
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
	selectedRunner, err := SelectRunner(d.TrustTier, s.runner, s.sandbox)
	if err != nil {
		return RegisterResult{}, &UntrustedNoSandboxError{Module: d.Name, cause: err}
	}
	// Install the desired-state row at generation 1.
	if err := s.store.Install(ctx, DesiredState{
		Module:            d.Name,
		DesiredGeneration: 1,
		Enabled:           false,
		ArtifactSHA256:    d.ArtifactSHA256,
		EffectiveGrants:   effective,
	}); err != nil {
		return RegisterResult{}, err
	}
	// Create the slot. The supervise loop is NOT started yet — it starts
	// (lazily) when StartLoops is called by the app wiring, or the first
	// time the module is Enabled.
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
	s.logf("processmodule: registered %q gen=1 grants=%d", d.Name, len(effective))
	return RegisterResult{Descriptor: d, EffectiveGrants: effective, Generation: 1}, nil
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

// startLoop spawns the supervise + heartbeat goroutines for one slot. It
// is safe to call only once per slot (guarded by the once-guard pattern of
// setting state to a non-Absent value before the call).
func (s *ProcessModuleSupervisor) startLoop(sl *moduleSlot) {
	s.wg.Add(2)
	go sl.supervise()
	go sl.heartbeat()
}

// Enable marks a module desired-enabled in the store and wakes its
// supervise loop. Returns [ErrNoDesiredRow] (wrapped) if the module is not
// registered.
func (s *ProcessModuleSupervisor) Enable(ctx context.Context, name string) error {
	if s.closed.Load() {
		return errClosedSup
	}
	if s.Slot(name) == nil {
		return fmt.Errorf("processmodule: %w: %q", ErrNoDesiredRow, name)
	}
	if err := s.store.SetEnabled(ctx, name, true); err != nil {
		return err
	}
	s.startLoopIfAbsent(name)
	s.wake(name)
	return nil
}

// Disable marks a module desired-disabled in the store and wakes its
// supervise loop (which drains the child).
func (s *ProcessModuleSupervisor) Disable(ctx context.Context, name string) error {
	if s.closed.Load() {
		return errClosedSup
	}
	if s.Slot(name) == nil {
		return fmt.Errorf("processmodule: %w: %q", ErrNoDesiredRow, name)
	}
	if err := s.store.SetEnabled(ctx, name, false); err != nil {
		return err
	}
	s.wake(name)
	return nil
}

// RevokeGrants replaces the effective grants AND bumps generation — every
// replica's next reconcile observes the higher generation and restarts the
// child with the narrowed view (design §5 "binding + revocation"). Returns
// the new generation.
func (s *ProcessModuleSupervisor) RevokeGrants(ctx context.Context, name string, grants []access.Permission) (uint64, error) {
	if s.closed.Load() {
		return 0, errClosedSup
	}
	if s.Slot(name) == nil {
		return 0, fmt.Errorf("processmodule: %w: %q", ErrNoDesiredRow, name)
	}
	gen, err := s.store.SetEffectiveGrants(ctx, name, grants)
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
// started yet. Used by Enable so a lazily-Registered module comes online.
func (s *ProcessModuleSupervisor) startLoopIfAbsent(name string) {
	s.mu.Lock()
	sl := s.slots[name]
	s.mu.Unlock()
	if sl == nil {
		return
	}
	// Use a once-guard: if state == Absent, we never started. Otherwise
	// the loop is already running (it sets state to InstalledDisabled at
	// construction; Absent is only the zero value of ProcessState).
	sl.mu.Lock()
	start := sl.state == StateAbsent
	sl.mu.Unlock()
	if start {
		sl.mu.Lock()
		sl.state = StateInstalledDisabled
		sl.mu.Unlock()
		s.startLoop(sl)
	}
}

// Reconcile pokes the named module's supervise loop. It is the single
// entry point the three reconcile sources funnel into (local enable/
// disable, handleRemoteToggle after its store re-read, periodic generation
// poll — design §8). Safe to call for unknown names (no-op).
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
		_ = sl.sup.store.RecordHeartbeat(ctx, sl.name, sl.sup.cfg.ReplicaID, gen, state.String())
		cancel()
	}
}

// reconcile drives one pass of the state machine. It is the only place
// state transitions happen (apart from the proxy path which is read-only).
func (sl *moduleSlot) reconcile() {
	defer func() {
		if r := recover(); r != nil {
			sl.sup.logf("processmodule: reconcile %s panic: %v\n%s", sl.name, r, debug.Stack())
		}
	}()
	desired, err := sl.refreshDesired()
	if err != nil {
		// Lease enforcement: if the store has been unreadable past
		// LeaseTTL, fail CLOSED (drain children). This is the deliberate
		// inversion of handleRemoteToggle's fail-open — documented at
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
			sl.transitionDrain(StateDrainingDisable, desired.DesiredGeneration)
		} else if state == StateDrainingDisable || state == StateInstalledDisabled ||
			state == StateAbsent || state == StateFailed {
			// Already drained or never started.
			sl.setState(StateInstalledDisabled)
		}
	case desired.Enabled && migrationsPending(sl.desc, desired):
		// Migrations pending (design §7 / §8): the module declares a
		// migration group whose DDL the coordinator has not yet applied
		// (MigrationsAppliedAt is nil). Refuse Ready — do NOT spawn. Hold
		// in InstalledDisabled so the proxy returns 503 and module tools
		// return retryable-unavailable until the operator runs the
		// coordinator, which stamps MigrationsAppliedAt and (via a
		// generation bump) re-arms reconcile. setState no-ops when already
		// InstalledDisabled, so this does not spam the periodic poll.
		if state != StateInstalledDisabled {
			sl.setState(StateInstalledDisabled)
		}
	case desired.Enabled && (state == StateInstalledDisabled || state == StateAbsent):
		// Desired enabled, currently down. Spawn.
		sl.spawnAsync(desired)
	case desired.Enabled && genAdvanced &&
		(state == StateReady || state == StateStarting || state == StateHandshaking):
		// Upgrade: drain then spawn.
		sl.transitionDrain(StateDrainingUpgrade, desired.DesiredGeneration)
	case desired.Enabled && state == StateCrashed:
		// Crashed: charge circuit, decide restart vs Failed.
		sl.handleCrashed(desired)
	case desired.Enabled && state == StateBackoff:
		// Backoff elapsed: try again.
		sl.spawnAsync(desired)
	case desired.Enabled && state == StateFailed:
		// Terminal; do nothing until generation bump.
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
	d, err := sl.sup.store.GetDesired(ctx, sl.name)
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
	sl.sup.logf("processmodule: %s lease expired — draining (fail-closed)", sl.name)
	if child == nil {
		return
	}
	// Quick drain — no module.drain notification (the child may be fine,
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
func (sl *moduleSlot) transitionDrain(drainState ProcessState, gen uint64) {
	sl.setState(drainState)
	sl.mu.Lock()
	child := sl.child
	peer := sl.peer
	sl.mu.Unlock()
	if child == nil {
		// Nothing running.
		sl.finishDrain(drainState, gen)
		return
	}
	budget := sl.sup.cfg.DrainPerModule
	// Notify the child; it may finish in-flight work and exit cleanly.
	if peer != nil {
		dCtx, cancel := context.WithTimeout(context.Background(), minDur(budget, 2*time.Second))
		_, _ = peer.Call(dCtx, moduleproto.MethodDrain, moduleproto.DrainParams{
			Reason:     drainReason(drainState),
			DeadlineMs: int(budget.Milliseconds()),
		})
		cancel()
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
	sl.mu.Lock()
	sl.child = nil
	sl.peer = nil
	sl.codec = nil
	sl.lastExit = drainState.String()
	sl.mu.Unlock()
	sl.finishDrain(drainState, gen)
}

// finishDrain transitions to the post-drain state. On an upgrade drain it
// re-reads the AUTHORITATIVE desired state from the store before respawning:
// a revoke/upgrade bumped the generation precisely because the effective
// grant set (or the enabled flag) changed, so the new child must be built
// from the store's current EffectiveGrants — never the descriptor's original
// RequestedGrants, which would keep serving a just-revoked capability across
// the respawn. If the store cannot be read, it fails closed (stays disabled)
// rather than respawn a possibly-revoked module — the same posture as the
// state lease.
func (sl *moduleSlot) finishDrain(drainState ProcessState, gen uint64) {
	switch drainState {
	case StateDrainingDisable:
		sl.setState(StateInstalledDisabled)
	case StateDrainingUpgrade:
		desired, err := sl.refreshDesired()
		if err != nil {
			sl.sup.logf("processmodule: %s upgrade-drain desired re-read failed (%v) — staying disabled (fail-closed)", sl.name, err)
			sl.setState(StateInstalledDisabled)
			return
		}
		// Preserve the generation we converged to if the store read lags it
		// (the poll that triggered this drain saw >= gen).
		if desired.DesiredGeneration < gen {
			desired.DesiredGeneration = gen
		}
		if desired.Enabled {
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
	sl.setState(StateStarting)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				sl.sup.logf("processmodule: spawn %s panic: %v\n%s", sl.name, r, debug.Stack())
				sl.markCrashed("spawn panic: " + fmt.Sprint(r))
			}
		}()
		err := sl.spawnOnce(desired)
		if err != nil {
			// Integrity faults → terminal Failed; other errors → Crashed.
			if isIntegrityFault(err) {
				sl.markFailed(err.Error())
			} else {
				sl.markCrashed(err.Error())
			}
		}
		// On success, spawnOnce already set state = Ready and started
		// the exit-watcher goroutine.
	}()
}

// spawnOnce runs the §4.7 startup sequence: mint instance_id, spawn,
// handshake, poll ready. Returns nil on success (state already = Ready).
func (sl *moduleSlot) spawnOnce(desired DesiredState) error {
	instanceID := mintInstanceID()
	stderr := moduleproto.NewRingSink(moduleproto.DefaultRingSinkBytes)
	spec := ChildSpec{
		Descriptor:      sl.desc,
		InstanceID:      instanceID,
		EffectiveGrants: desired.EffectiveGrants,
		Generation:      desired.DesiredGeneration,
		Stderr:          stderr,
	}
	spawnCtx, cancel := context.WithTimeout(context.Background(), sl.sup.cfg.SpawnDeadline)
	defer cancel()

	child, err := sl.runner.Start(spawnCtx, spec)
	if err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	// State → Handshaking under lock.
	sl.mu.Lock()
	if sl.state == StateFailed || sl.state == StateDrainingDisable {
		// State changed under us (operator disabled, integrity fault).
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
	hostPeer := moduleproto.NewPeer(child.Codec(), moduleproto.RoleHost,
		moduleproto.WithMaxInflight(sl.maxInflight()),
		moduleproto.WithMaxServeInflight(sl.maxInflight()),
	)
	view := ModuleGrantView{
		Name:       sl.name,
		Grants:     desired.EffectiveGrants,
		Generation: desired.DesiredGeneration,
	}
	sl.sup.broker.InstallHandlers(hostPeer, view)
	hostPeer.Start()

	// Stash the peer so the proxy can reach it.
	sl.mu.Lock()
	sl.peer = hostPeer
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
	// missing tool, renamed/reshaped tool) is a terminal integrity fault
	// — the child cannot add, rename, or reshape a tool at runtime (design
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
	if sl.state == StateDrainingDisable || sl.state == StateFailed {
		sl.mu.Unlock()
		teardownChild(child, hostPeer, sl.sup.cfg.BackoffMin)
		return errors.New("spawn aborted: disabled/failed during ready")
	}
	sl.state = StateReady
	sl.mu.Unlock()

	// Exit watcher: charge circuit on unexpected exit, transition to
	// Crashed. The supervise loop then drives Backoff → Starting.
	sup := sl.sup
	go func() {
		waitErr := child.Wait()
		sl.mu.Lock()
		expected := sl.state == StateDrainingDisable ||
			sl.state == StateDrainingUpgrade ||
			sl.state == StateInstalledDisabled ||
			sl.state == StateFailed
		curState := sl.state
		// Clear runtime handles; they are no longer valid.
		sl.child = nil
		sl.peer = nil
		sl.codec = nil
		sl.lastExit = exitLabel(waitErr, expected)
		sl.mu.Unlock()
		if expected {
			// Drained exit — no restart, no charge.
			sup.logf("processmodule: %s expected exit: %v", sl.name, waitErr)
			return
		}
		sup.logf("processmodule: %s UNEXPECTED exit in %s: %v", sl.name, curState, waitErr)
		// Crashed; the supervise loop will pick it up.
		sl.mu.Lock()
		sl.state = StateCrashed
		sl.mu.Unlock()
		select {
		case sl.wake <- struct{}{}:
		default:
		}
	}()
	return nil
}

// verifyToolSurface fetches module.tool.list and requires byte-equality
// between each returned tool's canonical digest and the matching
// [ProcessModuleDescriptor.Tools] digest (design §5.1 / §4.7 step 5).
// Any divergence — an extra tool the descriptor did not approve, a
// missing tool, a duplicate id, or a reshaped tool whose digest no longer
// matches — is a terminal integrity fault surfaced as a
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
		if err := sl.sup.tools.RegisterTools(sl.name, res.Tools); err != nil {
			return err
		}
	}
	return nil
}

// migrationsPending reports whether a module that declared a migration
// group is still waiting for the migration coordinator to run (design §7
// + §8: the supervisor refuses to spawn a module to Ready while its
// migrations are unapplied — MigrationsAppliedAt is nil until the
// short-lived coordinator provisions the per-module schema/role, runs
// Up, and stamps the timestamp). A module with no migration group is
// never pending.
func migrationsPending(desc ProcessModuleDescriptor, desired DesiredState) bool {
	return desc.MigrationGroup != "" && desired.MigrationsAppliedAt == nil
}

// handleCrashed charges the restart circuit and transitions to Backoff (or
// leaves the circuit open, in which case state stays Crashed and proxy
// returns 503 indefinitely until the operator bumps generation).
func (sl *moduleSlot) handleCrashed(desired DesiredState) {
	sl.chargeCircuit()
	open := sl.isCircuitOpen()
	if open {
		sl.sup.logf("processmodule: %s circuit OPEN — staying Crashed until generation bump", sl.name)
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

// markCrashed transitions to Crashed with a diagnostic; the supervise loop
// will pick up restart-or-Failed on its next tick.
func (sl *moduleSlot) markCrashed(reason string) {
	sl.mu.Lock()
	sl.state = StateCrashed
	sl.lastExit = reason
	sl.mu.Unlock()
	sl.sup.logf("processmodule: %s spawn failed: %s", sl.name, reason)
	select {
	case sl.wake <- struct{}{}:
	default:
	}
}

// markFailed transitions to terminal Failed. Used for integrity faults
// (SHA mismatch, handshake digest mismatch, negotiation failure) — a
// restart cannot fix a bad artifact (design §8).
func (sl *moduleSlot) markFailed(reason string) {
	sl.mu.Lock()
	sl.state = StateFailed
	sl.lastExit = "failed: " + reason
	sl.mu.Unlock()
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
	var hs *moduleproto.HandshakeMismatchError
	if errors.As(err, &hs) {
		return true
	}
	if errors.Is(err, moduleproto.ErrNegotiation) {
		return true
	}
	if errors.Is(err, moduleproto.ErrCriticalFeature) {
		return true
	}
	var sha *ExecutableSHAMismatchError
	if errors.As(err, &sha) {
		return true
	}
	// A handshake-stage error (round-trip mismatch surfaced as a wrapped
	// error) is integrity; the spawn-stage error (exec failed) is not.
	if errStr := err.Error(); strings.Contains(errStr, "handshake:") {
		return true
	}
	return false
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
