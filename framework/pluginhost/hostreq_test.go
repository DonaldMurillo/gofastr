package pluginhost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"log/slog"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// captureWarnings returns a logger writing WARN+ to a buffer, so tests can
// count and inspect exactly what CheckHostRequirements emitted.
func captureWarnings() (*slog.Logger, *strings.Builder) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	return log, &buf
}

func warnCount(buf *strings.Builder) int {
	return strings.Count(buf.String(), "level=WARN")
}

func scannerModule(t *testing.T, feature string) ClientModule {
	t.Helper()
	m, err := NewClientModule("scanner", Manifest{
		Entry:            "/__gofastr/plugin/scanner/scan.html",
		Sandbox:          []string{"allow-scripts"},
		HostRequirements: []string{HostRequirementPrefix + feature},
	}, nil)
	if err != nil {
		t.Fatalf("NewClientModule: %v", err)
	}
	return m
}

// The token grammar is a closed registry: well-formed tokens pass Validate,
// everything else — unknown prefix, unknown feature, embedded whitespace or
// header syntax — is rejected at registration.
func TestManifestValidateHostRequirementGrammar(t *testing.T) {
	valid := []string{
		"permissions-policy:camera",
		"permissions-policy:geolocation",
		"permissions-policy:clipboard-write",
		"  PERMISSIONS-POLICY:Camera  ", // case + outer whitespace normalise
	}
	for _, tok := range valid {
		m := Manifest{Entry: "/e.html", HostRequirements: []string{tok}}
		if err := m.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want accepted", tok, err)
		}
	}

	invalid := []struct {
		tok    string
		wantIn string
	}{
		{"", "unknown grammar"},                                                 // empty
		{"   ", "unknown grammar"},                                              // whitespace only
		{"permissions:camera", "unknown grammar"},                               // unknown prefix
		{"capability:document:read", "unknown grammar"},                         // scope grammar, not host grammar
		{"permissions-policy:", "not a known permissions-policy feature"},       // empty feature
		{"permissions-policy:camrea", "not a known permissions-policy feature"}, // typo
		{"permissions-policy:camera; microphone=()", "not a known"},             // ';' smuggle
		{"permissions-policy:camera=(self)", "not a known"},                     // grant syntax is not a declaration
		{"permissions-policy:camera microphone", "not a known"},                 // embedded whitespace
		{"allow-scripts", "unknown grammar"},                                    // sandbox vocabulary
	}
	for _, c := range invalid {
		m := Manifest{Entry: "/e.html", HostRequirements: []string{c.tok}}
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), c.wantIn) {
			t.Errorf("Validate(%q) = %v, want rejection containing %q", c.tok, err, c.wantIn)
		}
	}

	// A manifest with no host requirements stays valid.
	if err := (Manifest{Entry: "/e.html"}).Validate(); err != nil {
		t.Fatalf("nil HostRequirements should validate, got %v", err)
	}
}

// The worked case from the issue: default-shaped policy (camera=()) plus a
// plugin declaring permissions-policy:camera → exactly one warning naming
// the plugin and the token. A sibling module whose feature is not denied
// stays silent, and its token never warns.
func TestCheckWarnsOnDeniedFeature(t *testing.T) {
	log, buf := captureWarnings()
	charts, err := NewClientModule("charts", Manifest{
		Entry:            "/c.html",
		HostRequirements: []string{"permissions-policy:fullscreen"},
	}, nil)
	if err != nil {
		t.Fatalf("NewClientModule: %v", err)
	}

	CheckHostRequirements(log, "geolocation=(), microphone=(), camera=()",
		scannerModule(t, "camera"), charts)

	out := buf.String()
	if got := warnCount(buf); got != 1 {
		t.Fatalf("want exactly 1 warning, got %d:\n%s", got, out)
	}
	for _, want := range []string{`plugin=scanner`, `requirement=permissions-policy:camera`, `camera=(self)`} {
		if !strings.Contains(out, want) {
			t.Errorf("warning should carry %q:\n%s", want, out)
		}
	}
}

// Commas inside an allowlist must not split directives: microphone=() here
// is its own directive and still denies, while camera stays granted.
func TestCheckWarnsAcrossParenCommas(t *testing.T) {
	mic, err := NewClientModule("scanner", Manifest{
		Entry:            "/s.html",
		HostRequirements: []string{"permissions-policy:microphone"},
	}, nil)
	if err != nil {
		t.Fatalf("NewClientModule: %v", err)
	}
	log, buf := captureWarnings()
	CheckHostRequirements(log, "camera=(self, https://a.example), microphone=()", mic)
	if got := warnCount(buf); got != 1 {
		t.Fatalf("want 1 warning for microphone, got %d:\n%s", got, buf.String())
	}
	if !strings.Contains(buf.String(), "requirement=permissions-policy:microphone") {
		t.Fatalf("warning should name the microphone requirement:\n%s", buf.String())
	}
}

// Silence is as load-bearing as the warning: anything that grants the
// feature, leaves it unnamed, or cannot be decided at boot must NOT warn.
func TestCheckSilentWhenGrantedOrUndecidable(t *testing.T) {
	cases := []struct {
		name   string
		policy string
		feat   string // feature required by the module; "" uses camera
	}{
		{"self grant", "camera=(self)", ""},
		{"wildcard grant", "camera=*", ""},
		{"origin-list grant", "camera=(self, https://a.example)", ""},
		{"feature absent from header", "geolocation=(), microphone=()", ""},
		{"granted amid denials", "geolocation=(), camera=(self), microphone=()", ""},
		{"bare feature without allowlist", "camera", ""},
		{"no policy configured, feature not default-denied", "", "usb"},
		{"comma inside allowlist, feature granted", "camera=(self, https://a.example), microphone=()", ""},
		{"duplicate directives, one grants", "camera=(self), camera=()", ""},
		{"duplicate directives, reverse order", "camera=(), camera=(self)", ""},
		{"malformed allowlist padding", "camera=( )", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			feat := c.feat
			if feat == "" {
				feat = "camera"
			}
			log, buf := captureWarnings()
			CheckHostRequirements(log, c.policy, scannerModule(t, feat))
			if got := warnCount(buf); got != 0 {
				t.Fatalf("policy %q with feature %q: want 0 warnings, got %d:\n%s",
					c.policy, feat, got, buf.String())
			}
		})
	}
}

// The check's contract is LOG-and-never-fail: no error return, no panic,
// for any combination of hostile policy, nil logger, and modules that
// bypassed NewClientModule (struct literals with garbage tokens).
func TestCheckNeverPanicsOnHostileInput(t *testing.T) {
	hostile := []string{
		"", "(", "))", "(())", "=()", ";;;", "====", "camera=(", "camera=)",
		"camera=();;;", "ünïcode=()", "camera=(ünïcode, self)",
		strings.Repeat("(", 64), strings.Repeat("camera=(),", 64),
		"permissions-policy:camera=()",
	}
	garbageModules := []ClientModule{
		{},
		{Name: "raw", Manifest: Manifest{HostRequirements: []string{
			"", "nope:nah", "permissions-policy:camera", "permissions-policy:",
		}}},
	}
	log, buf := captureWarnings()
	for _, policy := range hostile {
		// Denied camera in the garbage module may legitimately warn; the
		// contract under test here is that nothing panics or errors.
		CheckHostRequirements(log, policy, garbageModules...)
		CheckHostRequirements(log, policy)
	}
	// nil logger falls back to slog.Default; exercise the path with a
	// policy that stays silent so test output is not polluted.
	CheckHostRequirements(nil, "camera=(self)", garbageModules...)
	if got := warnCount(buf); got == 0 {
		t.Fatal("hostile policies included plain denials; expected at least one warning, got none — the loop body never ran the check?")
	}
}

// An empty configured policy means the middleware's DEFAULT header, which
// denies camera/microphone/geolocation. The mirror must match what
// core/middleware actually emits: derive the real header by running the
// middleware once, then require identical warning behaviour for "" and for
// the emitted bytes, plus a warning for each default-denied feature.
func TestCheckHostRequirementsEmptyMeansDefault(t *testing.T) {
	rec := httptest.NewRecorder()
	mw := middleware.SecurityHeaders(middleware.SecurityHeadersConfig{})
	mw(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	emitted := rec.Header().Get("Permissions-Policy")
	if emitted == "" {
		t.Fatal("SecurityHeaders with empty config emitted no Permissions-Policy")
	}
	if !strings.Contains(emitted, "camera=()") {
		t.Fatalf("middleware default no longer denies camera; update defaultPermissionsPolicy mirror:\n%s", emitted)
	}

	for _, feat := range []string{"camera", "microphone", "geolocation"} {
		logEmpty, bufEmpty := captureWarnings()
		CheckHostRequirements(logEmpty, "", scannerModule(t, feat))
		logReal, bufReal := captureWarnings()
		CheckHostRequirements(logReal, emitted, scannerModule(t, feat))

		if warnCount(bufEmpty) != 1 || warnCount(bufReal) != 1 {
			t.Errorf("%s: empty-config and real-header runs must each warn exactly once; got %d and %d\n%s\n%s",
				feat, warnCount(bufEmpty), warnCount(bufReal), bufEmpty.String(), bufReal.String())
		}
	}
}
