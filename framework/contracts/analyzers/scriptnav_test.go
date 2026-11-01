package analyzers_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1802/1803 used to read Go files only, and 1802 needed a quote
// straight after the `=`, so an app's static/app.js and any assignment
// from a variable passed `gofastr verify`.

func TestAppScriptHardNavIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":       "package main\n",
		"static/app.js": "function go(next) {\n  window.location.href = next;\n}\n",
	})
	d := assertHas(t, ds, contracts.RuleHardNavigation)
	if d.File != "static/app.js" || d.Line != 2 {
		t.Errorf("finding at %s, want static/app.js:2", d.Location())
	}
}

func TestAppScriptEventSourceIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":        "package main\n",
		"static/feed.js": "const es = new EventSource('/events');\n",
	})
	assertHas(t, ds, contracts.RuleBespokeEventSource)
}

func TestLocationSearchAssignIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"js.go": "package main\n\nconst script = `function f(q) { location.search = '?' + q }`\n",
	})
	assertHas(t, ds, contracts.RuleHardNavigation)
}

func TestHrefFromVariableIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"js.go": "package main\n\nconst script = `function f(u) { location.href = u }`\n",
	})
	assertHas(t, ds, contracts.RuleHardNavigation)
}

func TestScriptNavLookalikesAreClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": "package main\n\nvar location string\n\nfunc f() { location = city() }\n\nfunc city() string { return location }\n",
		"static/app.js": "const init = {};\ninit.location = e.location;\nif (location.href === prev) {}\n" +
			"location.hash = '#top';\n// location.href = '/in-a-comment'\n/* new EventSource('/x') */\n",
	})
	assertNot(t, ds, contracts.RuleHardNavigation, "a property named location, a comparison, a hash change, a Go variable and a comment")
	assertNot(t, ds, contracts.RuleBespokeEventSource, "the only EventSource text is in a comment")
}

func TestGlobStringInScriptKeepsNavVisible(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":       "package main\n",
		"static/app.js": "const accept = '*/*', glob = `img/*`;\nlocation.href = next;\n",
	})
	assertHas(t, ds, contracts.RuleHardNavigation)
}

func TestMinifiedScriptIsSkipped(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":                 "package main\n",
		"static/vendor.min.js":    "location.href = next;\n",
		"static/huge-line.js":     longLine("location.href=next;") + "\n",
		"node_modules/x/index.js": "location.href = next;\n",
	})
	assertNot(t, ds, contracts.RuleHardNavigation, "a minified or vendored script is code the app did not write")
}

func TestRuntimeScriptOwnsNavigation(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":                    "package main\n",
		"core-ui/runtime/src/nav.js": "if (crossesDocBoundary(path)) { location.assign(path); }\n",
		"core-ui/runtime/src/sse.js": "const es = new EventSource(sseUrl);\n",
	})
	assertNot(t, ds, contracts.RuleHardNavigation, "the runtime is the navigator")
	assertNot(t, ds, contracts.RuleBespokeEventSource, "the runtime owns the one SSE bus")
}

func TestScriptTrailingAllowWaives(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":       "package main\n",
		"static/pay.js": "location.href = providerURL; // gofastr:allow(GOFASTR1802) leaving the app for the payment provider\n",
	})
	assertNot(t, ds, contracts.RuleHardNavigation, "the trailing directive waives the line")
	assertNot(t, ds, contracts.RuleSuppressionStale, "the directive matches a finding")
}

func TestScriptFileIsNotParsedAsGo(t *testing.T) {
	dir := t.TempDir()
	seedFixture(t, dir, map[string]string{
		"go.mod":        "module example.com/app\n\ngo 1.26\n",
		"main.go":       "package main\n",
		"static/app.js": "export const x = () => 1;\n",
	})
	pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Read the pass after Run: suppressions are collected after the
	// report's count, and they ask for every file's comments.
	if un := pass.Unparsed(); report.Unparsed != 0 || len(un) != 0 {
		t.Errorf("Unparsed = %v, want none: a script is not a broken Go file", un)
	}
	for _, f := range pass.Files() {
		if f.Rel == "static/app.js" {
			t.Error("Files() handed out a script; every Go analyzer assumes it parses as Go")
		}
	}
}

func longLine(tail string) string {
	b := make([]byte, 0, 1100)
	for len(b) < 1050 {
		b = append(b, "var a=1;"...)
	}
	return string(b) + tail
}

func TestScriptLintMarkerIsKnown(t *testing.T) {
	// core-ui/check's runtimeprose lint shares the marker spelling; now
	// that verify reads script comments, its markers must not read as
	// unknown contract rules.
	ds := fixture(t, map[string]string{
		"main.go": "package main\n",
		"static/embed.js": "//gofastr:allow(runtimeprose) customer-page loader, no runtime\n" +
			"frame.title = opts.title || 'Embedded';\n",
	})
	assertNot(t, ds, contracts.RuleSuppressionUnknownRule, "runtimeprose is a repo lint")
}
