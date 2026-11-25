package check

import "testing"

func TestTrustedTypesFlagsGlobalEval(t *testing.T) {
	for _, tc := range []struct {
		name, source, finding string
	}{
		{"window", "window.eval(source);", "window.eval is a sink"},
		{"globalThis", "globalThis . eval(source);", "globalThis . eval is a sink"},
		{"self", "self.eval(source);", "self.eval is a sink"},
		{"optional window", "window?.eval(source);", "window?.eval is a sink"},
		{"optional globalThis", "globalThis ?. eval(source);", "globalThis ?. eval is a sink"},
		{"optional nested receiver", "sandbox?.window.eval(source);", ""},
		{"other receiver", "sandbox.eval(source);", ""},
		{"spaced receiver", "sandbox . eval(source);", ""},
		{"nested receiver", "sandbox. window.eval(source);", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeRuntimeFixture(t, "fixture.js", tc.source)
			if tc.finding == "" {
				lintExpectFindings(t, LintTrustedTypesSinks, dir, 0, "none", "[trusted-types-sink]")
				return
			}
			lintOneFinding(t, LintTrustedTypesSinks, dir, tc.finding)
		})
	}
}
