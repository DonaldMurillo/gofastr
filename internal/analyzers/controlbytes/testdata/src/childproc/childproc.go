// Package childproc is framework/processmodule_probe.go reduced: the
// probe parser's stderr/stdout parameters flow into ProbeResult.Detail
// through tailForDetail (trim and cap, no scrub), which the test
// harness and ErrSandboxUnavailable then print as operator error text
// — probe TestProbeRedScrubsStderrControlBytes, against the
// scrubTerminalBytes/scrubTerminalOutput standard.
package childproc

import (
	"log/slog"
	"strings"
)

// ProbeResult mirrors the framework's operator-facing diagnostic.
type ProbeResult struct {
	ID     string
	Status string
	Detail string
}

// parseProbeOutput is the pre-fix parser reduced to the Detail arms.
func parseProbeOutput(stdout string, timedOut bool, stderr string) ProbeResult {
	if timedOut {
		detail := "probe timed out"
		if stderr != "" {
			detail += "; stderr tail: " + tailForDetail(stderr)
		}
		return ProbeResult{Detail: detail} // want `controlbytes: request-derived value reaches the Detail diagnostic field unscrubbed`
	}
	detail := "denial observed; stderr tail: " + tailForDetail(stderr)
	return ProbeResult{Detail: detail} // want `controlbytes: request-derived value reaches the Detail diagnostic field unscrubbed`
}

// parseProbeOutputScrubbed is the fixed spelling: the choke point
// scrubs with the scrubTerminalBytes contract.
func parseProbeOutputScrubbed(stdout string, timedOut bool, stderr string) ProbeResult {
	if timedOut {
		detail := "probe timed out"
		if stderr != "" {
			detail += "; stderr tail: " + scrubbedTail(stderr)
		}
		return ProbeResult{Detail: detail}
	}
	return ProbeResult{Detail: "denial observed; stderr tail: " + scrubbedTail(stderr)}
}

// stderrAtLogSink: child output reaching an ordinary log sink is the
// same property — the replay, not the field name, is the operator
// surface.
func stderrAtLogSink(stderr string) {
	slog.String("child stderr", stderr) // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
	slog.String("child stderr scrubbed", scrubbedTail(stderr))
}

// assignmentSpelling pins the .Detail = arm of the sink.
func assignmentSpelling(stderr string) ProbeResult {
	var res ProbeResult
	res.Detail = "stderr tail: " + tailForDetail(stderr) // want `controlbytes: request-derived value reaches the Detail diagnostic field unscrubbed`
	return res
}

// tailForDetail is the pre-fix helper: TrimSpace plus a 200-byte cap,
// no control-byte scrub.
func tailForDetail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 200 {
		return s
	}
	return "…" + s[len(s)-200:]
}

// scrubbedTail is scrubTerminalBytes' shape: the name says scrub and
// the body's comparisons name the control range, keeping LF and TAB.
func scrubbedTail(s string) string {
	var b strings.Builder
	for i := range s {
		if c := s[i]; (c < 0x20 && c != '\n' && c != '\t') || c == 0x7f {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
