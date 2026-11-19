package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// TaskItem is one entry in the plan.
type TaskItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm,omitempty"`
}

var (
	taskMu    sync.RWMutex
	taskStore = map[ids.SessionID][]TaskItem{}
	taskUpd   = map[ids.SessionID]time.Time{}
)

// TaskListSnapshot returns a copy of a session's task list and when it was last written.
func TaskListSnapshot(sess ids.SessionID) ([]TaskItem, time.Time) {
	taskMu.RLock()
	defer taskMu.RUnlock()
	items := append([]TaskItem(nil), taskStore[sess]...)
	return items, taskUpd[sess]
}

// ResetTasks clears a session's task list.
func ResetTasks(sess ids.SessionID) {
	taskMu.Lock()
	defer taskMu.Unlock()
	delete(taskStore, sess)
	delete(taskUpd, sess)
}

// TaskList replaces the calling session's task list, the session read from the dispatch context.
type TaskList struct{}

func (TaskList) Name() string { return "TaskList" }
func (TaskList) Description() string {
	return "Write or rewrite the agent's task plan. Pass the FULL list every call: it replaces, not appends. " +
		"Use to break a multi-step task into trackable items, mark the current one in_progress, and mark items completed as you finish them. " +
		"Each item: content (imperative, what TODO), status (pending|in_progress|completed), activeForm (optional, present-progressive description of the in-flight work)."
}
func (TaskList) Mutating() bool { return false }
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
	taskMu.Lock()
	taskStore[sess] = append([]TaskItem(nil), args.Tasks...)
	taskUpd[sess] = time.Now()
	taskMu.Unlock()

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
