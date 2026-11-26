package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestAgentBodyIsCapped(t *testing.T) {
	r := router.New()
	mountAgentRoutes(r, NewAdapterStore(Adapter{}), nil)
	body := `{"name":"` + strings.Repeat("a", maxAgentBody) + `"}`
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/kiln/agent", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized body: status %d, want 400", rec.Code)
	}
}

func TestAgentStateListsAdaptersInNameOrder(t *testing.T) {
	got := agentState(NewAdapterStore(Adapter{}))
	avail := got["available"].([]map[string]any)
	names := make([]string, 0, len(avail))
	for _, a := range avail {
		if _, ok := a["installed"].(bool); !ok {
			t.Fatalf("adapter %v has no installed flag", a["name"])
		}
		names = append(names, a["name"].(string))
	}
	if !sort.StringsAreSorted(names) || len(names) != len(adapters) {
		t.Fatalf("available = %v, want every adapter in name order", names)
	}
}

func TestAgentCancelEndpointKeepsAdapter(t *testing.T) {
	store := NewAdapterStore(Adapter{Name: "kept", BuildArgs: func(string) []string { return nil }})
	causes := make(chan error, 1)
	store.SetTurnCancel(func(c error) { causes <- c })
	r := router.New()
	mountAgentRoutes(r, store, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/kiln/agent/cancel", nil))
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["cancelled"] != true {
		t.Fatalf("cancel answered %s", rec.Body.String())
	}
	select {
	case c := <-causes:
		if !errors.Is(c, errCancelledByUser) {
			t.Fatalf("cause = %v, want errCancelledByUser", c)
		}
	default:
		t.Fatal("the in-flight turn was not cancelled")
	}
	if store.Get().Name != "kept" || store.InFlight() {
		t.Fatalf("cancel changed the adapter or left the turn in flight: %q %v", store.Get().Name, store.InFlight())
	}
}

func TestCancelHookPanicIsRecovered(t *testing.T) {
	store := NewAdapterStore(Adapter{})
	store.SetTurnCancel(func(error) { panic("hook\nforged line") })
	store.SetTurnCancel(func(error) {})
	store.Set(Adapter{})
	store.SetTurnCancel(func(error) { panic("again") })
	store.CancelInFlight()
}

func TestSafeBuildArgsRecoversPanic(t *testing.T) {
	a := Adapter{Name: "boom", BuildArgs: func(string) []string { panic("argv") }}
	if argv := safeBuildArgs(log.New(io.Discard, "", 0), a, "hi"); argv != nil {
		t.Fatalf("argv = %v, want nil after a panic", argv)
	}
}

func TestDestructiveIntentForcesPlan(t *testing.T) {
	for _, msg := range []string{
		"delete the posts entity", "remove the field", "drop users", "please WIPE everything", "rebuild it",
		"reset the seeds", "Clear All pages", "scrap that", "tear down the app",
	} {
		if got := enrichPrompt(msg); !strings.Contains(got, "propose_plan") || !strings.Contains(got, "Wait for the user's next message confirming") || !strings.HasSuffix(got, msg) {
			t.Errorf("enrichPrompt(%q) = %q, want a propose_plan directive ahead of the request", msg, got)
		}
	}
	if got := enrichPrompt("add a posts entity"); got != "add a posts entity" {
		t.Errorf("a constructive request was rewritten: %q", got)
	}
}

func TestCappedBufferTruncatesSilently(t *testing.T) {
	b := &cappedBuffer{limit: 4}
	for _, p := range []string{"ab", "cdef", "gh"} {
		if n, err := b.Write([]byte(p)); n != len(p) || err != nil {
			t.Fatalf("Write(%q) = %d, %v; want the full length and no error", p, n, err)
		}
	}
	if string(b.Bytes()) != "abcd" || !b.truncated {
		t.Fatalf("buffer = %q truncated=%v, want \"abcd\" truncated", b.Bytes(), b.truncated)
	}
	if maxAgentStdout != 4<<20 {
		t.Fatalf("maxAgentStdout = %d, want 4 MiB", maxAgentStdout)
	}
}

func TestShutdownWaitOutlastsTurnWaitDelay(t *testing.T) {
	if agentShutdownWait <= agentTurnWaitDelay {
		t.Fatalf("agentShutdownWait %v must exceed agentTurnWaitDelay %v so a held pipe still drains", agentShutdownWait, agentTurnWaitDelay)
	}
}

func TestCancelledTurnJournalsItsCause(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs /bin/sleep")
	}
	for cause, want := range map[error]string{
		errAgentSwitched:          "superseded by agent harness switch",
		errSupersededByNewMessage: "superseded by newer message",
		errCancelledByUser:        "cancelled by user",
		errors.New("other"):       "(turn cancelled:",
	} {
		l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp() })
		if err != nil {
			t.Fatal(err)
		}
		tools := protocol.New(l)
		ctx, cancel := context.WithCancelCause(context.Background())
		go func() { time.Sleep(100 * time.Millisecond); cancel(cause) }()
		a := Adapter{Name: "sleep", BuildArgs: func(string) []string { return []string{"/bin/sleep", "5"} }}
		runOneAgentTurn(ctx, log.New(io.Discard, "", 0), tools, a, "http://127.0.0.1:1", "hi")
		found := false
		l.ReadSession(func(s *journal.Session) {
			for _, e := range s.Chat {
				if e.Message != nil && strings.Contains(e.Message.Text, want) && strings.Contains(e.Message.Text, "partial work above is preserved") {
					found = true
				}
			}
		})
		if !found {
			t.Errorf("cause %v: no journaled note containing %q", cause, want)
		}
	}
}

func TestInstallSkillSyncsEmbeddedCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path, err := installSkill()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".claude", "skills", "kiln", "SKILL.md"); path != want {
		t.Fatalf("path = %s, want %s", path, want)
	}
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := installSkill(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != kilnSkillContent {
		t.Fatal("a stale installed skill was not replaced with the embedded copy")
	}
	if kilnSkillPath() != path {
		t.Fatalf("kilnSkillPath = %q, want %q", kilnSkillPath(), path)
	}
}

func TestAgentServeBindsLoopback(t *testing.T) {
	args := serveArgs(8765, "/j.jsonl")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--addr 127.0.0.1:8765") || !isLoopbackBindAddr("127.0.0.1:8765") {
		t.Fatalf("serve args %q do not bind loopback", joined)
	}
	if !strings.Contains(joined, "--journal /j.jsonl") || args[0] != "serve" {
		t.Fatalf("serve args %q", joined)
	}
}

func TestPrintFreezeDiffIsSorted(t *testing.T) {
	sess := journal.NewSession()
	for _, n := range []string{"zeta", "alpha", "mid", "beta", "omega"} {
		sess.World.Entities[n] = &world.Entity{Name: n}
	}
	for _, p := range []string{"/z", "/a", "/m"} {
		sess.World.Pages[p] = &world.Page{Path: p}
	}
	var buf bytes.Buffer
	printFreezeDiff(&buf, sess)
	out := buf.String()
	order := func(items ...string) {
		last := -1
		for _, it := range items {
			at := strings.Index(out, it)
			if at <= last {
				t.Fatalf("%q out of order in:\n%s", it, out)
			}
			last = at
		}
	}
	order("+ alpha", "+ beta", "+ mid", "+ omega", "+ zeta")
	order("+ /a:", "+ /m:", "+ /z:")
}

func TestInstalledSkillReachesEveryAdapter(t *testing.T) {
	skillHome := t.TempDir()
	t.Setenv("HOME", skillHome)
	t.Setenv("USERPROFILE", skillHome)
	path, err := installSkill()
	if err != nil {
		t.Fatal(err)
	}
	pi := strings.Join(adapters["pi"].BuildArgs("hi"), "\x00")
	if !strings.Contains(pi, "--skill\x00"+path) {
		t.Errorf("pi argv has no --skill %s", path)
	}
	for _, name := range []string{"codex", "claude-code", "omp"} {
		if argv := strings.Join(adapters[name].BuildArgs("hi"), " "); !strings.Contains(argv, "<kiln-skill>") {
			t.Errorf("%s argv carries no <kiln-skill> block", name)
		}
	}
}

func TestContractForbidsFileTools(t *testing.T) {
	if !strings.Contains(kilnContractPreamble, "Do NOT use Read, Write, Edit") {
		t.Fatal("the contract preamble no longer forbids the file tools")
	}
}

func TestTurnRunsInAFreshChildDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs /bin/pwd")
	}
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp() })
	if err != nil {
		t.Fatal(err)
	}
	fixed := filepath.Join(t.TempDir(), "kiln-fixed")
	a := Adapter{Name: "pwd", Dir: fixed, BuildArgs: func(string) []string { return []string{"/bin/pwd"} }}
	runOneAgentTurn(context.Background(), log.New(io.Discard, "", 0), protocol.New(l), a, "http://127.0.0.1:1", "hi")
	var cwd string
	l.ReadSession(func(s *journal.Session) {
		for _, e := range s.Chat {
			if e.Message != nil && strings.Contains(e.Message.Text, "kiln-fixed") {
				cwd = strings.TrimSpace(e.Message.Text)
			}
		}
	})
	real, _ := filepath.EvalSymlinks(fixed)
	if filepath.Dir(cwd) != real || !strings.HasPrefix(filepath.Base(cwd), "turn-") {
		t.Fatalf("turn ran in %q, want a turn- child of %q", cwd, real)
	}
}

func TestSupersedeCancelsWithNewMessageCause(t *testing.T) {
	store := NewAdapterStore(Adapter{})
	causes := make(chan error, 1)
	store.SetTurnCancel(func(c error) { causes <- c })
	store.SetTurnCancel(func(error) {})
	if c := <-causes; !errors.Is(c, errSupersededByNewMessage) {
		t.Fatalf("cause = %v, want errSupersededByNewMessage", c)
	}
}

func TestCustomAgentFlagDefaultsOff(t *testing.T) {
	if parseFlags(nil).allowCustom {
		t.Fatal("--allow-custom-agent defaults on")
	}
}

func TestEveryBuiltinAdapterHasIsolatedDir(t *testing.T) {
	for name, a := range adapters {
		if a.Dir == "" || filepath.Dir(a.Dir) != filepath.Clean(os.TempDir()) {
			t.Errorf("adapter %q Dir = %q, want a fixed directory directly under os.TempDir()", name, a.Dir)
		}
	}
}

func TestFreezeDiffWritesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "j.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if code := runFreeze([]string{"--journal", path, "--dir", out, "--diff"}); code != 0 {
		t.Fatalf("runFreeze --diff = %d", code)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("--diff created %s (stat err %v)", out, err)
	}
}
