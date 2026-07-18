package framework

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// This file adds unit coverage for the supervisor control surface, String
// methods, error types, accessor helpers, and error-path branches in
// processmodule_supervisor.go. No children are spawned; the tests exercise
// helpers directly or against a supervisor with a registered descriptor.

// ---- String methods ----

func TestProcessState_StringAllStates(t *testing.T) {
	states := []ProcessState{
		StateAbsent, StateInstalledDisabled, StateStarting, StateHandshaking,
		StateReady, StateCrashed, StateBackoff, StateDrainingDisable,
		StateDrainingUpgrade, StateFailed,
	}
	want := []string{
		"Absent", "InstalledDisabled", "Starting", "Handshaking",
		"Ready", "Crashed", "Backoff", "DrainingDisable",
		"DrainingUpgrade", "Failed",
	}
	for i, s := range states {
		if got := s.String(); got != want[i] {
			t.Errorf("state[%d].String() = %q, want %q", i, got, want[i])
		}
	}
	// Out-of-range state renders a numeric label.
	if got := ProcessState(999).String(); !strings.Contains(got, "999") {
		t.Errorf("unknown state String = %q, want numeric", got)
	}
}

func TestTrustTier_StringRenders(t *testing.T) {
	if TrustUntrusted.String() != "untrusted" {
		t.Errorf("untrusted = %q", TrustUntrusted.String())
	}
	if TrustTrusted.String() != "trusted" {
		t.Errorf("trusted = %q", TrustTrusted.String())
	}
	if got := TrustTier(99).String(); !strings.Contains(got, "99") {
		t.Errorf("unknown tier = %q, want numeric", got)
	}
}

// ---- drainReason ----

func TestDrainReason_mapsStates(t *testing.T) {
	cases := []struct {
		state ProcessState
		want  string
	}{
		{StateDrainingDisable, "disable"},
		{StateDrainingUpgrade, "upgrade"},
		{StateReady, "shutdown"},
		{StateAbsent, "shutdown"},
	}
	for _, c := range cases {
		if got := drainReason(c.state); got != c.want {
			t.Errorf("drainReason(%s) = %q, want %q", c.state, got, c.want)
		}
	}
}

// ---- minDur ----

func TestMinDur_picksSmaller(t *testing.T) {
	if got := minDur(time.Second, time.Millisecond); got != time.Millisecond {
		t.Errorf("minDur = %v", got)
	}
	if got := minDur(time.Millisecond, time.Second); got != time.Millisecond {
		t.Errorf("minDur = %v", got)
	}
}

// ---- applyDefaults ----

func TestApplyDefaults_fillsZeros(t *testing.T) {
	c := SupervisorConfig{Store: newTestStore(t)}
	c.applyDefaults()
	if c.Runner == nil || c.Broker == nil || c.Now == nil || c.Logf == nil {
		t.Errorf("applyDefaults left a nil: %+v", c)
	}
	if c.SpawnDeadline <= 0 || c.PollInterval <= 0 || c.HeartbeatInterval <= 0 {
		t.Errorf("durations not set: %+v", c)
	}
	if c.LeaseTTL <= 0 || c.DrainPerModule <= 0 || c.BackoffMin <= 0 || c.BackoffMax <= 0 {
		t.Errorf("durations not set: %+v", c)
	}
	if c.CircuitThreshold <= 0 || c.CircuitWindow <= 0 {
		t.Errorf("circuit knobs not set: %+v", c)
	}
	if c.ReplicaID == "" {
		t.Error("ReplicaID not minted")
	}
	if c.ToolListTimeout <= 0 {
		t.Error("ToolListTimeout not set")
	}
}

// ---- NewProcessModuleSupervisor ----

func TestNewSupervisor_nilStoreErrors(t *testing.T) {
	if _, err := NewProcessModuleSupervisor(SupervisorConfig{}); err == nil {
		t.Error("NewProcessModuleSupervisor with nil Store must error")
	}
}

// ---- Closed / Drain / Reconcile (control, no spawn) ----

func TestClosed_beforeAndAfterClose(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	if sup.Closed() {
		t.Error("fresh supervisor reports Closed")
	}
	if err := sup.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !sup.Closed() {
		t.Error("supervisor not Closed after Close")
	}
}

func TestClose_isIdempotent(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	if err := sup.Close(context.Background()); err != nil {
		t.Fatalf("Close 1: %v", err)
	}
	if err := sup.Close(context.Background()); err != nil {
		t.Fatalf("Close 2: %v", err)
	}
}

func TestDrain_emptySupervisorReturnsNil(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sup.Drain(ctx); err != nil {
		t.Errorf("Drain on empty supervisor = %v, want nil", err)
	}
}

func TestReconcile_unknownModuleIsNoOp(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	// Must not panic / block on an unregistered name.
	sup.Reconcile("ghost")
}

// ---- Register error paths ----

func TestRegister_closedSupervisorErrors(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	if err := sup.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := sup.Register(context.Background(), validDescriptor(), nil)
	if !errors.Is(err, errClosedSup) {
		t.Errorf("Register on closed supervisor = %v, want errClosedSup", err)
	}
}

func TestRegister_invalidDescriptorErrors(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	bad := validDescriptor()
	bad.Name = "" // invalid
	if _, err := sup.Register(context.Background(), bad, nil); err == nil {
		t.Error("Register with empty name must error")
	}
}

// ---- Enable / Disable / BumpGeneration / RevokeGrants error paths ----

func TestEnable_unknownModuleErrors(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	if err := sup.Enable(context.Background(), "ghost"); !errors.Is(err, ErrNoDesiredRow) {
		t.Errorf("Enable(unknown) = %v, want ErrNoDesiredRow", err)
	}
}

func TestDisable_unknownModuleErrors(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	if err := sup.Disable(context.Background(), "ghost"); !errors.Is(err, ErrNoDesiredRow) {
		t.Errorf("Disable(unknown) = %v, want ErrNoDesiredRow", err)
	}
}

func TestBumpGeneration_unknownModuleErrors(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	if _, err := sup.BumpGeneration(context.Background(), "ghost"); !errors.Is(err, ErrNoDesiredRow) {
		t.Errorf("BumpGeneration(unknown) = %v, want ErrNoDesiredRow", err)
	}
}

func TestRevokeGrants_unknownModuleErrors(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	if _, err := sup.RevokeGrants(context.Background(), "ghost", nil); !errors.Is(err, ErrNoDesiredRow) {
		t.Errorf("RevokeGrants(unknown) = %v, want ErrNoDesiredRow", err)
	}
}

// ---- UntrustedNoSandboxError ----

func TestUntrustedNoSandboxError_message(t *testing.T) {
	e := &UntrustedNoSandboxError{Module: "demo"}
	if !strings.Contains(e.Error(), "demo") || !strings.Contains(e.Error(), "fail-closed") {
		t.Errorf("Error() without cause = %q", e.Error())
	}
	e2 := &UntrustedNoSandboxError{Module: "demo", cause: errors.New("no backend")}
	if !strings.Contains(e2.Error(), "no backend") {
		t.Errorf("Error() with cause = %q", e2.Error())
	}
	if !errors.Is(e2.Unwrap(), e2.cause) {
		t.Error("Unwrap does not return cause")
	}
}

// ---- migrationsPending ----

func TestMigrationsPending_logic(t *testing.T) {
	// No migration group → never pending.
	if migrationsPending(ProcessModuleDescriptor{}, DesiredState{}) {
		t.Error("no migration group should not be pending")
	}
	// Group set, no stamp → pending.
	desc := ProcessModuleDescriptor{MigrationGroup: "demo"}
	if !migrationsPending(desc, DesiredState{}) {
		t.Error("group set with nil stamp should be pending")
	}
	// Group set, stamp present → not pending.
	stamp := time.Now()
	if migrationsPending(desc, DesiredState{MigrationsAppliedAt: &stamp}) {
		t.Error("group set with stamp should not be pending")
	}
}

// ---- isIntegrityFault ----

func TestIsIntegrityFault_classifies(t *testing.T) {
	if isIntegrityFault(nil) {
		t.Error("nil is not an integrity fault")
	}
	if isIntegrityFault(errors.New("some transient error")) {
		t.Error("generic error is not an integrity fault")
	}
	hs := &moduleproto.HandshakeMismatchError{Field: "x", Want: "a", Got: "b"}
	if !isIntegrityFault(hs) {
		t.Error("HandshakeMismatchError is an integrity fault")
	}
	if !isIntegrityFault(moduleproto.ErrNegotiation) {
		t.Error("ErrNegotiation is an integrity fault")
	}
	if !isIntegrityFault(moduleproto.ErrCriticalFeature) {
		t.Error("ErrCriticalFeature is an integrity fault")
	}
	sha := &ExecutableSHAMismatchError{Path: "x", Expected: "a", Actual: "b"}
	if !isIntegrityFault(sha) {
		t.Error("ExecutableSHAMismatchError is an integrity fault")
	}
	// A wrapped error whose message contains "handshake:" is treated as integrity.
	if !isIntegrityFault(errors.New("handshake: boom")) {
		t.Error("'handshake:' message should be integrity fault")
	}
}

// ---- slot limit accessors ----

func TestSlotLimitAccessors_defaultsAndOverrides(t *testing.T) {
	sl := &moduleSlot{desc: ProcessModuleDescriptor{}}
	if got := sl.maxInflight(); got != moduleproto.DefaultMaxInflight {
		t.Errorf("default maxInflight = %d, want %d", got, moduleproto.DefaultMaxInflight)
	}
	if got := sl.frameBytes(); got != moduleproto.DefaultMaxFrameBytes {
		t.Errorf("default frameBytes = %d, want %d", got, moduleproto.DefaultMaxFrameBytes)
	}
	if got := sl.callDeadline(); got != maxModuleCallDeadline {
		t.Errorf("default callDeadline = %v, want %v", got, maxModuleCallDeadline)
	}
	sl.desc.Limits.Inflight = 5
	sl.desc.Limits.FrameBytes = 1024
	sl.desc.Limits.Deadline = 2 * time.Second
	if got := sl.maxInflight(); got != 5 {
		t.Errorf("override maxInflight = %d, want 5", got)
	}
	if got := sl.frameBytes(); got != 1024 {
		t.Errorf("override frameBytes = %d, want 1024", got)
	}
	if got := sl.callDeadline(); got != 2*time.Second {
		t.Errorf("override callDeadline = %v, want 2s", got)
	}
}

// ---- exitLabel ----

func TestExitLabel_classifies(t *testing.T) {
	if got := exitLabel(nil, true); got != "drained" {
		t.Errorf("exitLabel(expected) = %q, want drained", got)
	}
	if got := exitLabel(nil, false); got != "exited-cleanly-unexpected" {
		t.Errorf("exitLabel(clean unexpected) = %q", got)
	}
	if got := exitLabel(errors.New("segfault"), false); !strings.Contains(got, "crashed") || !strings.Contains(got, "segfault") {
		t.Errorf("exitLabel(crash) = %q", got)
	}
}

// ---- grantsAsStrings ----

func TestGrantsAsStrings_preservesOrder(t *testing.T) {
	in := []access.Permission{"a:read", "b:write"}
	got := grantsAsStrings(in)
	if len(got) != 2 || got[0] != "a:read" || got[1] != "b:write" {
		t.Errorf("grantsAsStrings = %+v", got)
	}
}
