package framework

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This file holds the wave-3a sandbox conformance-probe tests:
//
//   - TestProbeIDs — the P1–P7 enum shape (the §6 contract surface).
//   - TestConformanceReport_Conforms — the fail-closed gate predicate.
//   - TestRunConformance_NilBackend / _UnavailableBackend — every probe
//     unreachable when the backend cannot run; Conforms() false.
//   - TestProbeChildBodies_PrintParseableLine — every probe body, run
//     in-process (no spawning), prints exactly one parseable result line.
//   - TestParseProbeOutput_Contract — the child-stdout → ProbeStatus
//     translation (a regression here would silently flip pass/fail).
//   - TestSandboxConformance — the CI gate: runs the full probe suite
//     against the host's available backend, logs the report, and either
//     passes (host conforms) or skips with the report summary (host
//     cannot run untrusted in v1 — the documented outcome per §11 risk 1).
//     It NEVER passes silently as if enforcement happened.

func TestProbeIDs(t *testing.T) {
	if len(allProbes) != 7 {
		t.Fatalf("allProbes: want 7, got %d", len(allProbes))
	}
	want := map[ProbeID]string{
		ProbeDistinctPrincipal:     "P1",
		ProbeNoInheritedSecret:     "P2",
		ProbeNoInheritedFD:         "P3",
		ProbeNoNetworkEgress:       "P4",
		ProbeFilesystemConfinement: "P5",
		ProbeResourceLimits:        "P6",
		ProbeNoPrivReEscalation:    "P7",
	}
	for p, label := range want {
		if p.String() != label {
			t.Errorf("ProbeID(%d).String() = %q, want %q", int(p), p.String(), label)
		}
		if p.Title() == "" {
			t.Errorf("ProbeID(%d).Title() empty", int(p))
		}
	}
}

func TestConformanceReport_Conforms(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    ConformanceReport
		want bool
	}{
		{
			name: "unavailable backend",
			r:    ConformanceReport{Backend: "x", Available: false},
			want: false,
		},
		{
			name: "available but no results",
			r:    ConformanceReport{Backend: "x", Available: true},
			want: false,
		},
		{
			name: "one breach",
			r: ConformanceReport{
				Backend: "x", Available: true,
				Results: []ProbeResult{
					{ID: ProbeDistinctPrincipal, Status: ProbeStatusFail},
					{ID: ProbeNoInheritedSecret, Status: ProbeStatusPass},
				},
			},
			want: false,
		},
		{
			name: "one unreachable",
			r: ConformanceReport{
				Backend: "x", Available: true,
				Results: []ProbeResult{
					{ID: ProbeDistinctPrincipal, Status: ProbeStatusPass},
					{ID: ProbeNoInheritedSecret, Status: ProbeStatusUnreachable},
				},
			},
			want: false,
		},
		{
			name: "all pass",
			r: func() ConformanceReport {
				results := make([]ProbeResult, 0, len(allProbes))
				for _, p := range allProbes {
					results = append(results, ProbeResult{ID: p, Status: ProbeStatusPass})
				}
				return ConformanceReport{Backend: "x", Available: true, Results: results}
			}(),
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Conforms(); got != tc.want {
				t.Errorf("Conforms = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRunConformance_NilBackend(t *testing.T) {
	r := RunConformance(context.Background(), nil, t)
	if r.Conforms() {
		t.Fatal("nil backend must not conform")
	}
	if r.Available {
		t.Error("nil backend must report Available=false")
	}
	for _, res := range r.Results {
		if res.Status != ProbeStatusUnreachable {
			t.Errorf("nil backend probe %s: want unreachable, got %s", res.ID, res.Status)
		}
	}
}

func TestRunConformance_UnavailableBackend(t *testing.T) {
	b := &fakeBackend{name: "fake", available: false, missing: "test-missing"}
	r := RunConformance(context.Background(), b, t)
	if r.Conforms() {
		t.Fatal("unavailable backend must not conform")
	}
	if r.MissingReason != "test-missing" {
		t.Errorf("MissingReason = %q, want %q", r.MissingReason, "test-missing")
	}
	for _, res := range r.Results {
		if res.Status != ProbeStatusUnreachable {
			t.Errorf("unavailable backend probe %s: want unreachable, got %s", res.ID, res.Status)
		}
	}
}

// TestProbeChildBodies_PrintParseableLine asserts each probe body, run
// in-process with the context env vars it expects, prints exactly one
// line beginning with one of the probeOut* sentinels. The bodies run
// WITHOUT a sandbox wrap (we test the bodies, not the backends) — so
// probes that check for sandbox enforcement will report BREACH here (no
// sandbox = the forbidden action succeeds). That is expected: this test
// asserts the body produces a parseable line, not that the action was
// denied.
func TestProbeChildBodies_PrintParseableLine(t *testing.T) {
	for _, p := range allProbes {
		t.Run(p.String(), func(t *testing.T) {
			withProbeEnv(t, p, t.TempDir(), func() {
				var out bytes.Buffer
				restore := captureStdout(&out)
				_ = runProbeChildBody(p)
				restore() // close pipe + drain copy goroutine BEFORE reading
				line := strings.TrimSpace(out.String())
				if line == "" {
					t.Fatalf("probe %s body printed nothing", p)
				}
				first := line
				if idx := strings.IndexByte(line, '\n'); idx >= 0 {
					first = strings.TrimSpace(line[:idx])
				}
				switch {
				case strings.HasPrefix(first, probeOutPass),
					strings.HasPrefix(first, probeOutBreach),
					strings.HasPrefix(first, probeOutUnreachable):
					// ok — parseable protocol line
				default:
					t.Errorf("probe %s body printed unparsable line %q", p, first)
				}
			})
		})
	}
}

// withProbeEnv sets the GOFASTR_PROBE_* context vars the body expects
// for probe p, runs fn, and restores the env on cleanup via t.Setenv.
func withProbeEnv(t *testing.T, p ProbeID, scratch string, fn func()) {
	t.Helper()
	t.Setenv(probeEnvName, fmt.Sprintf("%d", int(p)))
	t.Setenv("GOFASTR_PROBE_HOST_UID", hostUIDString())
	t.Setenv("GOFASTR_PROBE_HOST_PID", fmt.Sprintf("%d", os.Getpid()))
	switch p {
	case ProbeNoInheritedSecret:
		t.Setenv("GOFASTR_PROBE_CANARY_NAME", "GOFASTR_TEST_CANARY")
		secretPath := scratch + "/host-secret"
		if err := os.WriteFile(secretPath, []byte("file-secret"), 0o600); err != nil {
			t.Fatalf("plant secret: %v", err)
		}
		t.Setenv("GOFASTR_PROBE_SECRET_FILE", secretPath)
	case ProbeNoInheritedFD:
		t.Setenv("GOFASTR_PROBE_FD_SECRET", scratch+"/fd-secret")
		t.Setenv("GOFASTR_PROBE_HOST_FD_NUM", "3")
	case ProbeNoNetworkEgress:
		t.Setenv("GOFASTR_PROBE_NET_TARGETS", "127.0.0.1:1,1.1.1.1:53,169.254.169.254:80")
	case ProbeFilesystemConfinement:
		t.Setenv("GOFASTR_PROBE_SCRATCH", scratch)
		if home, err := os.UserHomeDir(); err == nil {
			t.Setenv("GOFASTR_PROBE_HOME", home)
		}
	case ProbeResourceLimits:
		t.Setenv("GOFASTR_PROBE_FORK_COUNT", "8")
	}
	fn()
}

// captureStdout redirects os.Stdout into w until the returned restore
// func is called. The pipe + copy goroutine is the standard pattern.
func captureStdout(w *bytes.Buffer) (restore func()) {
	r, wPipe, err := os.Pipe()
	if err != nil {
		return func() {}
	}
	orig := os.Stdout
	os.Stdout = wPipe
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(w, r)
		_ = r.Close()
		close(done)
	}()
	return func() {
		os.Stdout = orig
		_ = wPipe.Close()
		<-done
	}
}

// TestParseProbeOutput_Contract asserts the probe-child stdout parser
// maps each sentinel correctly. This is the load-bearing translation
// between the child's printed line and the ProbeStatus the report
// carries; a regression here would silently flip pass/fail.
func TestParseProbeOutput_Contract(t *testing.T) {
	for _, tc := range []struct {
		in       string
		timedOut bool
		want     ProbeStatus
	}{
		{"PASS", false, ProbeStatusPass},
		{"PASS child uid=42 isolated", false, ProbeStatusPass},
		{"BREACH setuid(0) succeeded", false, ProbeStatusFail},
		{"UNREACHABLE no /proc", false, ProbeStatusUnreachable},
		{"", false, ProbeStatusUnreachable},
		{"PASS", true, ProbeStatusUnreachable},
		{"garbage line", false, ProbeStatusUnreachable},
	} {
		got := parseProbeOutput(ProbeNoInheritedSecret, tc.in, tc.timedOut, tc.in).Status
		if got != tc.want {
			t.Errorf("parseProbeOutput(%q, timedOut=%v) = %s, want %s", tc.in, tc.timedOut, got, tc.want)
		}
	}
}

// TestProbeTimeoutIsBounded guards against a future edit accidentally
// unbounded-ing the per-probe budget (a hung dial would otherwise stall
// the suite past the test timeout).
func TestProbeTimeoutIsBounded(t *testing.T) {
	if probeTimeout <= 0 || probeTimeout > 60*time.Second {
		t.Fatalf("probeTimeout = %s, want >0 and <=60s", probeTimeout)
	}
}

// TestSandboxConformance is the CI gate (design §10 gate item 3). It
// runs the full P1–P7 probe suite against the host's available backend,
// logs the report, and:
//
//   - PASSES if the host conforms (every probe P1–P7 enforced);
//   - SKIPS with the report summary if the host cannot conform (the
//     documented v1 outcome on darwin/windows/some-linux per §11 risk 1)
//     — never passing silently as if enforcement happened;
//   - SKIPS with a clear message if the host has no backend available.
func TestSandboxConformance(t *testing.T) {
	b := HostSandboxBackend()
	if b == nil {
		t.Skipf("no sandbox backend available on %s: untrusted modules fail-closed here (design §6)", runtime.GOOS)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report := RunConformance(ctx, b, t)
	t.Logf("conformance report for %s:\n%s", b.Name(), report.Summary())

	if !report.Conforms() {
		var failed []string
		for _, res := range report.Results {
			if res.Status != ProbeStatusPass {
				failed = append(failed, fmt.Sprintf("%s=%s", res.ID, res.Status))
			}
		}
		t.Skipf("host backend %s does not conform (%s); untrusted modules fail-closed on %s per design §11 risk 1",
			b.Name(), strings.Join(failed, ", "), runtime.GOOS)
	}
}
