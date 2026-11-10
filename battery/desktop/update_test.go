package desktop

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/update"
)

// The updater's battery surface, tested in-package: config validation
// at New, the unconfigured refusals, the closed error codes with no
// URL in them, the scheduled check's event and its panic guard, the
// unsupported stop, and the stale-.old cleanup. The apply step's
// observation through this same seam runs under desktoptest.Run in
// update_harness_test.go (package desktop_test).

// fakeUpdatePlatform is the in-package Platform double: a fixed
// running version and bundle location, and a record of the apply
// steps.
type fakeUpdatePlatform struct {
	version string
	name    string
	dir     string

	mu         sync.Mutex
	verified   []string
	relaunched []string
	verifyErr  error
	identifier string
	// verifyHook, when set, runs inside VerifySignature (outside mu).
	verifyHook func()
}

func (f *fakeUpdatePlatform) Version() string { return f.version }

func (f *fakeUpdatePlatform) Identifier() string { return f.identifier }

func (f *fakeUpdatePlatform) Bundle() (string, string, bool) {
	if f.name == "" {
		return "", "", false
	}
	return f.name, f.dir, true
}

func (f *fakeUpdatePlatform) VerifySignature(appDir string) error {
	if f.verifyHook != nil {
		f.verifyHook()
	}
	f.mu.Lock()
	f.verified = append(f.verified, appDir)
	f.mu.Unlock()
	return f.verifyErr
}

func (f *fakeUpdatePlatform) Relaunch(appDir string) error {
	f.mu.Lock()
	f.relaunched = append(f.relaunched, appDir)
	f.mu.Unlock()
	return nil
}

// panickyPlatform panics inside Version to prove the scheduler's
// recover guard.
type panickyPlatform struct{ fakeUpdatePlatform }

func (p *panickyPlatform) Version() string { panic("boom in version read") }

// updateFeedServer serves a signed feed plus its archive.
type updateFeedServer struct {
	srv    *httptest.Server
	pubHex string
	mu     sync.Mutex
	feed   update.Feed
}

func newUpdateFeedServer(t *testing.T, version string) *updateFeedServer {
	t.Helper()
	return newUpdateFeedServerExpiring(t, version, time.Now().Add(time.Hour))
}

// newUpdateFeedServerExpiring serves a feed signed with expires as its
// expiry.
func newUpdateFeedServerExpiring(t *testing.T, version string, expires time.Time) *updateFeedServer {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	f := update.Feed{
		Expires: expires.UTC().Format(time.RFC3339),
		Version: version,
		Notes:   "What changed",
		Platforms: map[string]update.PlatformEntry{
			update.PlatformKey(): {URL: "https://example.com/Notes.zip", SHA256: strings.Repeat("0", 64), Size: 1},
		},
	}
	manifest, sig, err := update.SignFeed(f, priv)
	if err != nil {
		t.Fatal(err)
	}
	s := &updateFeedServer{pubHex: hex.EncodeToString(pub), feed: f}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			_, _ = w.Write(manifest)
		case "/manifest.json.sig":
			_, _ = w.Write(sig)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// updateTestBattery builds a battery with an Update config pointing at
// feed and a fake platform reporting version/bundle.
func updateTestBattery(t *testing.T, feedURL, pubHex, version, bundleName, bundleDir string) (*Battery, *fakeUpdatePlatform) {
	t.Helper()
	shell := newFakeShell()
	b := New(Config{
		ID:     "update.test.app",
		Title:  "UpdateTest",
		Shell:  shell,
		Logger: testLogger(t),
		Update: &UpdateConfig{FeedURL: feedURL, PublicKey: pubHex},
	})
	b.grants = newMemGrantStore()
	fp := &fakeUpdatePlatform{version: version, name: bundleName, dir: bundleDir}
	b.updaterFor().platform = fp
	return b, fp
}

func TestUpdateConfigValidation(t *testing.T) {
	good := &UpdateConfig{FeedURL: "https://updates.example.com/feed/manifest.json", PublicKey: strings.Repeat("ab", 32)}
	if err := validateUpdateConfig(good); err != nil {
		t.Fatalf("good config refused: %v", err)
	}
	loop := &UpdateConfig{FeedURL: "http://127.0.0.1:9/manifest.json", PublicKey: strings.Repeat("ab", 32)}
	if err := validateUpdateConfig(loop); err != nil {
		t.Fatalf("loopback config refused: %v", err)
	}
	bad := map[string]func(*UpdateConfig){
		"plain http":   func(c *UpdateConfig) { c.FeedURL = "http://example.com/manifest.json" },
		"no host":      func(c *UpdateConfig) { c.FeedURL = "https://" },
		"garbage url":  func(c *UpdateConfig) { c.FeedURL = "not a url" },
		"short key":    func(c *UpdateConfig) { c.PublicKey = "abcd" },
		"non-hex key":  func(c *UpdateConfig) { c.PublicKey = strings.Repeat("zz", 32) },
		"long channel": func(c *UpdateConfig) { c.Channel = strings.Repeat("a", 65) },
		"ctrl channel": func(c *UpdateConfig) { c.Channel = "a\nb" },
		"short team":   func(c *UpdateConfig) { c.MacOSTeamID = "ABCDE1234" },
		"long team":    func(c *UpdateConfig) { c.MacOSTeamID = "ABCDE123456" },
		"lower team":   func(c *UpdateConfig) { c.MacOSTeamID = "abcde12345" },
		"quoted team":  func(c *UpdateConfig) { c.MacOSTeamID = `ABCDE1234"` },
		"spaced team":  func(c *UpdateConfig) { c.MacOSTeamID = "ABCDE 1234" },
		"team and ad-hoc": func(c *UpdateConfig) {
			c.MacOSTeamID, c.MacOSAllowAdHoc = "ABCDE12345", true
		},
	}
	for name, ok := range map[string]func(*UpdateConfig){
		"team":   func(c *UpdateConfig) { c.MacOSTeamID = "ABCDE12345" },
		"ad-hoc": func(c *UpdateConfig) { c.MacOSAllowAdHoc = true },
	} {
		c := *good
		ok(&c)
		if err := validateUpdateConfig(&c); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
	for name, mutate := range bad {
		c := *good
		mutate(&c)
		if err := validateUpdateConfig(&c); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// New panics on a bad Update config, the same construction-time
	// posture as a bad menu.
	defer func() {
		if recover() == nil {
			t.Fatal("New accepted a bad Config.Update")
		}
	}()
	New(Config{ID: "x.example.app", Shell: newFakeShell(), Logger: testLogger(t),
		Update: &UpdateConfig{FeedURL: "http://example.com/m.json", PublicKey: "short"}})
}

// policyPlatform records the signing pin the battery hands the OS
// platform.
type policyPlatform struct {
	fakeUpdatePlatform
	policy update.SignaturePolicy
	set    bool
}

func (p *policyPlatform) SetSignaturePolicy(policy update.SignaturePolicy) {
	p.policy, p.set = policy, true
}

func TestSigningPinReachesPlatform(t *testing.T) {
	for name, cfg := range map[string]UpdateConfig{
		"default": {},
		"team":    {MacOSTeamID: "ABCDE12345"},
		"ad-hoc":  {MacOSAllowAdHoc: true},
	} {
		cfg.FeedURL, cfg.PublicKey = "https://updates.example.com/manifest.json", strings.Repeat("ab", 32)
		b := New(Config{ID: "update.test.app", Title: "UpdateTest", Shell: newFakeShell(), Logger: testLogger(t), Update: &cfg})
		p := &policyPlatform{}
		b.updaterFor().configurePlatform(p)
		want := update.SignaturePolicy{TeamID: cfg.MacOSTeamID, AllowAdHoc: cfg.MacOSAllowAdHoc}
		if !p.set || p.policy != want {
			t.Errorf("%s: platform got %+v (set=%v), want %+v", name, p.policy, p.set, want)
		}
	}
}

func TestCheckExpiredFeedRefused(t *testing.T) {
	feed := newUpdateFeedServerExpiring(t, "9.9.9", time.Now().Add(-time.Minute))
	b, _ := updateTestBattery(t, feed.srv.URL+"/manifest.json", feed.pubHex, "1.0.0", "Notes.app", t.TempDir())
	// A replayed manifest naming a newer version must not surface as an
	// available update once its expiry has passed.
	res, err := b.CheckForUpdates(context.Background())
	if err != errUpdateFeedExpired {
		t.Fatalf("err = %v, want errUpdateFeedExpired", err)
	}
	if res.Available || res.Version != "" {
		t.Fatalf("an expired feed still reported %+v", res)
	}
	if err := b.InstallUpdate(context.Background()); err != errUpdateFeedExpired {
		t.Fatalf("install err = %v, want errUpdateFeedExpired", err)
	}
}

func TestUpdatesCapabilityShape(t *testing.T) {
	b, _ := newTestBattery(t)
	cap, ok := b.reg.lookup("updates")
	if !ok {
		t.Fatal("updates capability not registered")
	}
	if cap.Version != 1 || len(cap.Methods) != 2 {
		t.Fatalf("capability = %+v", cap)
	}
	check, install := cap.Methods[0], cap.Methods[1]
	if check.Name != "check" || check.Permission != "" {
		t.Fatalf("check = %+v, want ungated", check)
	}
	if install.Name != "install" || install.Permission != "updates:install" {
		t.Fatalf("install = %+v, want updates:install", install)
	}
}

func TestUpdatesMethodsUnconfiguredRefuse(t *testing.T) {
	b, _ := newTestBattery(t)
	_, err := callMethod(t, b, "updates", "check", `{}`)
	if de, ok := err.(*Error); !ok || de.Code != CodeUnsupported {
		t.Fatalf("check err = %v, want unsupported", err)
	}
	_, err = callMethod(t, b, "updates", "install", `{}`)
	if de, ok := err.(*Error); !ok || de.Code != CodeUnsupported {
		t.Fatalf("install err = %v, want unsupported", err)
	}
	if _, err := b.CheckForUpdates(context.Background()); err != errUpdateNotConfigured {
		t.Fatalf("CheckForUpdates err = %v", err)
	}
	if err := b.InstallUpdate(context.Background()); err != errUpdateNotConfigured {
		t.Fatalf("InstallUpdate err = %v", err)
	}
}

func TestCheckUnbundledRefuses(t *testing.T) {
	feed := newUpdateFeedServer(t, "1.2.0")
	// No platform injected: the OS default on a test binary reports no
	// bundle... except the darwin default would try objc.Main, which a
	// Go test never drains; use the explicit "" version fake instead,
	// which is the same contract ("" never updates).
	b, fp := updateTestBattery(t, feed.srv.URL+"/manifest.json", feed.pubHex, "", "", "")
	fp.name = "" // not bundled
	res, err := b.CheckForUpdates(context.Background())
	if err == nil || res.Available {
		t.Fatalf("unbundled check = %+v, %v; want refusal", res, err)
	}
	if de, ok := err.(*Error); !ok || de.Code != CodeUnsupported {
		t.Fatalf("err = %v, want unsupported", err)
	}
	if err := b.InstallUpdate(context.Background()); err == nil {
		t.Fatal("unbundled install accepted")
	}
}

func TestCheckFeedFailureIsClosedCodeWithoutURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	pub, _, _ := ed25519.GenerateKey(nil)
	b, _ := updateTestBattery(t, srv.URL+"/manifest.json", hex.EncodeToString(pub), "1.0.0", "Notes.app", t.TempDir())
	_, err := b.CheckForUpdates(context.Background())
	if de, ok := err.(*Error); !ok || de.Code != CodeInternal {
		t.Fatalf("err = %v, want internal", err)
	}
	if strings.Contains(err.Error(), srv.URL) || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("error leaks the feed URL: %v", err)
	}
}

func TestCheckComparesAgainstRunningVersion(t *testing.T) {
	feed := newUpdateFeedServer(t, "1.2.0")
	b, _ := updateTestBattery(t, feed.srv.URL+"/manifest.json", feed.pubHex, "1.0.0", "Notes.app", t.TempDir())
	res, err := b.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Available || res.Version != "1.2.0" || res.Notes != "What changed" {
		t.Fatalf("check = %+v", res)
	}
	// A running version at or above the feed's is not an update.
	b2, _ := updateTestBattery(t, feed.srv.URL+"/manifest.json", feed.pubHex, "1.2.0", "Notes.app", t.TempDir())
	res2, err := b2.CheckForUpdates(context.Background())
	if err != nil || res2.Available {
		t.Fatalf("same-version check = %+v, %v; want not available", res2, err)
	}
}

func TestScheduledCheckEmitsEventOncePerVersion(t *testing.T) {
	feed := newUpdateFeedServer(t, "1.2.0")
	b, _ := updateTestBattery(t, feed.srv.URL+"/manifest.json", feed.pubHex, "1.0.0", "Notes.app", t.TempDir())
	fw := &fakeWindow{id: "main"}
	b.windowMu.Lock()
	b.window = fw
	b.windowMu.Unlock()
	u := b.updaterFor()

	if stop := u.checkAndNotify(context.Background()); stop {
		t.Fatal("available update stopped the loop")
	}
	evs := fw.evals()
	if len(evs) != 1 || !strings.Contains(evs[0], "update_available") ||
		!strings.Contains(evs[0], "1.2.0") {
		t.Fatalf("evals = %v", evs)
	}
	var payload struct {
		Version string `json:"version"`
		Notes   string `json:"notes"`
	}
	// The payload is a quoted JSON string inside the dispatch call.
	raw := evs[0]
	start := strings.Index(raw, "JSON.parse(") + len("JSON.parse(")
	end := strings.Index(raw[start:], ")") + start
	var quoted string
	if err := json.Unmarshal([]byte(raw[start:end]), &quoted); err != nil {
		t.Fatalf("payload not a quoted string: %v", err)
	}
	if err := json.Unmarshal([]byte(quoted), &payload); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if payload.Version != "1.2.0" || payload.Notes != "What changed" {
		t.Fatalf("payload = %+v", payload)
	}
	// The same still-available version does not re-emit.
	if stop := u.checkAndNotify(context.Background()); stop {
		t.Fatal("second check stopped the loop")
	}
	if got := len(fw.evals()); got != 1 {
		t.Fatalf("evals after second check = %d, want 1 (once per version)", got)
	}
}

func TestScheduledCheckUpToDateEmitsNothing(t *testing.T) {
	feed := newUpdateFeedServer(t, "1.0.0")
	b, _ := updateTestBattery(t, feed.srv.URL+"/manifest.json", feed.pubHex, "1.0.0", "Notes.app", t.TempDir())
	fw := &fakeWindow{id: "main"}
	b.windowMu.Lock()
	b.window = fw
	b.windowMu.Unlock()
	if stop := b.updaterFor().checkAndNotify(context.Background()); stop {
		t.Fatal("up-to-date check stopped the loop")
	}
	if got := len(fw.evals()); got != 0 {
		t.Fatalf("evals = %v, want none", fw.evals())
	}
}

func TestScheduledCheckStopsOnUnsupported(t *testing.T) {
	feed := newUpdateFeedServer(t, "1.2.0")
	b, fp := updateTestBattery(t, feed.srv.URL+"/manifest.json", feed.pubHex, "", "", "")
	fp.name = ""
	if stop := b.updaterFor().checkAndNotify(context.Background()); !stop {
		t.Fatal("unbundled host did not stop the scheduled loop")
	}
}

func TestScheduledCheckRecoversFromPanic(t *testing.T) {
	feed := newUpdateFeedServer(t, "1.2.0")
	b, _ := updateTestBattery(t, feed.srv.URL+"/manifest.json", feed.pubHex, "1.0.0", "Notes.app", t.TempDir())
	b.updaterFor().platform = &panickyPlatform{}
	// Must return (false), not take the process down.
	if stop := b.updaterFor().checkAndNotify(context.Background()); stop {
		t.Fatal("a panicked check should retry next interval, not stop")
	}
}

func TestStartUpdaterRemovesStaleOldBundle(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Notes.app")
	if err := os.MkdirAll(app, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "Notes.app.old")
	if err := os.MkdirAll(filepath.Join(old, "Contents"), 0o700); err != nil {
		t.Fatal(err)
	}
	feed := newUpdateFeedServer(t, "1.2.0")
	b, _ := updateTestBattery(t, feed.srv.URL+"/manifest.json", feed.pubHex, "1.2.0", "Notes.app", dir)
	b.startUpdater(context.Background())
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("stale .old bundle survived: %v", err)
	}
	if _, err := os.Stat(app); err != nil {
		t.Fatalf("the running bundle was removed: %v", err)
	}
}

func TestStartUpdaterNegativeIntervalStartsNoLoop(t *testing.T) {
	feed := newUpdateFeedServer(t, "1.2.0")
	pub, _, _ := ed25519.GenerateKey(nil)
	b := New(Config{
		ID:     "update.test.app",
		Shell:  newFakeShell(),
		Logger: testLogger(t),
		Update: &UpdateConfig{FeedURL: feed.srv.URL + "/manifest.json", PublicKey: hex.EncodeToString(pub), Interval: -1},
	})
	b.updaterFor().platform = &fakeUpdatePlatform{version: "1.0.0", name: "Notes.app", dir: t.TempDir()}
	b.startUpdater(context.Background())
	// Nothing to observe directly without waiting 30 s for the first
	// check; the guard is that startUpdater returns immediately (the
	// test's runtime proves no synchronous check ran).
}

// The opt-out is announced once at start, so it is not left on by
// accident in a release that has since gained a signing identity.
func TestStartUpdaterWarnsOnAdHocOptOut(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the field only acts on macOS")
	}
	for _, allow := range []bool{false, true} {
		var logs bytes.Buffer
		b := New(Config{
			ID:     "update.test.app",
			Shell:  newFakeShell(),
			Logger: slog.New(slog.NewTextHandler(&logs, nil)),
			Update: &UpdateConfig{FeedURL: "https://updates.example.com/manifest.json", PublicKey: strings.Repeat("ab", 32),
				Interval: -1, MacOSAllowAdHoc: allow},
		})
		b.updaterFor().platform = &fakeUpdatePlatform{version: "1.0.0", name: "Notes.app", dir: t.TempDir()}
		b.startUpdater(context.Background())
		if warned := strings.Contains(logs.String(), "MacOSAllowAdHoc is set"); warned != allow {
			t.Errorf("MacOSAllowAdHoc=%v: warned=%v; log: %s", allow, warned, logs.String())
		}
	}
}

// An unavailable page and an Eval refusal must both leave delivery retryable.
func TestUpdateNotifyRetriesDelivery(t *testing.T) {
	feed := newUpdateFeedServer(t, "1.2.0")
	b, _ := updateTestBattery(t, feed.srv.URL+"/manifest.json", feed.pubHex, "1.0.0", "Notes.app", t.TempDir())
	u := b.updaterFor()
	res := Update{Available: true, Version: "1.2.0", Notes: "Changes"}
	u.notifyOnce(res) // no window yet
	w := &retryUpdateWindow{fakeWindow: fakeWindow{id: "main"}, refuse: true}
	b.windowMu.Lock()
	b.window = w
	b.windowMu.Unlock()
	u.notifyOnce(res) // window exists but Eval refuses
	w.refuse = false
	u.notifyOnce(res)
	u.notifyOnce(res)
	if got := len(w.evals()); got != 1 {
		t.Fatalf("successful deliveries = %d, want 1", got)
	}
	if w.attempts != 2 {
		t.Fatalf("Eval attempts = %d, want refusal then success", w.attempts)
	}
}

type retryUpdateWindow struct {
	fakeWindow
	refuse   bool
	attempts int
}

func (w *retryUpdateWindow) Eval(js string) error {
	w.attempts++
	if w.refuse {
		return fmt.Errorf("window not ready")
	}
	return w.fakeWindow.Eval(js)
}

func TestUpdateNotifySharesInflight(t *testing.T) {
	feed := newUpdateFeedServer(t, "1.2.0")
	b, _ := updateTestBattery(t, feed.srv.URL+"/manifest.json", feed.pubHex, "1.0.0", "Notes.app", t.TempDir())
	w := &blockingUpdateWindow{started: make(chan struct{}), release: make(chan struct{})}
	b.window = w
	u := b.updaterFor()
	res := Update{Available: true, Version: "1.2.0"}
	first, second := make(chan struct{}), make(chan struct{})
	go func() { defer close(first); u.notifyOnce(res) }()
	<-w.started
	go func() { defer close(second); u.notifyOnce(res) }()
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Error("overlapping notification did not return")
	}
	close(w.release)
	<-first
	<-second
	if got := len(w.evals()); got != 1 {
		t.Fatalf("overlapping deliveries = %d, want 1", got)
	}
}

type blockingUpdateWindow struct {
	fakeWindow
	once             sync.Once
	started, release chan struct{}
}

func (w *blockingUpdateWindow) Eval(js string) error {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return w.fakeWindow.Eval(js)
}

func TestUpdateNotifyRetriesAfterPanic(t *testing.T) {
	feed := newUpdateFeedServer(t, "1.2.0")
	b, _ := updateTestBattery(t, feed.srv.URL+"/manifest.json", feed.pubHex, "1.0.0", "Notes.app", t.TempDir())
	w := &panicUpdateWindow{panics: true}
	b.window = w
	u := b.updaterFor()
	if u.checkAndNotify(context.Background()) {
		t.Fatal("panic stopped scheduled checks")
	}
	w.panics = false
	u.checkAndNotify(context.Background())
	if got := len(w.evals()); got != 1 {
		t.Fatalf("deliveries after panic = %d, want 1", got)
	}
}

type panicUpdateWindow struct {
	fakeWindow
	panics bool
}

func (w *panicUpdateWindow) Eval(js string) error {
	if w.panics {
		panic("window not ready")
	}
	return w.fakeWindow.Eval(js)
}
