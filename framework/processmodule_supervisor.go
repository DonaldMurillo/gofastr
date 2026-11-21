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

// ProcessState is a module's supervisor state on one replica.
type ProcessState int

const (
	// StateAbsent: not registered with this supervisor.
	StateAbsent ProcessState = iota

	// StateInstalledDisabled: registered, no child running.
	StateInstalledDisabled

	// StateStarting: child exec'd, handshake not started.
	StateStarting

	// StateHandshaking: handshake in progress.
	StateHandshaking

	// StateReady: child live; the proxy forwards to it.
	StateReady

	// StateCrashed: unexpected exit while enabled.
	StateCrashed

	// StateBackoff: waiting to restart.
	StateBackoff

	// StateDrainingDisable: draining the child after a disable.
	StateDrainingDisable

	// StateDrainingUpgrade: draining the old child before a new generation.
	StateDrainingUpgrade

	// StateFailed: terminal integrity fault, until a generation bump.
	StateFailed
)

// String returns the state's name.
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

// SupervisorConfig configures a supervisor. Every zero field takes a default.
type SupervisorConfig struct {
	// Store is required.
	Store ProcessModuleStore

	// Runner spawns trusted modules (default [TrustedProcessRunner]).
	Runner Runner

	// Sandbox spawns untrusted modules; nil refuses every untrusted Register.
	Sandbox *SandboxRunner

	// Broker answers reverse calls (default [NopBroker], deny-all).
	Broker ReverseBroker

	// ToolRegistrar installs verified module tools; nil verifies without installing.
	ToolRegistrar ToolRegistrar

	// ToolListTimeout bounds the handshake's module.tool.list call (default 2s).
	ToolListTimeout time.Duration

	// ReplicaID names this replica in heartbeat rows (default random).
	ReplicaID string

	// SpawnDeadline bounds spawn, handshake and ready together (default 5s).
	SpawnDeadline time.Duration

	// PollInterval is the periodic reconcile tick (default 2s).
	PollInterval time.Duration

	// HeartbeatInterval is the heartbeat write interval (default 1s).
	HeartbeatInterval time.Duration

	// LeaseTTL is how long an unreadable store is tolerated before children are
	// drained (default 5s).
	LeaseTTL time.Duration

	// DrainPerModule bounds one child's graceful drain (default 30s).
	DrainPerModule time.Duration

	// BackoffMin and BackoffMax bound the restart backoff (default 250ms, 30s).
	BackoffMin time.Duration
	BackoffMax time.Duration

	// CircuitThreshold crashes within CircuitWindow stop restarts until a generation
	// bump (default 5 in 60s).
	CircuitThreshold int
	CircuitWindow    time.Duration

	// Now is the clock (default time.Now).
	Now func() time.Time

	// Logf receives diagnostic lines; nil discards them.
	Logf func(format string, args ...any)
}

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

// ProcessModuleSupervisor spawns, watches, restarts and stops process modules.
// Supervisors on several replicas converge through one shared store.
type ProcessModuleSupervisor struct {
	cfg     SupervisorConfig
	store   ProcessModuleStore
	runner  Runner
	sandbox *SandboxRunner
	broker  ReverseBroker
	tools   ToolRegistrar
	now     func() time.Time
	logf    func(string, ...any)
	mu      sync.Mutex
	slots   map[string]*moduleSlot
	closed  atomic.Bool
	closeCh chan struct{}
	wg      sync.WaitGroup

	loopsStarted atomic.Bool

	leaseMu sync.Mutex
	lease   map[string]time.Time
}

type moduleSlot struct {
	name   string
	desc   ProcessModuleDescriptor
	sup    *ProcessModuleSupervisor
	runner Runner
	wake   chan struct{}
	done   chan struct{}
	mu     sync.RWMutex

	state        ProcessState
	desiredGen   uint64
	enabled      bool
	instanceID   string
	child        RunningChild
	peer         *moduleproto.Peer
	codec        *moduleproto.Codec
	stderr       *moduleproto.RingSink
	restarts     []time.Time
	circuitOpen  bool
	lastExit     string
	leaseFailing bool
	loopStarted  bool

	liveGrants    []access.Permission
	reverseClosed *atomic.Bool

	spawnSeq    uint64
	spawnCancel context.CancelFunc
}

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

// NewProcessModuleSupervisor returns a supervisor; it errors on a nil Store and
// does not create the store's schema.
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

// Closed reports whether Close or Drain has run.
func (s *ProcessModuleSupervisor) Closed() bool { return s.closed.Load() }

// Close stops the loops and kills every running child. It is idempotent and leaves
// the store open.
func (s *ProcessModuleSupervisor) Close(ctx context.Context) error {
	slots := s.stopLoops(ctx)
	for _, sl := range slots {
		sl.mu.Lock()
		if sl.child != nil {
			_ = sl.child.Kill()
		}
		sl.mu.Unlock()
	}
	return nil
}

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

// Drain closes the supervisor and drains every child at once, each within
// min(DrainPerModule, ctx's remaining budget). Later calls do nothing.
func (s *ProcessModuleSupervisor) Drain(ctx context.Context) error {
	slots := s.stopLoops(ctx)

	deadline, ok := ctx.Deadline()
	remaining := s.cfg.DrainPerModule
	if ok {
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
	s.drainChild(sl.name, child, peer, budget, "shutdown", false)
	sl.mu.Lock()
	sl.child = nil
	sl.peer = nil
	sl.codec = nil
	sl.lastExit = exitLabel(nil, true)
	sl.mu.Unlock()
}

func (s *ProcessModuleSupervisor) drainChild(name string, child RunningChild, peer *moduleproto.Peer, budget time.Duration, reason string, closePeer bool) {
	waitCh := make(chan error, 1)
	go func() { waitCh <- child.Wait() }()
	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	if peer != nil {
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

func randomReplicaID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "replica-" + hex.EncodeToString(b[:])
}

func mintInstanceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

var errClosedSup = errors.New("processmodule: supervisor closed")

var _ ReverseBroker = NopBroker{}

// RegisterResult is what [ProcessModuleSupervisor.Register] did.
type RegisterResult struct {
	Descriptor      ProcessModuleDescriptor
	EffectiveGrants []access.Permission
	Generation      uint64
	// Outcome says how the store reconciled an existing row.
	Outcome InstallOutcome
	// PreviousGrants is the grant set a grants change replaced, else nil.
	PreviousGrants []access.Permission
}

// Register validates d, selects its runner by trust tier, and installs or
// reconciles its desired-state row. A second Register of a name returns
// [ErrModuleInstalled]; an untrusted module without a conforming sandbox returns
// [UntrustedNoSandboxError].
func (s *ProcessModuleSupervisor) Register(ctx context.Context, d ProcessModuleDescriptor, approved ApprovedGrants) (RegisterResult, error) {
	if s.closed.Load() {
		return RegisterResult{}, errClosedSup
	}
	effective, err := ValidateProcessModuleDescriptor(d, approved)
	if err != nil {
		return RegisterResult{}, err
	}
	s.mu.Lock()
	sandbox := s.sandbox
	s.mu.Unlock()
	selectedRunner, err := SelectRunner(d.TrustTier, s.runner, sandbox)
	if err != nil {
		return RegisterResult{}, &UntrustedNoSandboxError{Module: d.Name, cause: err}
	}
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
	slot.mu.Lock()
	slot.enabled = inst.Row.Enabled
	slot.mu.Unlock()
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

func (s *ProcessModuleSupervisor) setSandbox(r *SandboxRunner) {
	s.mu.Lock()
	s.sandbox = r
	s.mu.Unlock()
}

// UntrustedNoSandboxError is Register's refusal of an untrusted module when no
// conforming sandbox is available; it unwraps to the cause, usually
// [ErrSandboxUnavailable].
type UntrustedNoSandboxError struct {
	Module string
	cause  error
}

// Unwrap returns the cause.
func (e *UntrustedNoSandboxError) Unwrap() error { return e.cause }

func (e *UntrustedNoSandboxError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("processmodule: %q declares TrustUntrusted but no probe-passing SandboxRunner backend is available (design §6 fail-closed; never a silent downgrade): %v", e.Module, e.cause)
	}
	return fmt.Sprintf("processmodule: %q declares TrustUntrusted but no probe-passing SandboxRunner backend is available (design §6 fail-closed; never a silent downgrade)", e.Module)
}

// Slot returns the module's slot, or nil when it is not registered.
func (s *ProcessModuleSupervisor) Slot(name string) *moduleSlot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.slots[name]
}

// StartLoops starts every registered module's supervise loop. It is idempotent.
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

// Enable marks the module enabled and wakes it; it wraps [ErrNoDesiredRow] for an
// unknown name.
func (s *ProcessModuleSupervisor) Enable(ctx context.Context, name string) error {
	if err := s.setDesiredEnabled(ctx, name, true); err != nil {
		return err
	}
	s.startLoopIfAbsent(name)
	s.wake(name)
	return nil
}

// Disable marks the module disabled and wakes it to drain its child.
func (s *ProcessModuleSupervisor) Disable(ctx context.Context, name string) error {
	if err := s.setDesiredEnabled(ctx, name, false); err != nil {
		return err
	}
	s.wake(name)
	return nil
}

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

// RevokeGrants removes grants from the module's effective grants and bumps its
// generation, so every replica restarts the child with the narrower set. It
// returns the new generation.
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

// BumpGeneration increments the module's generation, which drains and respawns
// its child and re-arms a Failed or circuit-open module.
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

// Reconcile wakes the module's supervise loop; an unknown name is a no-op.
func (s *ProcessModuleSupervisor) Reconcile(name string) {
	s.wake(name)
}

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

func (sl *moduleSlot) recordHeartbeatSafely(ctx context.Context, gen uint64, state ProcessState) {
	defer func() {
		if r := recover(); r != nil {
			sl.sup.logf("processmodule: %s heartbeat panic: %v\n%s", sl.name, textsafe.Recovered(r), debug.Stack())
		}
	}()
	_ = sl.sup.store.RecordHeartbeat(ctx, sl.name, sl.sup.cfg.ReplicaID, gen, state.String())
}

func (sl *moduleSlot) reconcile() {
	defer func() {
		if r := recover(); r != nil {
			sl.sup.logf("processmodule: reconcile %s panic: %v\n%s", sl.name, textsafe.Recovered(r), debug.Stack())
		}
	}()
	desired, err := sl.refreshDesired()
	if err != nil {
		if errors.Is(err, errLeaseExpired) {
			sl.handleLeaseExpired()
			return
		}
		sl.sup.logf("processmodule: reconcile %s store read: %v", sl.name, err)
		return
	}

	sl.mu.Lock()
	oldGen := sl.desiredGen
	genAdvanced := desired.DesiredGeneration > oldGen
	sl.desiredGen = desired.DesiredGeneration
	sl.enabled = desired.Enabled
	state := sl.state
	childLive := sl.child != nil
	leaseRecovered := sl.leaseFailing
	sl.leaseFailing = false
	sl.mu.Unlock()

	if leaseRecovered {
		sl.sup.logf("processmodule: %s lease recovered: re-arming", sl.name)
	}

	if genAdvanced && oldGen != 0 {
		sl.resetCircuit()
	}

	switch {
	case !desired.Enabled:
		if state == StateReady || state == StateStarting ||
			state == StateHandshaking || state == StateCrashed ||
			state == StateBackoff {
			sl.transitionDrain(StateDrainingDisable, desired.DesiredGeneration, nil)
		} else if state == StateDrainingDisable || state == StateInstalledDisabled ||
			state == StateAbsent || state == StateFailed {
			sl.setState(StateInstalledDisabled)
		}
	case desired.Enabled && migrationsPending(sl.desc, desired):
		if state == StateReady || state == StateStarting ||
			state == StateHandshaking || state == StateCrashed ||
			state == StateBackoff {
			sl.transitionDrain(StateDrainingDisable, desired.DesiredGeneration, nil)
		} else if state != StateInstalledDisabled {
			sl.setState(StateInstalledDisabled)
		}
	case desired.Enabled && otherArtifact(sl.desc, desired):
		if state == StateReady || state == StateStarting ||
			state == StateHandshaking || state == StateCrashed ||
			state == StateBackoff {
			sl.sup.logf("processmodule: %s row names artifact %s, this replica runs %s: holding", sl.name, shortHash(desired.ArtifactSHA256), shortHash(sl.desc.ArtifactSHA256))
			sl.transitionDrain(StateDrainingDisable, desired.DesiredGeneration, nil)
		} else if state != StateInstalledDisabled {
			sl.setState(StateInstalledDisabled)
		}
	case desired.Enabled && (state == StateInstalledDisabled || state == StateAbsent):
		sl.spawnAsync(desired)
	case desired.Enabled && genAdvanced &&
		(state == StateReady || state == StateStarting || state == StateHandshaking):
		sl.transitionDrain(StateDrainingUpgrade, desired.DesiredGeneration, desired.EffectiveGrants)
	case desired.Enabled && state == StateReady && !childLive:
		sl.spawnAsync(desired)
	case desired.Enabled && state == StateCrashed:
		sl.handleCrashed(desired)
	case desired.Enabled && state == StateBackoff:
		sl.spawnAsync(desired)
	case desired.Enabled && state == StateFailed:
		if genAdvanced {
			sl.spawnAsync(desired)
		}
	case desired.Enabled && state == StateReady:
	default:
	}
}

const desiredStateReadTimeout = 2 * time.Second

func (sl *moduleSlot) refreshDesired() (DesiredState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), desiredStateReadTimeout)
	defer cancel()
	d, err := sl.getDesiredSafely(ctx)
	if err == nil {
		sl.sup.leaseMu.Lock()
		sl.sup.lease[sl.name] = sl.sup.now()
		sl.sup.leaseMu.Unlock()
		return d, nil
	}
	sl.sup.leaseMu.Lock()
	last, ok := sl.sup.lease[sl.name]
	sl.sup.leaseMu.Unlock()
	if !ok {
		return DesiredState{}, errLeaseExpired
	}
	if sl.sup.now().Sub(last) > sl.sup.cfg.LeaseTTL {
		return DesiredState{}, errLeaseExpired
	}
	return DesiredState{}, err
}

func (sl *moduleSlot) getDesiredSafely(ctx context.Context) (d DesiredState, err error) {
	defer func() {
		if r := recover(); r != nil {
			d, err = DesiredState{}, fmt.Errorf("store GetDesired panic: %v\n%s", textsafe.Recovered(r), debug.Stack())
		}
	}()
	return sl.sup.store.GetDesired(ctx, sl.name)
}

func (s *ProcessModuleSupervisor) listDesiredSafely(ctx context.Context) (rows []DesiredState, err error) {
	defer func() {
		if r := recover(); r != nil {
			rows, err = nil, fmt.Errorf("store ListDesired panic: %v\n%s", textsafe.Recovered(r), debug.Stack())
		}
	}()
	return s.store.ListDesired(ctx)
}

var errLeaseExpired = errors.New("processmodule: state lease expired")

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
	sl.state = state
	sl.lastExit = "lease-expired-drain"
	sl.mu.Unlock()
}

func (sl *moduleSlot) setState(next ProcessState) {
	sl.mu.Lock()
	prev := sl.state
	sl.state = next
	sl.mu.Unlock()
	if prev != next {
		sl.sup.logf("processmodule: %s %s → %s", sl.name, prev, next)
	}
}

func (sl *moduleSlot) transitionDrain(drainState ProcessState, gen uint64, next []access.Permission) {
	sl.setState(drainState)
	sl.mu.Lock()
	sl.supersedeSpawnLocked()
	child := sl.child
	peer := sl.peer
	closed, held := sl.reverseClosed, sl.liveGrants
	if closed != nil && (drainState == StateDrainingDisable || !grantsCover(next, held)) {
		closed.Store(true)
	}
	sl.mu.Unlock()
	if child == nil {
		sl.finishDrain(drainState, gen)
		return
	}
	stopWatch := sl.watchDrainRevoke(closed, held)
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

func grantsCover(next, held []access.Permission) bool {
	for _, g := range held {
		if !access.ScopeMatch(next, g) {
			return false
		}
	}
	return true
}

func (sl *moduleSlot) newHostPeer(codec *moduleproto.Codec) (*moduleproto.Peer, *atomic.Bool) {
	closed := new(atomic.Bool)
	return moduleproto.NewPeer(codec, moduleproto.RoleHost,
		moduleproto.WithMaxInflight(sl.maxInflight()),
		moduleproto.WithMaxServeInflight(sl.maxInflight()),
		moduleproto.WithServeGate(reverseGate(closed)),
	), closed
}

func reverseGate(closed *atomic.Bool) func(string) *moduleproto.Error {
	return func(method string) *moduleproto.Error {
		if closed.Load() && strings.HasPrefix(method, "host.") {
			return &moduleproto.Error{Code: moduleproto.CodeCapabilityDenied, Message: "moduleproto broker: module is draining after a revoke or disable"}
		}
		return nil
	}
}

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
		if desired.DesiredGeneration < gen {
			desired.DesiredGeneration = gen
		}
		if desired.Enabled && !migrationsPending(sl.desc, desired) && !otherArtifact(sl.desc, desired) {
			sl.spawnAsync(desired)
		} else {
			sl.setState(StateInstalledDisabled)
		}
	}
}

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
			sl.mu.Lock()
			if sl.spawnSeq == seq {
				sl.spawnCancel = nil
			}
			sl.mu.Unlock()
			return
		}
		if !sl.failSpawn(seq, err.Error(), isIntegrityFault(err)) {
			sl.sup.logf("processmodule: %s superseded spawn ended: %v", sl.name, err)
		}
	}()
}

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
	stderr := sl.stderr
	sl.mu.Unlock()
	if terminal {
		sl.reportFailed(reason, stderr)
	} else {
		sl.reportCrashed(reason, stderr)
	}
	return true
}

func (sl *moduleSlot) supersedeSpawnLocked() {
	if sl.spawnCancel != nil {
		sl.spawnCancel()
		sl.spawnCancel = nil
	}
	sl.spawnSeq++
}

var errSpawnSuperseded = errors.New("spawn aborted: superseded by a drain or a newer spawn")

func (sl *moduleSlot) spawnOnce(ctx context.Context, seq uint64, desired DesiredState) error {
	instanceID := mintInstanceID()
	stderr := moduleproto.NewRingSink(moduleproto.DefaultRingSinkBytes)
	sl.mu.Lock()
	if sl.spawnSeq != seq {
		sl.mu.Unlock()
		return errSpawnSuperseded
	}
	sl.stderr = stderr
	sl.mu.Unlock()
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
	sl.mu.Lock()
	if sl.spawnSeq != seq {
		sl.mu.Unlock()
		_ = child.Kill()
		_ = child.Wait()
		return errSpawnSuperseded
	}
	if sl.state == StateFailed || sl.state == StateDrainingDisable || sl.sup.closed.Load() {
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
		teardownChild(child, hostPeer, sl.sup.cfg.BackoffMin)
		return fmt.Errorf("handshake: %w", err)
	}

	if err := moduleproto.WaitForReady(spawnCtx, hostPeer, 50*time.Millisecond); err != nil {
		teardownChild(child, hostPeer, sl.sup.cfg.BackoffMin)
		return fmt.Errorf("ready: %w", err)
	}

	if len(sl.desc.Tools) > 0 {
		if err := sl.verifyToolSurface(spawnCtx, hostPeer); err != nil {
			teardownChild(child, hostPeer, sl.sup.cfg.BackoffMin)
			return fmt.Errorf("tool surface: %w", err)
		}
	}
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

func (sl *moduleSlot) watchExit(child RunningChild, instanceID string) {
	sup := sl.sup
	waitErr := child.Wait()
	sl.mu.Lock()
	if sl.child == nil || sl.instanceID != instanceID {
		sl.mu.Unlock()
		return
	}
	leaseDrain := sl.leaseFailing
	expected := sup.closed.Load() ||
		sl.state == StateDrainingDisable ||
		sl.state == StateDrainingUpgrade ||
		sl.state == StateInstalledDisabled ||
		sl.state == StateFailed ||
		leaseDrain
	curState := sl.state
	sl.child = nil
	sl.peer = nil
	sl.codec = nil
	sl.lastExit = exitLabel(waitErr, expected)
	if leaseDrain {
		sl.lastExit = "lease-expired-drain"
	}
	if !expected {
		sl.state = StateCrashed
	}
	stderr := sl.stderr
	sl.mu.Unlock()
	if expected {
		sup.logf("processmodule: %s expected exit: %v", sl.name, waitErr)
		return
	}
	sup.logf("processmodule: %s UNEXPECTED exit in %s: %v", sl.name, curState, waitErr)
	sup.logChildStderr(sl.name, stderr)
	select {
	case sl.wake <- struct{}{}:
	default:
	}
}

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

func (sl *moduleSlot) startChildSafely(ctx context.Context, spec ChildSpec) (child RunningChild, err error) {
	defer func() {
		if r := recover(); r != nil {
			child = nil
			err = fmt.Errorf("runner Start panic: %v\n%s", textsafe.Recovered(r), debug.Stack())
		}
	}()
	return sl.runner.Start(ctx, spec)
}

func (sl *moduleSlot) installHandlersSafely(peer *moduleproto.Peer, view ModuleGrantView) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("broker InstallHandlers panic: %v\n%s", textsafe.Recovered(r), debug.Stack())
		}
	}()
	sl.sup.broker.InstallHandlers(peer, view)
	return nil
}

func (sl *moduleSlot) registerToolsSafely(name string, tools []moduleproto.Tool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("tools RegisterTools panic: %v\n%s", textsafe.Recovered(r), debug.Stack())
		}
	}()
	return sl.sup.tools.RegisterTools(name, tools)
}

func migrationsPending(desc ProcessModuleDescriptor, desired DesiredState) bool {
	return desc.MigrationGroup != "" && desired.MigrationsAppliedAt == nil
}

func otherArtifact(desc ProcessModuleDescriptor, desired DesiredState) bool {
	return desired.ArtifactSHA256 != "" && asciicase.Lower(desired.ArtifactSHA256) != asciicase.Lower(desc.ArtifactSHA256)
}

func (sl *moduleSlot) handleCrashed(desired DesiredState) {
	sl.chargeCircuit()
	open := sl.isCircuitOpen()
	if open {
		sl.sup.logf("processmodule: %s circuit OPEN: staying Crashed until generation bump", sl.name)
		return
	}
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
		select {
		case sl.wake <- struct{}{}:
		default:
		}
	}()
}

func (sl *moduleSlot) nextBackoff() time.Duration {
	sl.mu.RLock()
	n := len(sl.restarts)
	sl.mu.RUnlock()
	min := sl.sup.cfg.BackoffMin
	max := sl.sup.cfg.BackoffMax
	d := min << n
	if d <= 0 || d > max {
		d = max
	}
	if d < min {
		d = min
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	jitter := time.Duration(int64(d) * int64(b[0]) / 512)
	return d + jitter
}

func (sl *moduleSlot) chargeCircuit() {
	now := sl.sup.now()
	cutoff := now.Add(-sl.sup.cfg.CircuitWindow)
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.restarts = append(sl.restarts, now)
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

func (sl *moduleSlot) isCircuitOpen() bool {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.circuitOpen
}

func (sl *moduleSlot) resetCircuit() {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.restarts = nil
	sl.circuitOpen = false
}

func (sl *moduleSlot) markCrashedLocked(reason string) {
	sl.state = StateCrashed
	sl.lastExit = reason
}

func (sl *moduleSlot) reportCrashed(reason string, stderr *moduleproto.RingSink) {
	sl.sup.logChildStderr(sl.name, stderr)
	sl.sup.logf("processmodule: %s spawn failed: %s", sl.name, reason)
	select {
	case sl.wake <- struct{}{}:
	default:
	}
}

func (sl *moduleSlot) markFailedLocked(reason string) {
	sl.state = StateFailed
	sl.lastExit = "failed: " + reason
}

func (sl *moduleSlot) reportFailed(reason string, stderr *moduleproto.RingSink) {
	sl.sup.logChildStderr(sl.name, stderr)
	sl.sup.logf("processmodule: %s FAILED (terminal): %s", sl.name, reason)
}

func (sl *moduleSlot) maxInflight() int {
	if n := sl.desc.Limits.Inflight; n > 0 {
		return n
	}
	return moduleproto.DefaultMaxInflight
}

func (sl *moduleSlot) frameBytes() int {
	if n := sl.desc.Limits.FrameBytes; n > 0 {
		return n
	}
	return moduleproto.DefaultMaxFrameBytes
}

func (sl *moduleSlot) callDeadline() time.Duration {
	if d := sl.desc.Limits.Deadline; d > 0 {
		return d
	}
	return maxModuleCallDeadline
}

func grantsAsStrings(grants []access.Permission) []string {
	out := make([]string, len(grants))
	for i, g := range grants {
		out[i] = string(g)
	}
	return out
}

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
	if isTransportFailure(err) {
		return false
	}
	if errStr := err.Error(); strings.Contains(errStr, "handshake:") {
		return true
	}
	return false
}

func isTransportFailure(err error) bool {
	return errors.Is(err, moduleproto.ErrHandshakeUnanswered) ||
		errors.Is(err, moduleproto.ErrClosed) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.ErrClosedPipe) || errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, os.ErrClosed) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

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

func exitLabel(waitErr error, expected bool) string {
	if expected {
		return "drained"
	}
	if waitErr == nil {
		return "exited-cleanly-unexpected"
	}
	return "crashed: " + waitErr.Error()
}

func (s *ProcessModuleSupervisor) logChildStderr(name string, stderr *moduleproto.RingSink) {
	if stderr == nil {
		return
	}
	tail := textsafe.ScrubControlBytes(string(stderr.Tail()))
	if tail != "" {
		s.logf("processmodule: %s stderr tail: %s", textsafe.ScrubControlBytes(name), tail)
	}
}
