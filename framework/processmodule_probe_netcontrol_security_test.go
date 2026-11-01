package framework

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"time"
)

// P4 counted every dial error as a denial against fixed targets
// (127.0.0.1:1, 1.1.1.1:53, the metadata IP). On a host where those are
// closed or filtered, a backend with no network denial passed. The proof
// of a refusal is a positive control: a listener the runner reached
// itself (2026-10-31 bug hunt, S71).

func TestP4NoControlIsUnreachable(t *testing.T) {
	t.Setenv("GOFASTR_PROBE_NET_CONTROLS", "")
	// Every fixed target refuses here, which used to read as a pass.
	t.Setenv("GOFASTR_PROBE_NET_TARGETS", closedLoopbackAddr(t))
	if line := runProbeBodyLine(t, ProbeNoNetworkEgress); !strings.HasPrefix(line, probeOutUnreachable) {
		t.Fatalf("no positive control: body printed %q, want UNREACHABLE", line)
	}
}

func TestP4ReachedControlIsBreach(t *testing.T) {
	controls, err := openProbeNetControls()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeProbeNetControls(controls) })
	addrs := make([]string, 0, len(controls))
	for _, c := range controls {
		addrs = append(addrs, c.ln.Addr().String())
	}
	t.Setenv("GOFASTR_PROBE_NET_CONTROLS", strings.Join(addrs, ","))
	t.Setenv("GOFASTR_PROBE_NET_TARGETS", closedLoopbackAddr(t))
	// Unconfined, the body reaches the control.
	if line := runProbeBodyLine(t, ProbeNoNetworkEgress); !strings.HasPrefix(line, probeOutBreach) {
		t.Fatalf("control reachable: body printed %q, want BREACH", line)
	}
	// And the host's own count fails the probe even if the child had
	// printed PASS.
	got := probeNetControlsVerdict(controls, ProbeResult{ID: ProbeNoNetworkEgress, Status: ProbeStatusPass})
	if got.Status != ProbeStatusFail {
		t.Fatalf("verdict after the child reached a control: %s (%s), want fail", got.Status, got.Detail)
	}
}

func TestP4ProofDialIsNotCounted(t *testing.T) {
	controls, err := openProbeNetControls()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeProbeNetControls(controls) })
	got := probeNetControlsVerdict(controls, ProbeResult{ID: ProbeNoNetworkEgress, Status: ProbeStatusPass})
	if got.Status != ProbeStatusPass {
		t.Fatalf("untouched controls: %s (%s), want the child's pass kept", got.Status, got.Detail)
	}
}

// runProbeBodyLine runs a probe body in this process and returns its
// result line.
func runProbeBodyLine(t *testing.T, p ProbeID) string {
	t.Helper()
	var out bytes.Buffer
	restore := captureStdout(&out)
	_ = runProbeChildBody(p)
	restore()
	return strings.TrimSpace(out.String())
}

// closedLoopbackAddr is a loopback address nothing listens on.
func closedLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	// Let the close settle so a dial is refused, not queued.
	time.Sleep(10 * time.Millisecond)
	return addr
}
