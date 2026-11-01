package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// recordingDispatcher keeps every command the TUI sends.
type recordingDispatcher struct {
	mu   sync.Mutex
	cmds []control.Command
}

func (r *recordingDispatcher) Dispatch(_ context.Context, _ control.Client, cmd control.Command) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cmds = append(r.cmds, cmd)
	return nil
}

func newRecordingTUI() (*TUI, *recordingDispatcher) {
	rec := &recordingDispatcher{}
	tui := &TUI{
		Session: ids.NewSessionID(),
		Client:  inproc.New(ids.NewClientID(), control.IdentityHuman, nil, rec),
		out:     &bytes.Buffer{},
	}
	return tui, rec
}

func feed(t *testing.T, tui *TUI, ev control.Event) {
	t.Helper()
	env, err := control.EncodeEvent(1, ev, tui.Session, ids.NewClientID(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tui.renderEvent(env)
}

func askFor(tool string) control.PermissionRequested {
	return control.PermissionRequested{CallID: ids.NewCallID(), Tool: tool, Args: json.RawMessage(`{}`)}
}

func typeLine(tui *TUI, line string) {
	tui.input = []rune(line)
	tui.submit()
}

// Two prompts in one response are answered in the order they arrived,
// each under its own call id.
func TestPermissionPromptsQueueInOrder(t *testing.T) {
	tui, rec := newRecordingTUI()
	bash, write := askFor("Bash"), askFor("Write")
	feed(t, tui, bash)
	feed(t, tui, write)
	typeLine(tui, "y")
	typeLine(tui, "n")
	if len(rec.cmds) != 2 {
		t.Fatalf("sent %d commands, want 2: %+v", len(rec.cmds), rec.cmds)
	}
	first, ok1 := rec.cmds[0].(control.AnswerPermission)
	second, ok2 := rec.cmds[1].(control.AnswerPermission)
	if !ok1 || !ok2 {
		t.Fatalf("commands = %+v, want two answers", rec.cmds)
	}
	if first.CallID != bash.CallID || first.Decision != control.DecisionAllow {
		t.Errorf("first answer = %+v, want allow for Bash", first)
	}
	if second.CallID != write.CallID || second.Decision != control.DecisionDeny {
		t.Errorf("second answer = %+v, want deny for Write", second)
	}
}

// A prompt that resolved without this TUI (timed out, answered in the
// browser) is dropped, so the user's next line is a message, not a deny.
func TestResolvedPromptFreesInput(t *testing.T) {
	for name, resolve := range map[string]func(control.PermissionRequested) control.Event{
		"tool result": func(p control.PermissionRequested) control.Event {
			return control.ToolResult{CallID: p.CallID, IsError: true}
		},
		"turn ended": func(control.PermissionRequested) control.Event { return control.TurnEnded{} },
		"cancelled":  func(control.PermissionRequested) control.Event { return control.Cancelled{} },
	} {
		t.Run(name, func(t *testing.T) {
			tui, rec := newRecordingTUI()
			p := askFor("Bash")
			feed(t, tui, p)
			feed(t, tui, resolve(p))
			typeLine(tui, "fix the test")
			if len(rec.cmds) != 1 {
				t.Fatalf("sent %d commands", len(rec.cmds))
			}
			if _, ok := rec.cmds[0].(control.SendInput); !ok {
				t.Fatalf("sent %T, want SendInput", rec.cmds[0])
			}
		})
	}
}
