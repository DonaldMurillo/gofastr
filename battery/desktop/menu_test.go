package desktop

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// Group 7: menu validation and activation dispatch.

func TestMenuValidationCases(t *testing.T) {
	both := MenuItem{Title: testPhrase("x"), Navigate: "/a", Handler: func(context.Context) error { return nil }}
	badNav := MenuItem{Title: testPhrase("x"), Navigate: "https://evil.example"}
	badNav2 := MenuItem{Title: testPhrase("x"), Navigate: "/a/../../etc"}
	badNav3 := MenuItem{Title: testPhrase("x"), Navigate: "//host/path"}
	badRole := MenuItem{Title: testPhrase("x"), Role: "explody"}
	sepWithNav := MenuItem{Role: RoleSeparator, Navigate: "/a"}
	badKey := MenuItem{Title: testPhrase("x"), Key: "cmd+n!", Navigate: "/a"}
	dup := Menu{Items: []MenuItem{
		{ID: "same", Title: testPhrase("a"), Navigate: "/a"},
		{ID: "same", Title: testPhrase("b"), Navigate: "/b"},
	}}
	cases := []struct {
		name string
		menu *Menu
		want string
	}{
		{"both navigate and handler", &Menu{Items: []MenuItem{both}}, "both Navigate and Handler"},
		{"navigate with scheme", &Menu{Items: []MenuItem{badNav}}, "Navigate"},
		{"navigate with dotdot", &Menu{Items: []MenuItem{badNav2}}, "Navigate"},
		{"navigate protocol-relative", &Menu{Items: []MenuItem{badNav3}}, "Navigate"},
		{"unknown role", &Menu{Items: []MenuItem{badRole}}, "unknown role"},
		{"separator with navigate", &Menu{Items: []MenuItem{sepWithNav}}, "separator"},
		{"bad accelerator", &Menu{Items: []MenuItem{badKey}}, "Key"},
		{"duplicate ids", &dup, "duplicate menu item id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateMenu(tc.menu)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestMenuValidationAcceptsValidTreeAndAssignsIDs(t *testing.T) {
	m := &Menu{Items: []MenuItem{
		{Title: testPhrase("File"), Children: []MenuItem{
			{Title: testPhrase("New"), Navigate: "/new", Key: "cmd+n"},
			{ID: "custom", Title: testPhrase("Open"), Navigate: "/open"},
			{Role: RoleSeparator},
			{Title: testPhrase("Quit"), Role: RoleQuit},
		}},
	}}
	out, err := validateMenu(m)
	if err != nil {
		t.Fatal(err)
	}
	// Declaration order includes the parent: File gets m1, New m2.
	if out.Items[0].Children[0].ID != "m2" {
		t.Fatalf("auto id = %q, want m2", out.Items[0].Children[0].ID)
	}
	if out.Items[0].Children[1].ID != "custom" {
		t.Fatalf("explicit id overwritten: %q", out.Items[0].Children[1].ID)
	}
	// The caller's menu is not mutated.
	if m.Items[0].Children[0].ID != "" {
		t.Fatal("validateMenu mutated the caller's tree")
	}
}

func TestNewPanicsOnInvalidMenuNavigate(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("New accepted an invalid menu")
		}
		if !strings.Contains(r.(string), "Navigate") {
			t.Fatalf("panic = %v", r)
		}
	}()
	New(Config{ID: "x.example.app", Menu: &Menu{Items: []MenuItem{
		{Title: testPhrase("bad"), Navigate: "https://evil.example"},
	}}})
}

// captureLogger records slog records for handler-error assertions.
type captureLogger struct {
	mu      sync.Mutex
	records []string
	attrs   []string
}

func (c *captureLogger) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r.Message)
	r.Attrs(func(a slog.Attr) bool {
		c.attrs = append(c.attrs, a.Key+"="+a.Value.String())
		return true
	})
	return nil
}
func (c *captureLogger) Enabled(context.Context, slog.Level) bool { return true }
func (c *captureLogger) WithAttrs([]slog.Attr) slog.Handler       { return c }
func (c *captureLogger) WithGroup(string) slog.Handler            { return c }
func (c *captureLogger) joined() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.records, "\n") + "\n" + strings.Join(c.attrs, " ")
}

func TestMenuNavigateDispatchEmitsExactEval(t *testing.T) {
	b, _ := newTestBattery(t)
	win := &fakeWindow{}
	b.windowMu.Lock()
	b.window = win
	b.addr = "127.0.0.1:8123"
	b.windowMu.Unlock()
	b.menu = &Menu{Items: []MenuItem{{ID: "m1", Title: testPhrase("Go"), Navigate: "/x"}}}
	b.dispatchMenu("m1")
	evals := win.evals()
	if len(evals) != 1 {
		t.Fatalf("evals = %v", evals)
	}
	if evals[0] != `if (location.origin === "http://127.0.0.1:8123") { window.__gofastr.navigate("/x") }` {
		t.Fatalf("eval = %q", evals[0])
	}
}

func TestMenuHandlerRunsAsyncAndErrorsAreLoggedWithID(t *testing.T) {
	cl := &captureLogger{}
	b, _ := newTestBattery(t)
	b.logger = slog.New(cl)
	errCh := make(chan error, 1)
	b.menu = &Menu{Items: []MenuItem{{
		ID:    "boomitem",
		Title: testPhrase("Boom"),
		Handler: func(ctx context.Context) error {
			errCh <- errors.New("handler blew up")
			return errors.New("handler blew up")
		},
	}}}
	b.dispatchMenu("boomitem")
	// Handler ran (async).
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never ran")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(cl.joined(), "boomitem") && strings.Contains(cl.joined(), "handler blew up") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("handler error not logged with the item id; log = %q", cl.joined())
}

func TestMenuUnknownIDLoggedWarn(t *testing.T) {
	cl := &captureLogger{}
	b, _ := newTestBattery(t)
	b.logger = slog.New(cl)
	b.dispatchMenu("nonexistent")
	if !strings.Contains(cl.joined(), "nonexistent") {
		t.Fatalf("unknown id not logged: %q", cl.joined())
	}
}

func TestMenuNavigateWithoutWindowLogged(t *testing.T) {
	cl := &captureLogger{}
	b, _ := newTestBattery(t)
	b.logger = slog.New(cl)
	b.menu = &Menu{Items: []MenuItem{{ID: "m1", Navigate: "/x"}}}
	b.dispatchMenu("m1") // no window set
	if !strings.Contains(cl.joined(), "window") {
		t.Fatalf("missing-window warn not logged: %q", cl.joined())
	}
}

// Guard against unused-import churn in this file.
var _ = bytes.MinRead

// Tray callbacks must remain in the native worker until the actual app handler
// completes; a detached goroutine here defeats the native queue bound.
func TestTrayMenuHandlerCompletionIsSynchronous(t *testing.T) {
	b, _ := newTestBattery(t)
	started, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	b.menu = &Menu{Items: []MenuItem{{ID: "blocked", Handler: func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("tray handler has no deadline")
		}
		close(started)
		<-release
		return nil
	}}}}
	go func() { b.dispatchTrayMenu("blocked"); close(returned) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}
	select {
	case <-returned:
		unblock()
		t.Fatal("tray dispatch detached its handler")
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch did not finish with handler")
	}
}

func TestTrayMenuHandlerPanicIsContained(t *testing.T) {
	cl := &captureLogger{}
	b, _ := newTestBattery(t)
	b.logger = slog.New(cl)
	b.menu = &Menu{Items: []MenuItem{{ID: "panic-tray", Handler: func(context.Context) error { panic("fixture") }}}}
	b.dispatchTrayMenu("panic-tray")
	if !strings.Contains(cl.joined(), "menu handler panicked") {
		t.Fatal("tray handler panic was not reported")
	}
}
