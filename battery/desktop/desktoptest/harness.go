package desktoptest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Harness runs an app through the desktop battery's REAL Run flow with
// the fake Shell in place of the OS: the loopback listener, the boot
// token, the session cookie, the Host pin, the frozen registry, the
// menu and tray wiring, and the chokepoint all run exactly as in a
// window, and the harness plays the two sides the OS normally plays.
//
// The page side: Get/Post/Call issue requests the way the WebView
// would, carrying the window's session cookie (a Stranger client has
// none). The native side: ClickMenu, ClickTray, PressKey, OpenSettings,
// CloseWindow, CloseMainWindow, and Answer are the user's hands on the
// menus, the tray, the window chrome, and the permission alerts.
// Everything that crossed the shell is observable: Notifications,
// Events, Navigations, WindowIDs, and the Shell's own recorders.
//
//	shell := desktoptest.NewShell()
//	app, d := buildApp(shell) // desktop.Config{Shell: shell, ...}
//	h := desktoptest.Run(t, app, d)
//	h.Get("/").AssertStatus(t, 200)
//	h.ClickMenu("File", "New note")
//	h.Wait("the navigation", func() bool { return len(h.Navigations()) == 1 })
//	var out struct{ Title string }
//	h.Call("window", "title", nil).MustResult(t, &out)
//
// Run quits the app and waits for Battery.Run to return in t.Cleanup.
type Harness struct {
	t testing.TB

	// App is the app under test; Battery its desktop battery; Shell
	// the fake shell the battery was configured with.
	App     *framework.App
	Battery *desktop.Battery
	Shell   *Shell

	addr    string
	session *http.Cookie
	client  *http.Client
	// bootRedirect is the Location the enter handshake answered with
	// ("/", or the main window's remembered path).
	bootRedirect string

	runErr     chan error
	quitOnce   sync.Once
	quitResult error

	// eventsSeen is WaitEvent's cursor into Events.
	eventsSeen int
}

// pollInterval and waitTimeout bound Wait.
const (
	pollInterval = 10 * time.Millisecond
	waitTimeout  = 10 * time.Second
	quitTimeout  = 30 * time.Second
)

// dataDirEnv is the battery's data-dir override (desktop.DataDir).
const dataDirEnv = "GOFASTR_DESKTOP_DATA_DIR"

// Run starts d.Run(app) on a goroutine, waits for the fake window to
// receive the boot navigation, walks the single-use token handshake
// as the WebView would, and returns the harness holding the resulting
// session. d must have been constructed with a *Shell (Config.Shell).
//
// When GOFASTR_DESKTOP_DATA_DIR is unset it is pointed at t.TempDir()
// so a suite never writes under the developer's Application Support;
// an app whose main assembles desktop.AppOptions before Run must set
// the variable itself, before that call.
func Run(t testing.TB, app *framework.App, d *desktop.Battery) *Harness {
	t.Helper()
	shell, ok := d.Shell().(*Shell)
	if !ok {
		t.Fatalf("desktoptest.Run: the battery's Shell is %T; construct it with desktop.Config{Shell: desktoptest.NewShell()}", d.Shell())
	}
	if os.Getenv(dataDirEnv) == "" {
		t.Setenv(dataDirEnv, t.TempDir())
	}
	h := &Harness{t: t, App: app, Battery: d, Shell: shell, runErr: make(chan error, 1)}
	go func() { h.runErr <- d.Run(app) }()
	t.Cleanup(func() {
		if err := h.Quit(); err != nil {
			t.Errorf("desktoptest: Battery.Run returned %v", err)
		}
	})

	var enterURL string
	h.Wait("the window to open and load the boot URL", func() bool {
		select {
		case err := <-h.runErr:
			h.runErr <- err
			t.Fatalf("desktoptest.Run: Battery.Run returned before the window opened: %v", err)
		default:
		}
		w := shell.Window(mainWindowID)
		if w == nil {
			return false
		}
		urls := w.NavURLs()
		if len(urls) == 0 {
			return false
		}
		enterURL = urls[0]
		return true
	})
	u, err := url.Parse(enterURL)
	if err != nil || u.Host == "" {
		t.Fatalf("desktoptest.Run: boot URL %q is not absolute", enterURL)
	}
	h.addr = u.Host

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("desktoptest.Run: cookie jar: %v", err)
	}
	h.client = &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: waitTimeout,
	}
	resp, err := h.client.Get(enterURL)
	if err != nil {
		t.Fatalf("desktoptest.Run: boot handshake: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("desktoptest.Run: boot handshake answered %d, want 302", resp.StatusCode)
	}
	h.bootRedirect = resp.Header.Get("Location")
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			h.session = c
		}
	}
	if h.session == nil {
		t.Fatal("desktoptest.Run: boot handshake set no session cookie")
	}
	return h
}

// mainWindowID and sessionCookieName mirror the battery's constants;
// the harness asserts against them rather than importing internals.
const (
	mainWindowID      = "main"
	sessionCookieName = "__gofastr_desktop"
)

// Addr is the loopback host:port the app is listening on.
func (h *Harness) Addr() string { return h.addr }

// URL joins path onto the app's loopback origin.
func (h *Harness) URL(path string) string { return "http://" + h.addr + path }

// SessionCookie is the window's session cookie, for a client the
// harness does not own (a real browser driven through CDP, say).
func (h *Harness) SessionCookie() *http.Cookie {
	c := *h.session
	return &c
}

// Client is the page's HTTP client: it carries the session cookie and
// never follows redirects.
func (h *Harness) Client() *http.Client { return h.client }

// Stranger is a client with no session: another local process that
// found the port. The gate must refuse everything it sends.
func (h *Harness) Stranger() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: waitTimeout,
	}
}

// ----- the page side ------------------------------------------------------

// Response is one recorded reply.
type Response struct {
	Status int
	Header http.Header
	Body   string
	err    error
}

// AssertStatus fails the test unless the status matches. Chainable.
func (r *Response) AssertStatus(t testing.TB, want int) *Response {
	t.Helper()
	if r.err != nil {
		t.Fatalf("request failed: %v", r.err)
	}
	if r.Status != want {
		t.Fatalf("status = %d, want %d; body: %.400s", r.Status, want, r.Body)
	}
	return r
}

// AssertContains fails the test unless the body contains substr.
func (r *Response) AssertContains(t testing.TB, substr string) *Response {
	t.Helper()
	if r.err != nil {
		t.Fatalf("request failed: %v", r.err)
	}
	if !strings.Contains(r.Body, substr) {
		t.Fatalf("body does not contain %q: %.400s", substr, r.Body)
	}
	return r
}

// JSON decodes the body into v.
func (r *Response) JSON(v any) error {
	if r.err != nil {
		return r.err
	}
	return json.Unmarshal([]byte(r.Body), v)
}

// Get issues a GET from the window.
func (h *Harness) Get(path string) *Response {
	return h.request(http.MethodGet, path, nil)
}

// Post issues a JSON POST from the window (what the page's islands and
// forms send to the app's own routes).
func (h *Harness) Post(path string, body any) *Response {
	return h.request(http.MethodPost, path, body)
}

// Put issues a JSON PUT from the window.
func (h *Harness) Put(path string, body any) *Response {
	return h.request(http.MethodPut, path, body)
}

// Delete issues a DELETE from the window.
func (h *Harness) Delete(path string) *Response {
	return h.request(http.MethodDelete, path, nil)
}

// Do sends an arbitrary request from the window: the session cookie
// is attached by the client's jar (the request URL must be on the
// app's origin, see URL).
func (h *Harness) Do(req *http.Request) *Response {
	return h.send(h.client, req)
}

func (h *Harness) request(method, path string, body any) *Response {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return &Response{err: err}
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, h.URL(path), r)
	if err != nil {
		return &Response{err: err}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// What a same-origin fetch from the page carries.
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	return h.send(h.client, req)
}

func (h *Harness) send(c *http.Client, req *http.Request) *Response {
	resp, err := c.Do(req)
	if err != nil {
		return &Response{err: err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Response{Status: resp.StatusCode, Header: resp.Header, err: err}
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: string(data)}
}

// CallResult is one bridge call's outcome, decoded from the
// chokepoint's envelope: OK with Result, or Code and Message.
type CallResult struct {
	Status  int
	OK      bool
	Result  json.RawMessage
	Code    string
	Message string
	err     error
}

// AssertOK fails the test unless the call succeeded. Chainable.
func (c *CallResult) AssertOK(t testing.TB) *CallResult {
	t.Helper()
	if c.err != nil {
		t.Fatalf("bridge call failed: %v", c.err)
	}
	if !c.OK {
		t.Fatalf("bridge call rejected: %d %s: %s", c.Status, c.Code, c.Message)
	}
	return c
}

// AssertCode fails the test unless the call was rejected with code.
func (c *CallResult) AssertCode(t testing.TB, code string) *CallResult {
	t.Helper()
	if c.err != nil {
		t.Fatalf("bridge call failed: %v", c.err)
	}
	if c.OK {
		t.Fatalf("bridge call succeeded, want rejection %q: %s", code, c.Result)
	}
	//gofastr:allow(secretcompare) Code is the bridge's closed error code (denied, not_found, ...), not a credential
	if c.Code != code {
		t.Fatalf("bridge call rejected with %q (%d: %s), want %q", c.Code, c.Status, c.Message, code)
	}
	return c
}

// MustResult asserts success and decodes the result into v.
func (c *CallResult) MustResult(t testing.TB, v any) {
	t.Helper()
	c.AssertOK(t)
	if err := json.Unmarshal(c.Result, v); err != nil {
		t.Fatalf("bridge result %s does not decode into %T: %v", c.Result, v, err)
	}
}

// Call invokes a capability method the way the page's bridge does: a
// JSON POST to the chokepoint with the session cookie and the calling
// window's header. The caller is the MAIN window's page; CallFrom
// plays any other window. input nil sends an empty object.
func (h *Harness) Call(capability, method string, input any) *CallResult {
	return h.CallFrom(mainWindowID, capability, method, input)
}

// CallFrom invokes a capability method as the page in windowID does:
// the same POST plus X-Gofastr-Window: windowID. The id is exactly
// what the bridge reads from a real page, a claim it does not verify,
// so a test can send even a grammatical id no window has.
func (h *Harness) CallFrom(windowID, capability, method string, input any) *CallResult {
	if input == nil {
		input = map[string]any{}
	}
	data, err := json.Marshal(input)
	if err != nil {
		return &CallResult{err: err}
	}
	req, err := http.NewRequest(http.MethodPost,
		h.URL("/__gofastr/desktop/call/"+capability+"/"+method), bytes.NewReader(data))
	if err != nil {
		return &CallResult{err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gofastr-Window", windowID)
	resp := h.send(h.client, req)
	if resp.err != nil {
		return &CallResult{err: resp.err}
	}
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &env); err != nil {
		return &CallResult{Status: resp.Status, err: fmt.Errorf("bridge envelope %.200q: %w", resp.Body, err)}
	}
	return &CallResult{
		Status:  resp.Status,
		OK:      env.OK,
		Result:  env.Result,
		Code:    env.Error.Code,
		Message: env.Error.Message,
	}
}

// Manifest fetches the frozen capability manifest the page's bridge
// is generated from.
func (h *Harness) Manifest() desktop.Manifest {
	h.t.Helper()
	var m desktop.Manifest
	resp := h.Get("/__gofastr/desktop/manifest.json").AssertStatus(h.t, http.StatusOK)
	if err := resp.JSON(&m); err != nil {
		h.t.Fatalf("manifest does not decode: %v", err)
	}
	return m
}

// ----- the native side ----------------------------------------------------

// config is the WindowConfig Run received.
func (h *Harness) config() desktop.WindowConfig {
	h.t.Helper()
	cfg, ok := h.Shell.Config()
	if !ok {
		h.t.Fatal("desktoptest: the shell has not run")
	}
	return cfg
}

// ClickMenu activates a main-menu item by its title path ("File",
// "New note"). A role item with no title matches its role name
// ("quit"). Navigate and Handler items go through the battery's menu
// dispatch like a native click; quit, settings, and show roles do what
// the native shell does for them.
func (h *Harness) ClickMenu(titles ...string) {
	h.t.Helper()
	cfg := h.config()
	if cfg.Menu == nil {
		h.t.Fatal("desktoptest.ClickMenu: the app configured no menu")
	}
	item := h.findItem(cfg.Menu.Items, titles)
	h.activate(cfg, item, strings.Join(titles, " > "))
}

// ClickTray activates a tray-menu item by title (or role name for a
// title-less role item).
func (h *Harness) ClickTray(title string) {
	h.t.Helper()
	cfg := h.config()
	if cfg.Tray == nil || cfg.Tray.Menu == nil {
		h.t.Fatal("desktoptest.ClickTray: the app configured no tray menu")
	}
	item := h.findItem(cfg.Tray.Menu.Items, []string{title})
	h.activate(cfg, item, "tray > "+title)
}

// PressKey activates the main-menu item carrying the accelerator
// ("cmd+n"), matched case-insensitively.
func (h *Harness) PressKey(key string) {
	h.t.Helper()
	cfg := h.config()
	if cfg.Menu == nil {
		h.t.Fatal("desktoptest.PressKey: the app configured no menu")
	}
	item, ok := findKey(cfg.Menu.Items, strings.ToLower(key))
	if !ok {
		h.t.Fatalf("desktoptest.PressKey: no menu item carries %q", key)
	}
	h.activate(cfg, item, key)
}

func findKey(items []desktop.MenuItem, key string) (desktop.MenuItem, bool) {
	for _, it := range items {
		if strings.ToLower(it.Key) == key && it.Key != "" {
			return it, true
		}
		if found, ok := findKey(it.Children, key); ok {
			return found, true
		}
	}
	return desktop.MenuItem{}, false
}

func (h *Harness) findItem(items []desktop.MenuItem, titles []string) desktop.MenuItem {
	h.t.Helper()
	if len(titles) == 0 {
		h.t.Fatal("desktoptest: a menu path needs at least one title")
	}
	var found desktop.MenuItem
	for i, title := range titles {
		var ok bool
		found, ok = matchItem(items, title)
		if !ok {
			h.t.Fatalf("desktoptest: no menu item %q at %q; have %s",
				title, strings.Join(titles[:i], " > "), itemTitles(items))
		}
		items = found.Children
	}
	return found
}

func matchItem(items []desktop.MenuItem, title string) (desktop.MenuItem, bool) {
	for _, it := range items {
		if it.Title == title || (it.Title == "" && it.Role == title) {
			return it, true
		}
	}
	return desktop.MenuItem{}, false
}

func itemTitles(items []desktop.MenuItem) string {
	var out []string
	for _, it := range items {
		switch {
		case it.Title != "":
			out = append(out, fmt.Sprintf("%q", it.Title))
		case it.Role != "":
			out = append(out, "<"+it.Role+">")
		}
	}
	return "[" + strings.Join(out, ", ") + "]"
}

// activate does what the native shell does for one clicked item.
func (h *Harness) activate(cfg desktop.WindowConfig, item desktop.MenuItem, where string) {
	h.t.Helper()
	switch {
	case item.Role == desktop.RoleQuit:
		h.Shell.Quit()
	case item.Role == desktop.RoleSettings:
		if cfg.OnSettings == nil {
			h.t.Fatalf("desktoptest: %s is a settings role but the battery installed no OnSettings", where)
		}
		cfg.OnSettings()
	case item.Role == desktop.RoleShow:
		if w := h.Shell.Window(mainWindowID); w != nil {
			_ = w.Focus()
		}
	case item.Role != "":
		h.t.Fatalf("desktoptest: %s is a %q role, which the OS handles without the app", where, item.Role)
	case len(item.Children) > 0:
		h.t.Fatalf("desktoptest: %s is a submenu, not an item", where)
	default:
		if cfg.OnMenu == nil {
			h.t.Fatal("desktoptest: the battery installed no OnMenu")
		}
		cfg.OnMenu(item.ID)
	}
}

// OpenSettings activates the app menu's own Settings item (cmd+, on
// macOS), the one the shell synthesizes when Config.Settings is set.
func (h *Harness) OpenSettings() {
	h.t.Helper()
	cfg := h.config()
	if cfg.Settings == nil {
		h.t.Fatal("desktoptest.OpenSettings: the app configured no Settings window")
	}
	if cfg.OnSettings == nil {
		h.t.Fatal("desktoptest.OpenSettings: the battery installed no OnSettings")
	}
	cfg.OnSettings()
}

// CloseWindow is the user closing a secondary window through the OS:
// the fake window is marked closed and the battery is told.
func (h *Harness) CloseWindow(id string) {
	h.t.Helper()
	if id == mainWindowID {
		h.t.Fatal("desktoptest.CloseWindow: use CloseMainWindow for the main window")
	}
	w := h.Shell.Window(id)
	if w == nil {
		h.t.Fatalf("desktoptest.CloseWindow: no window %q; open: %v", id, h.WindowIDs())
	}
	_ = w.Close()
	if cb := h.config().OnWindowClosed; cb != nil {
		cb(id)
	}
}

// CloseMainWindow is the user clicking the main window's close
// button: with Tray.CloseHidesWindow the window hides and the app
// stays alive (Focus, the show role, brings it back); otherwise the
// app quits.
func (h *Harness) CloseMainWindow() {
	h.t.Helper()
	cfg := h.config()
	w := h.Shell.Window(mainWindowID)
	if w == nil {
		h.t.Fatal("desktoptest.CloseMainWindow: no main window")
	}
	if cfg.Tray != nil && cfg.Tray.CloseHidesWindow {
		w.hide()
		return
	}
	h.Shell.Quit()
}

// Answer queues decisions for the next permission prompts, in order.
// An unanswered prompt denies.
func (h *Harness) Answer(ds ...desktop.Decision) { h.Shell.SetPromptDecisions(ds...) }

// ----- observation --------------------------------------------------------

// Window returns the fake window with the given id; the test fails
// when there is none.
func (h *Harness) Window(id string) *Window {
	h.t.Helper()
	w := h.Shell.Window(id)
	if w == nil {
		h.t.Fatalf("desktoptest: no window %q; open: %v", id, h.WindowIDs())
	}
	return w
}

// MoveWindow plays the user dragging window id to frame f: the fake
// window's frame changes and the shell reports through
// WindowConfig.OnWindowFrame, the way a native move does.
func (h *Harness) MoveWindow(id string, f desktop.Frame) {
	h.t.Helper()
	h.Window(id) // fails the test naming a window that is not open
	h.Shell.MoveWindow(id, f)
}

// BootRedirect is the Location the boot handshake answered with: "/"
// or, when the app remembers windows, the main window's last path.
func (h *Harness) BootRedirect() string { return h.bootRedirect }

// WindowIDs lists the battery's live windows, main first.
func (h *Harness) WindowIDs() []string {
	var ids []string
	for _, w := range h.Battery.Windows() {
		ids = append(ids, w.ID())
	}
	return ids
}

// Event is one native event Battery.Emit delivered to the main window.
type Event struct {
	Name    string
	Payload json.RawMessage
}

// Unmarshal decodes the event's payload into v.
func (e Event) Unmarshal(v any) error { return json.Unmarshal(e.Payload, v) }

// Events returns every native event dispatched to the main window's
// page, in order, parsed out of the evals the battery issued. A
// secondary window's events are that window's own: h.Window(id).Events().
func (h *Harness) Events() []Event {
	w := h.Shell.Window(mainWindowID)
	if w == nil {
		return nil
	}
	return w.Events()
}

// WaitEvent waits for the next event named name that Events has not
// handed out through an earlier WaitEvent, and returns it.
func (h *Harness) WaitEvent(name string) Event {
	h.t.Helper()
	seen := h.eventsSeen
	var got Event
	h.Wait("event "+name, func() bool {
		evs := h.Events()
		for i := seen; i < len(evs); i++ {
			if evs[i].Name == name {
				got = evs[i]
				h.eventsSeen = i + 1
				return true
			}
		}
		return false
	})
	return got
}

// Navigations returns every path the battery told the main window's
// page to navigate to (menu Navigate items), in order.
func (h *Harness) Navigations() []string {
	w := h.Shell.Window(mainWindowID)
	if w == nil {
		return nil
	}
	var out []string
	for _, js := range w.Evals() {
		if p, ok := ParseNavigate(js); ok {
			out = append(out, p)
		}
	}
	return out
}

// Wait polls cond every 10ms for up to 10s and fails the test naming
// what it waited for.
func (h *Harness) Wait(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(pollInterval)
	}
	if cond() {
		return
	}
	h.t.Fatalf("desktoptest: timed out waiting for %s", what)
}

// Quit ends the app the way the quit role does and waits for
// Battery.Run to return; it returns Run's error. Idempotent, and
// registered in t.Cleanup by Run.
func (h *Harness) Quit() error {
	h.quitOnce.Do(func() {
		h.Shell.Quit()
		select {
		case err := <-h.runErr:
			h.quitResult = err
		case <-time.After(quitTimeout):
			h.quitResult = errors.New("desktoptest: Battery.Run did not return within 30s of Quit")
		}
	})
	return h.quitResult
}

// ----- eval parsing -------------------------------------------------------

// dispatchMarker and navigateMarker are the shapes Battery.Emit and
// the menu dispatch evaluate in the page. The names and payloads are
// JSON string literals, so a JSON decoder reads them back exactly.
const (
	dispatchMarker = ".desktop._dispatch("
	navigateMarker = "window.__gofastr.navigate("
)

// ParseEvent recognizes the script Battery.Emit evaluates and returns
// the event it carries.
func ParseEvent(js string) (Event, bool) {
	i := strings.Index(js, dispatchMarker)
	if i < 0 {
		return Event{}, false
	}
	rest := js[i+len(dispatchMarker):]
	name, n, ok := jsonString(rest)
	if !ok {
		return Event{}, false
	}
	rest = strings.TrimLeft(rest[n:], " ")
	const parse = ", JSON.parse("
	if !strings.HasPrefix(rest, parse) {
		return Event{}, false
	}
	data, _, ok := jsonString(rest[len(parse):])
	if !ok || !json.Valid([]byte(data)) {
		return Event{}, false
	}
	return Event{Name: name, Payload: json.RawMessage(data)}, true
}

// ParseNavigate recognizes the script a menu Navigate item evaluates
// and returns its path.
func ParseNavigate(js string) (string, bool) {
	if !strings.HasPrefix(js, navigateMarker) {
		return "", false
	}
	p, _, ok := jsonString(js[len(navigateMarker):])
	return p, ok
}

// jsonString decodes one JSON string literal at the start of s and
// returns it with the number of bytes consumed.
func jsonString(s string) (string, int, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	var v string
	if err := dec.Decode(&v); err != nil {
		return "", 0, false
	}
	return v, int(dec.InputOffset()), true
}
