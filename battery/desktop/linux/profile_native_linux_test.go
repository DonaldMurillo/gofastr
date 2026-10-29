//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

const (
	profileNativeRole  = "GOFASTR_PROFILE_CHILD"
	profileNativeRoot  = "GOFASTR_PROFILE_FIXTURE"
	profileNativeID    = "dev.gofastr.native-profile"
	profileNativeTitle = "Persistent profile native fixture"
)

// This parent never initializes GTK. Each role runs in a fresh process, with
// real Battery.Run and one native lifetime. Run this UNEMBEDDED test binary in
// an authenticated graphical session; no OS handler or desktop state is changed.
func TestNativeProfile(t *testing.T) {
	if os.Getenv("GOFASTR_LINUX_NATIVE") != "1" || os.Getenv("GOFASTR_LINUX_PROFILE") != "1" {
		t.Skip("set GOFASTR_LINUX_NATIVE=1 and GOFASTR_LINUX_PROFILE=1 in a graphical session")
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Fatal("native profile test requires an existing graphical display")
	}
	root := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 210*time.Second)
	defer cancel()
	run := func(role string) {
		t.Helper()
		output, err := runProfileChild(ctx, root, role)
		previous, _ := readProfileNativeEvidence(root)
		redacted := profileSafeDiagnostic(string(output), previous)
		if outputDir := os.Getenv("GOFASTR_LINUX_TEST_OUTPUT"); outputDir != "" {
			logs := filepath.Join(outputDir, "profile-logs")
			if mkdirErr := os.MkdirAll(logs, 0700); mkdirErr != nil {
				t.Fatal("PROFILE: cannot create redacted log directory")
			}
			if writeErr := fileperm.WriteOwnerOnly(filepath.Join(logs, role+".log"), []byte(redacted)); writeErr != nil {
				t.Fatal("PROFILE: cannot save redacted child log")
			}
		}
		if err != nil {
			// Child/native diagnostics can include a navigation URL. Keep raw logs
			// private, and print only our bounded, credential-free assertions.
			logPath := filepath.Join(root, role+".log")
			_ = fileperm.WriteOwnerOnly(logPath, output)
			var safe []string
			for _, line := range strings.Split(redacted, "\n") {
				if strings.Contains(line, "PROFILE:") {
					safe = append(safe, line)
				}
			}
			t.Fatalf("profile child %s failed (%v); assertions: %s", role, err, strings.Join(safe, "; "))
		}
		if !bytes.Contains(output, []byte("--- PASS: TestProfileNativeChild")) {
			t.Fatalf("profile child %s did not execute its assertions", role)
		}
		t.Logf("PROFILE %s: fresh-process assertions passed", role)
	}
	run("write")
	profileDir := filepath.Join(root, "appdata", "browser-profiles", profileNativeID)
	if err := profileCookieDiskEvidence(ctx, profileDir); err != nil {
		t.Fatal("PROFILE: persistent cookie disk witness failed: " + err.Error())
	}
	if err := profileWorkerDiskEvidence(ctx, profileDir); err != nil {
		t.Fatal("PROFILE: worker disk witness failed: " + err.Error())
	}
	t.Log("PROFILE disk witness: non-session cookie and worker registration exist; session/boot/deleted cookies absent")
	run("read")
	run("other-id")
	run("ephemeral-write")
	run("ephemeral-read")
	if _, err := os.Stat(filepath.Join(root, "ephemeral-data", "browser-profiles")); !os.IsNotExist(err) {
		t.Fatal("PROFILE: default ephemeral launches created a persistent browser profile")
	}
	previous, err := readProfileNativeEvidence(root)
	if err != nil {
		t.Fatal("PROFILE: private origin evidence unavailable")
	}
	u, err := url.Parse(previous.Origin)
	if err != nil {
		t.Fatal("PROFILE: invalid private origin evidence")
	}
	listener, err := net.Listen("tcp", u.Host)
	if err != nil {
		t.Fatal("PROFILE: could not occupy retained fixture port")
	}
	defer listener.Close()
	run("occupied-port")
}

var profileDiagnosticURL = regexp.MustCompile(`https?://[^\s]+`)

func profileSafeDiagnostic(message string, evidence ...profileNativeEvidence) string {
	for _, item := range evidence {
		for _, secret := range []string{item.BootURL, item.SessionName, item.SessionValue} {
			if secret != "" {
				message = strings.ReplaceAll(message, secret, "<credential redacted>")
			}
		}
		if parsed, err := url.Parse(item.BootURL); err == nil && parsed.Query().Get("t") != "" {
			message = strings.ReplaceAll(message, parsed.Query().Get("t"), "<boot token redacted>")
		}
	}
	return profileDiagnosticURL.ReplaceAllString(message, "<URL redacted>")
}

func runProfileChild(ctx context.Context, root, role string) ([]byte, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	childCtx, cancel := context.WithTimeout(ctx, 65*time.Second)
	defer cancel()
	cmd := exec.CommandContext(childCtx, executable, "-test.run=^TestProfileNativeChild$", "-test.v", "-test.timeout=60s")
	cmd.WaitDelay = 3 * time.Second
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, profileNativeRole+"=") && !strings.HasPrefix(value, profileNativeRoot+"=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, profileNativeRole+"="+role, profileNativeRoot+"="+root)
	return cmd.CombinedOutput()
}

// A secret-bearing witness is confined to this test's private temporary tree.
// Neither credential values nor URLs containing tokens are diagnostic output.
type profileNativeEvidence struct {
	BootURL, Origin, SessionName, SessionValue string
}

func readProfileNativeEvidence(root string) (profileNativeEvidence, error) {
	var evidence profileNativeEvidence
	data, err := os.ReadFile(filepath.Join(root, "credentials.json"))
	if err != nil {
		return evidence, err
	}
	err = json.Unmarshal(data, &evidence)
	return evidence, err
}

type profileNativeShell struct {
	*linuxShell
	mu       sync.Mutex
	evidence profileNativeEvidence
	previous profileNativeEvidence
	runs     atomic.Int32
	bootErr  error
}

func (s *profileNativeShell) Run(ctx context.Context, cfg desktop.WindowConfig, ready func(desktop.Window)) error {
	s.runs.Add(1)
	return s.linuxShell.Run(ctx, cfg, func(w desktop.Window) {
		ready(&profileNativeWindow{linuxWindow: w.(*linuxWindow), owner: s})
	})
}

type profileNativeWindow struct {
	*linuxWindow
	owner *profileNativeShell
}

func (w *profileNativeWindow) Navigate(raw string) error {
	u, err := url.Parse(raw)
	if err == nil && u.Path == "/__gofastr/desktop/enter" {
		s := w.owner
		s.mu.Lock()
		s.evidence.BootURL = raw
		s.evidence.Origin = u.Scheme + "://" + u.Host
		s.mu.Unlock()
		if s.previous.Origin != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			s.bootErr = refusePriorProfileCredentials(ctx, s.previous, u.Scheme+"://"+u.Host)
			cancel()
			if s.bootErr != nil {
				return s.bootErr
			}
		}
	}
	return w.linuxWindow.Navigate(raw)
}

type profileNativeScreen struct{}

func (profileNativeScreen) Render() render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text(profileNativeTitle))
}

func TestProfileNativeChild(t *testing.T) {
	role, root := os.Getenv(profileNativeRole), os.Getenv(profileNativeRoot)
	if role == "" {
		t.Skip("native profile subprocess helper")
	}
	if os.Getenv("GOFASTR_LINUX_NATIVE") != "1" || os.Getenv("GOFASTR_LINUX_PROFILE") != "1" || !filepath.IsAbs(root) {
		t.Fatal("PROFILE: subprocess requires both native opt-ins and its private fixture root")
	}
	switch role {
	case "write", "read", "other-id", "ephemeral-write", "ephemeral-read", "locked", "occupied-port":
	default:
		t.Fatal("PROFILE: unknown subprocess role")
	}
	t.Setenv(desktop.ManifestEnv, "")
	t.Setenv("GOFASTR_ISOLATION", "off")
	t.Setenv("GOFASTR_DOTENV", "off")
	persistent := !strings.HasPrefix(role, "ephemeral-")
	id, dataDir := profileNativeID, filepath.Join(root, "appdata")
	if role == "other-id" {
		id += ".other"
	}
	if !persistent {
		id += ".ephemeral"
		dataDir = filepath.Join(root, "ephemeral-data")
	}
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", dataDir)
	shell := &profileNativeShell{linuxShell: New().(*linuxShell)}
	if role == "read" {
		previous, err := readProfileNativeEvidence(root)
		if err != nil {
			t.Fatal("PROFILE: previous private credential witness unavailable")
		}
		shell.previous = previous
	}
	site := appui.NewApp(profileNativeTitle)
	layout := appui.NewLayout("main", appui.LayoutSpec{}, func(_ context.Context, l *appui.LayoutTree) render.HTML {
		return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage}, l.Primary())
	})
	site.Register("/", profileNativeScreen{}, layout)
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "profile-native"}))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app.SetLogger(logger)
	app.Mount(uihost.New(site))
	installProfileFixtureRoutes(app, shell)
	battery := desktop.New(desktop.Config{ID: id, Title: profileNativeTitle, DataDir: dataDir, Shell: shell,
		PersistentProfile: persistent, KeepIsolation: true, Logger: logger})
	app.RegisterBattery(battery)
	if role == "locked" || role == "occupied-port" {
		err := battery.Run(app)
		want := "in use"
		if role == "occupied-port" {
			want = "app failed to start"
		}
		if err == nil || !strings.Contains(err.Error(), want) || shell.runs.Load() != 0 {
			t.Fatalf("PROFILE: %s was not rejected before native shell entry", role)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		defer shell.Quit()
		result <- exerciseProfileNative(ctx, role, root, id, dataDir, battery, shell)
	}()
	watchdog := time.AfterFunc(50*time.Second, shell.Quit)
	defer watchdog.Stop()
	runErr := battery.Run(app)
	cancel()
	if runErr != nil {
		// Do not include arbitrary native errors, which may contain boot URLs.
		shell.mu.Lock()
		evidence := shell.evidence
		shell.mu.Unlock()
		t.Fatal("PROFILE: Battery.Run failed: " + profileSafeDiagnostic(runErr.Error(), evidence, shell.previous))
	}
	if err := <-result; err != nil {
		t.Fatal("PROFILE: " + err.Error())
	}
	if shell.runs.Load() != 1 {
		t.Fatal("PROFILE: expected one real GTK shell lifetime")
	}
}

func installProfileFixtureRoutes(app *framework.App, shell *profileNativeShell) {
	app.Router().HandleFunc(http.MethodGet, "/__profile/probe", func(w http.ResponseWriter, r *http.Request) {
		oldSession := false
		for _, cookie := range r.Cookies() {
			if strings.HasPrefix(cookie.Name, "__gofastr_desktop_") {
				shell.mu.Lock()
				shell.evidence.SessionName, shell.evidence.SessionValue = cookie.Name, cookie.Value
				shell.mu.Unlock()
			}
			oldSession = oldSession || cookie.Name == shell.previous.SessionName
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		if oldSession {
			_, _ = io.WriteString(w, `{"oldSession":true}`)
		} else {
			_, _ = io.WriteString(w, `{"oldSession":false}`)
		}
	})
	app.Router().HandleFunc(http.MethodGet, "/__profile/seed", func(w http.ResponseWriter, _ *http.Request) {
		//gofastr:allow(security/insecure-cookie) disposable native test's HTTP loopback origin only
		http.SetCookie(w, &http.Cookie{Name: "profile-kept", Value: "kept", Path: "/", MaxAge: 3600, SameSite: http.SameSiteStrictMode})
		//gofastr:allow(security/insecure-cookie) disposable native test's HTTP loopback origin only
		http.SetCookie(w, &http.Cookie{Name: "profile-session", Value: "temporary", Path: "/", SameSite: http.SameSiteStrictMode})
		//gofastr:allow(security/insecure-cookie) disposable native test's HTTP loopback origin only
		http.SetCookie(w, &http.Cookie{Name: "profile-deleted", Value: "temporary", Path: "/", MaxAge: 3600, SameSite: http.SameSiteStrictMode})
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
	app.Router().HandleFunc(http.MethodGet, "/__profile/expire", func(w http.ResponseWriter, _ *http.Request) {
		//gofastr:allow(security/insecure-cookie) disposable native test's HTTP loopback origin only
		http.SetCookie(w, &http.Cookie{Name: "profile-deleted", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), SameSite: http.SameSiteStrictMode})
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
	app.Router().HandleFunc(http.MethodGet, "/__profile/sw.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Service-Worker-Allowed", "/")
		_, _ = io.WriteString(w, `self.addEventListener('install', e => e.waitUntil(self.skipWaiting()));
self.addEventListener('activate', e => e.waitUntil(self.clients.claim()));
self.addEventListener('fetch', e => {
 const path = new URL(e.request.url).pathname;
 if (path === '/__gofastr/desktop/enter' || path === '/__profile/sw-sentinel')
  e.respondWith(new Response('profile worker interception witness', {status:418}));
});`)
	})
	app.Router().HandleFunc(http.MethodGet, "/__profile/sw-sentinel", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
}

type profilePageObservation struct {
	Ready                                           bool
	Origin, Heading, Local                          string
	Kept, Session, Deleted, BootVisible, Controlled bool
	Registrations                                   int
	ProbeStatus                                     int
	OldSession                                      bool
	Title                                           string
}

func observeProfilePage(ctx context.Context, evaluator desktop.PageEvaluator) (profilePageObservation, error) {
	var state profilePageObservation
	raw, err := evaluator.EvalAsync(ctx, `const ready = !!(window.__gofastr?.desktop?.manifest);
if (!ready) return {ready:false};
const response = await fetch('/__profile/probe', {cache:'no-store'});
const probe = response.status === 200 ? await response.json() : {};
const title = await __gofastr.desktop.window.title();
const cookies = document.cookie.split(';').map(s => s.trim());
return {ready, origin:location.origin, heading:document.querySelector('h1')?.textContent || '',
 local:localStorage.getItem('profile-persisted') || '', kept:cookies.includes('profile-kept=kept'),
 session:cookies.includes('profile-session=temporary'), deleted:cookies.some(c => c.startsWith('profile-deleted=')),
 bootVisible:document.cookie.includes('__gofastr_desktop'), controlled:!!navigator.serviceWorker?.controller,
 registrations:navigator.serviceWorker ? (await navigator.serviceWorker.getRegistrations()).length : -1,
 probeStatus:response.status, oldSession:probe.oldSession === true, title:title.title};`)
	if err != nil || json.Unmarshal(raw, &state) != nil {
		return state, errors.New("native profile page observation unavailable")
	}
	return state, nil
}

func exerciseProfileNative(ctx context.Context, role, root, id, dataDir string, battery *desktop.Battery, shell *profileNativeShell) error {
	var evaluator desktop.PageEvaluator
	var state profilePageObservation
	if err := pollProfile(ctx, func() bool {
		window, ok := battery.Window()
		if !ok {
			return false
		}
		evaluator, _ = window.(desktop.PageEvaluator)
		if evaluator == nil {
			return false
		}
		probe, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		var err error
		state, err = observeProfilePage(probe, evaluator)
		return err == nil && state.Ready && state.Heading == profileNativeTitle
	}); err != nil {
		return errors.New("fresh native boot/bridge did not become ready (worker interception is a failure)")
	}
	if state.ProbeStatus != 200 || state.Title != profileNativeTitle || state.BootVisible || state.OldSession || state.Controlled || (state.Registrations != 0 && !(strings.HasPrefix(role, "ephemeral-") && state.Registrations == -1)) {
		return errors.New("fresh bridge/session/HttpOnly or pre-boot worker clearing assertion failed")
	}
	if role == "read" {
		if state.Origin != shell.previous.Origin || !state.Kept || state.Local != "stored" || state.Session || state.Deleted {
			return errors.New("fresh-process cookie/localStorage/origin persistence or session/expiry semantics failed")
		}
	} else if state.Kept || state.Local != "" || state.Session || state.Deleted {
		return errors.New("fresh or isolated profile unexpectedly inherited website data")
	}
	if role == "write" || role == "ephemeral-write" {
		raw, err := evaluator.EvalAsync(ctx, `await fetch('/__profile/seed', {cache:'no-store'});
localStorage.setItem('profile-persisted', 'stored');
const seeded = document.cookie.includes('profile-deleted=temporary') && document.cookie.includes('profile-session=temporary') && document.cookie.includes('profile-kept=kept');
await fetch('/__profile/expire', {cache:'no-store'});
return seeded && !document.cookie.includes('profile-deleted=');`)
		if err != nil || string(raw) != "true" {
			return errors.New("native cookie seed/session/expiry deletion prerequisite failed")
		}
	}
	if role == "write" {
		if output, err := runProfileChild(ctx, root, "locked"); err != nil || !bytes.Contains(output, []byte("--- PASS: TestProfileNativeChild")) {
			return errors.New("second live process was not refused by profile lease")
		}
		fmt.Println("PROFILE: lock contender refused before native shell entry")
		shell.mu.Lock()
		evidence := shell.evidence
		shell.mu.Unlock()
		if evidence.SessionName == "" || evidence.SessionValue == "" || evidence.BootURL == "" || evidence.Origin != state.Origin {
			return errors.New("real native boot/session witness missing")
		}
		data, err := json.Marshal(evidence)
		if err != nil || fileperm.WriteOwnerOnly(filepath.Join(root, "credentials.json"), data) != nil {
			return errors.New("could not save private credential witness")
		}
		if _, err := evaluator.EvalAsync(ctx, `await navigator.serviceWorker.register('/__profile/sw.js', {scope:'/'}); await navigator.serviceWorker.ready; return true;`); err != nil {
			return errors.New("service-worker registration prerequisite failed")
		}
		if err := pollProfile(ctx, func() bool {
			raw, err := evaluator.EvalAsync(ctx, `return !!navigator.serviceWorker.controller && (await fetch('/__profile/sw-sentinel', {cache:'no-store'})).status === 418;`)
			return err == nil && string(raw) == "true"
		}); err != nil {
			return errors.New("service-worker interception prerequisite was not demonstrated")
		}
		profileDir := filepath.Join(dataDir, "browser-profiles", id)
		if err := pollProfile(ctx, func() bool {
			return profileWorkerDiskEvidence(ctx, profileDir) == nil && profileCookieDiskEvidence(ctx, profileDir) == nil
		}); err != nil {
			return errors.New("native writes did not produce the required on-disk cookie/worker witnesses")
		}
	}
	if role == "read" {
		fmt.Println("PROFILE: retained origin, cookie/localStorage, session/expiry exclusion, stale credentials and worker clearing passed")
		return profileNativeSnapshot(ctx, battery)
	}
	return nil
}

func pollProfile(ctx context.Context, check func() bool) error {
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for {
		if check() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func refusePriorProfileCredentials(ctx context.Context, old profileNativeEvidence, origin string) error {
	if old.Origin != origin || old.BootURL == "" || old.SessionName == "" || old.SessionValue == "" {
		return errors.New("persistent origin changed or private credential witness is missing")
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	for _, probe := range []struct {
		path   string
		cookie bool
	}{{"/", false}, {"/", true}, {"/__gofastr/desktop/call/window/title", true}, {old.BootURL, false}} {
		method, endpoint := http.MethodGet, origin+probe.path
		if strings.HasPrefix(probe.path, "http://") {
			endpoint = probe.path
		}
		if strings.Contains(probe.path, "/call/") {
			method = http.MethodPost
		}
		request, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader("{}"))
		if err != nil {
			return errors.New("stale credential request construction failed")
		}
		request.Header.Set("Content-Type", "application/json")
		if probe.cookie {
			request.AddCookie(&http.Cookie{Name: old.SessionName, Value: old.SessionValue})
		}
		response, err := client.Do(request)
		if err != nil {
			return errors.New("stale credential refusal transport failed")
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			return errors.New("old boot token/session or cookieless request was accepted before fresh boot")
		}
	}
	return nil
}

func profileReadCount(ctx context.Context, path, query string, args ...any) (int, error) {
	u := &url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return 0, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var count int
	err = db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func profileCookieDiskEvidence(ctx context.Context, dir string) error {
	path := filepath.Join(dir, "cookies.sqlite")
	kept, err := profileReadCount(ctx, path, "SELECT count(*) FROM moz_cookies WHERE name = ? AND value = ?", "profile-kept", "kept")
	if err != nil || kept != 1 {
		return errors.New("non-session cookie is not in the native SQLite store")
	}
	for _, name := range []string{"profile-session", "profile-deleted", "__gofastr_desktop%"} {
		count, err := profileReadCount(ctx, path, "SELECT count(*) FROM moz_cookies WHERE name LIKE ?", name)
		if err != nil || count != 0 {
			return errors.New("session, boot, or expired/deleted cookie reached persistent SQLite storage")
		}
	}
	return nil
}

// This is a test-only non-vacuity witness, not a supported storage format or
// production corruption check. WebKit's SWRegistrationDatabase uses Records
// in ServiceWorkerRegistrations-*.sqlite3; if that changes, this test must be
// updated rather than silently skipping its persisted-worker prerequisite.
func profileWorkerDiskEvidence(ctx context.Context, dir string) error {
	found := false
	err := filepath.WalkDir(filepath.Join(dir, "data"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), "ServiceWorkerRegistrations-") && strings.HasSuffix(entry.Name(), ".sqlite3") {
			count, err := profileReadCount(ctx, path, "SELECT count(*) FROM Records WHERE scriptURL LIKE ?", "%/__profile/sw.js")
			if err == nil && count > 0 {
				found = true
			}
		}
		return nil
	})
	if err != nil || !found {
		return errors.New("no persisted service-worker registration witness")
	}
	return nil
}

func profileNativeSnapshot(ctx context.Context, battery *desktop.Battery) error {
	window, ok := battery.Window()
	if !ok {
		return errors.New("native profile window disappeared before snapshot")
	}
	data, err := window.Snapshot(ctx)
	if err != nil {
		return errors.New("native profile snapshot failed")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 100 || config.Height < 100 {
		return errors.New("native profile snapshot is not a window-sized PNG")
	}
	if output := os.Getenv("GOFASTR_LINUX_TEST_OUTPUT"); output != "" {
		if err := os.MkdirAll(output, 0700); err != nil {
			return errors.New("cannot create native profile snapshot directory")
		}
		if err := fileperm.WriteOwnerOnly(filepath.Join(output, "profile-native.png"), data); err != nil {
			return errors.New("cannot save native profile snapshot")
		}
	}
	fmt.Printf("PROFILE native snapshot validated: %dx%d\n", config.Width, config.Height)
	return nil
}
