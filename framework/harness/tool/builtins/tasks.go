package builtins

// TaskList — the harness equivalent of Claude Code's TodoWrite. The
// model maintains a structured plan as it works through a multi-step
// task. Each call REPLACES the whole list (the model thinks holistically
// about what's left); the harness keeps the latest snapshot per session
// so clients (TUI, web) can render it in a sidebar.
//
// Design choices vs Claude Code:
//
//   - Single tool, single action ("write the new list"). Atomic-replace
//     is simpler than create/update/delete/list, and matches how LLMs
//     actually think about plans.
//   - State is per-session, kept in a mutex-protected package map
//     keyed by SessionID. Persisting to SQLite is a roadmap item; the
//     in-memory store is enough for v0.1 since the model re-emits the
//     list on every change (so it's never the source of truth).
//   - Tasks have three statuses: pending, in_progress, completed.
//     Exactly one task at a time SHOULD be in_progress; we don't
//     enforce this (the model is free to violate it for valid
//     parallel-work patterns).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// TaskItem is one entry in the plan.
type TaskItem struct {
	Content    string `json:"content"`              // imperative form, "Read x.go"
	Status     string `json:"status"`               // pending|in_progress|completed
	ActiveForm string `json:"activeForm,omitempty"` // present-progressive, "Reading x.go"
}

// taskStore is the in-memory per-session list. Plain map + mutex; the
// state is tiny (≤ N tasks per session, ≤ 1KB total) so we don't need
// anything fancier.
var (
	taskMu    sync.RWMutex
	taskStore = map[ids.SessionID][]TaskItem{}
	taskUpd   = map[ids.SessionID]time.Time{} // last-write timestamp, for UI
)

// TaskListSnapshot returns the current list for a session. Exported
// so transports (REST, WS, web sidecar) can surface it without
// dispatching the tool — the tool is the WRITE path; this is the read.
func TaskListSnapshot(sess ids.SessionID) ([]TaskItem, time.Time) {
	taskMu.RLock()
	defer taskMu.RUnlock()
	items := append([]TaskItem(nil), taskStore[sess]...)
	return items, taskUpd[sess]
}

// ResetTasks clears a session's task list. Used by /clear-style
// commands or explicit test teardown.
func ResetTasks(sess ids.SessionID) {
	taskMu.Lock()
	defer taskMu.Unlock()
	delete(taskStore, sess)
	delete(taskUpd, sess)
}

// TaskList is the tool the model calls to rewrite its plan. The
// active session is resolved from the dispatch ctx via
// tool.SessionFromContext, so a single registered instance serves
// all sessions without stale-binding bugs.
type TaskList struct{}

func (TaskList) Name() string { return "TaskList" }
func (TaskList) Description() string {
	return "Write or rewrite the agent's task plan. Pass the FULL list every call — it replaces, not appends. " +
		"Use to break a multi-step task into trackable items, mark the current one in_progress, and mark items completed as you finish them. " +
		"Each item: content (imperative, what TODO), status (pending|in_progress|completed), activeForm (optional, present-progressive description of the in-flight work)."
}
func (TaskList) Mutating() bool { return false } // plan changes are stateless from the tool's POV
func (TaskList) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "tasks": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "content":    {"type": "string", "description": "Imperative description: 'Read x.go and find the bug'"},
          "status":     {"type": "string", "enum": ["pending","in_progress","completed"]},
          "activeForm": {"type": "string", "description": "Optional present-progressive label shown to the user, e.g. 'Reading x.go'"}
        },
        "required": ["content","status"],
        "additionalProperties": false
      }
    }
  },
  "required": ["tasks"],
  "additionalProperties": false
}`)
}

type taskListArgs struct {
	Tasks []TaskItem `json:"tasks"`
}

func (TaskList) Run(ctx context.Context, call tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	sess, ok := tool.SessionFromContext(ctx)
	if !ok {
		return errorResult("TaskList: no session in dispatch context"), nil
	}
	var args taskListArgs
	if err := json.Unmarshal(call.Input, &args); err != nil {
		return nil, fmt.Errorf("TaskList: invalid arguments: %w", err)
	}
	// Validate status values.
	for i, t := range args.Tasks {
		if t.Content == "" {
			return errorResult(fmt.Sprintf("TaskList: tasks[%d].content is required", i)), nil
		}
		switch t.Status {
		case "pending", "in_progress", "completed":
		default:
			return errorResult(fmt.Sprintf("TaskList: tasks[%d].status = %q (must be pending|in_progress|completed)",
				i, t.Status)), nil
		}
	}
	// Replace the list atomically.
	taskMu.Lock()
	taskStore[sess] = append([]TaskItem(nil), args.Tasks...)
	taskUpd[sess] = time.Now()
	taskMu.Unlock()

	// Echo a compact summary to the model — it doesn't need the full
	// list back, just confirmation of what's recorded.
	var lines []string
	var pending, inProgress, done int
	for i, t := range args.Tasks {
		mark := "○"
		switch t.Status {
		case "in_progress":
			mark = "▸"
			inProgress++
		case "completed":
			mark = "✓"
			done++
		default:
			pending++
		}
		lines = append(lines, fmt.Sprintf("  %s %d. %s", mark, i+1, t.Content))
	}
	body := fmt.Sprintf("plan updated: %d task%s (%d pending, %d in progress, %d completed)\n%s",
		len(args.Tasks), plural(len(args.Tasks)),
		pending, inProgress, done,
		strings.Join(lines, "\n"))
	return &tool.ToolResult{
		Content: []control.ContentBlock{{Type: "text", Text: body}},
	}, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
