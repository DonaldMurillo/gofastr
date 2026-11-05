package framework

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

type panickingProbeBackend struct{ fakeBackend }

func (*panickingProbeBackend) Wrap(*exec.Cmd, SandboxOpts) error {
	panic("backend wrap failed")
}

func TestProbePanicRefusesSandbox(t *testing.T) {
	backend := &panickingProbeBackend{fakeBackend: fakeBackend{name: "panic", available: true}}
	runner, err := NewSandboxRunner(backend, SandboxOpts{})
	var refusal *SandboxConformanceError
	if runner != nil || !errors.As(err, &refusal) {
		t.Fatalf("runner = %v, error = %v, want conformance refusal", runner, err)
	}
	if len(refusal.Report.Results) != len(allProbes) {
		t.Fatalf("probe results = %d, want %d", len(refusal.Report.Results), len(allProbes))
	}
	for _, result := range refusal.Report.Results {
		if result.Status != ProbeStatusUnreachable || !strings.Contains(result.Detail, "panic: backend wrap failed") {
			t.Errorf("probe %s = %+v, want unreachable backend panic", result.ID, result)
		}
	}
}
