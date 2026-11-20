package tui

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/slash"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool/builtins"
)

func slashCandidates() []string {
	var out []string
	for _, b := range slash.AllBuiltins() {
		out = append(out, b.Name)
	}
	for _, ns := range slash.AllNamespaces() {
		out = append(out, ns+":")
	}
	sort.Strings(out)
	return out
}

func completeSlash(input string) (completion string, matches []string) {
	if !strings.HasPrefix(input, "/") {
		return "", nil
	}
	prefix := input[1:]
	var hits []string
	for _, c := range slashCandidates() {
		if strings.HasPrefix(c, prefix) {
			hits = append(hits, c)
		}
	}
	switch len(hits) {
	case 0:
		return "", nil
	case 1:
		return "/" + hits[0], nil
	default:
		return "/" + longestCommonPrefix(hits), hits
	}
}

func longestCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		for !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}

func slashPopupItems(input string, maxRows int) (rows []string, names []string, total int) {
	if !strings.HasPrefix(input, "/") {
		return nil, nil, 0
	}
	prefix := input[1:]
	var allRows, allNames []string
	for _, b := range slash.AllBuiltins() {
		if strings.HasPrefix(b.Name, prefix) {
			allNames = append(allNames, "/"+b.Name)
			allRows = append(allRows, fmt.Sprintf("/%-14s %s", b.Name, b.Description))
		}
	}
	for _, ns := range slash.AllNamespaces() {
		if strings.HasPrefix(ns, prefix) {
			allNames = append(allNames, "/"+ns+":")
			allRows = append(allRows, fmt.Sprintf("/%-14s (namespace: type to scope)", ns+":"))
		}
	}
	total = len(allNames)
	if total <= maxRows {
		return allRows, allNames, total
	}
	rows = append([]string{}, allRows[:maxRows-1]...)
	rows = append(rows, fmt.Sprintf("… (+%d more — keep typing to narrow)", total-(maxRows-1)))
	names = append([]string{}, allNames[:maxRows-1]...)
	return rows, names, total
}

func shortToken(tok string) string {
	if len(tok) <= 12 {
		return tok
	}
	return tok[:12]
}

func (t *TUI) popupActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.modal != nil {
		return false
	}
	_, names, _ := slashPopupItems(string(t.input), 8)
	return len(names) > 0
}

func (t *TUI) popupMove(delta int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, names, _ := slashPopupItems(string(t.input), 8)
	n := len(names)
	if n == 0 {
		return
	}
	t.popupIdx = ((t.popupIdx+delta)%n + n) % n
}

func (t *TUI) popupAcceptsEnter() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.modal != nil {
		return false
	}
	input := string(t.input)
	_, names, _ := slashPopupItems(input, 8)
	if len(names) == 0 {
		return false
	}
	return !slices.Contains(names, input)
}

func (t *TUI) dispatchLocalSlash(input string) (handled, exit bool) {
	c, err := slash.Parse(input)
	if err != nil {
		return false, false
	}
	switch c.Name {
	case "clear":
		t.mu.Lock()
		t.scrollback = nil
		t.assistantOpen = false
		t.thinkingOpen = false
		t.scrollOffset = 0
		t.mu.Unlock()
		t.showWelcome()
		return true, false
	case "help":
		var lines []string
		lines = append(lines, "Built-in slash commands:")
		lines = append(lines, "")
		for _, b := range slash.AllBuiltins() {
			line := "  /" + b.Name
			if b.ArgsHelp != "" {
				line += " " + b.ArgsHelp
			}
			pad := max(18-runeLen(line), 1)
			line += strings.Repeat(" ", pad) + b.Description
			lines = append(lines, line)
		}
		lines = append(lines, "")
		lines = append(lines, "Reserved namespaces (plugin-claimable):")
		lines = append(lines, "  "+strings.Join(slash.AllNamespaces(), ", "))
		lines = append(lines, "")
		lines = append(lines, "Keyboard shortcuts:")
		lines = append(lines, "  Tab          slash-command completion")
		lines = append(lines, "  Ctrl-P / N   input history")
		lines = append(lines, "  Ctrl-O       expand last truncated tool result")
		lines = append(lines, "  PgUp/Down    scroll scrollback")
		lines = append(lines, "  Ctrl-C × 2   exit")
		t.openModal("Help", lines)
		return true, false
	case "tasks":
		items, updated := builtins.TaskListSnapshot(t.Session)
		var lines []string
		if len(items) == 0 {
			lines = []string{"No plan yet.",
				"",
				"The agent records its plan by calling the TaskList tool. Ask it to break a task into steps and it'll show up here."}
		} else {
			for i, it := range items {
				mark := "○"
				switch it.Status {
				case "in_progress":
					mark = "▸"
				case "completed":
					mark = "✓"
				}
				label := it.Content
				if it.Status == "in_progress" && it.ActiveForm != "" {
					label = it.ActiveForm + "  (" + it.Content + ")"
				}
				lines = append(lines, fmt.Sprintf("  %s  %d. %s", mark, i+1, label))
			}
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("Last updated: %s", updated.Format("15:04:05")))
		}
		t.openModal("Plan", lines)
		return true, false
	case "web":
		if t.WebURL == "" {
			t.openModal("Web sidecar", []string{
				"The HTTP control plane isn't running.",
				"",
				"Start it with one of:",
				"  gofastr harness            # auto-enabled in interactive TTY",
				"  gofastr harness -web       # force-enable",
				"  gofastr harness -listen 127.0.0.1:8421",
			})
			return true, false
		}
		lines := []string{
			"URL:    " + t.WebURL,
			"",
			"Open the URL in a browser for the SSR landing page,",
			"or use the REST endpoints with the bearer token below.",
			"",
		}
		if t.WebToken != "" {
			lines = append(lines,
				"Token (24h, session-scoped):",
				"  "+t.WebToken,
				"",
				"Curl example:",
				"  curl -H 'Authorization: Bearer "+shortToken(t.WebToken)+"…' \\",
				"       "+t.WebURL+"/v1/sessions",
			)
		} else {
			lines = append(lines, "Token: (none: running without auth)")
		}
		t.openModal("Web sidecar", lines)
		return true, false
	case "quit":
		return true, true
	case "profile":
		lines := []string{
			"Profile · " + nonempty(t.Profile, "(none)"),
			"Model   · " + nonempty(t.Model, "(none)"),
			"Session · " + string(t.Session),
			"",
			"Per-profile tool packs and skills are loaded at boot.",
			"To switch profile, restart with `gofastr harness -profile <path>`.",
		}
		t.openModal("Profile", lines)
		return true, false
	case "cost":
		t.mu.Lock()
		usd := t.costUSD
		t.mu.Unlock()
		lines := []string{
			fmt.Sprintf("Session cost · $%.4f", usd),
			"",
			"Per-turn costs accumulate from CostIncremented events.",
			"Rates come from the provider's pricing table (zai static,",
			"openrouter dynamic from /models).",
		}
		t.openModal("Cost", lines)
		return true, false
	case "health":
		lines := []string{
			"Subsystem status:",
			"",
			"  TUI         · live",
			"  Session     · " + string(t.Session),
			"  Web sidecar · " + nonempty(t.WebURL, "(not running: start with -web)"),
		}
		if t.WebURL != "" {
			lines = append(lines, "",
				"For deep checks, hit "+t.WebURL+"/v1/health",
			)
		}
		t.openModal("Health", lines)
		return true, false
	case "model":
		rest := strings.TrimSpace(strings.Join(c.Args, " "))
		if rest == "" {
			lines := []string{
				"Current model · " + nonempty(t.Model, "(none)"),
				"",
				"To switch: /model <provider:id>",
				"e.g. /model zai:glm-5.1",
				"",
				"Note: actual model-switch dispatch is in v0.2; for v0.1",
				"this command reports the current model only.",
			}
			t.openModal("Model", lines)
			return true, false
		}
		if t.Client != nil {
			_ = t.Client.Send(context.Background(), control.SetModel{
				SessionID: t.Session,
				Model:     rest,
			})
		}
		t.mu.Lock()
		t.ensureBlankBefore()
		t.scrollback = append(t.scrollback,
			fmt.Sprintf("[model] requested switch to %q", rest))
		t.mu.Unlock()
		return true, false
	case "compact":
		if t.Client != nil {
			_ = t.Client.Send(context.Background(), control.CustomCommand{
				SessionID: t.Session,
				Verb:      "compact",
			})
		}
		t.mu.Lock()
		t.ensureBlankBefore()
		t.scrollback = append(t.scrollback,
			"[compact] requested: engine-side compaction lands in v0.2")
		t.mu.Unlock()
		return true, false
	}
	return false, false
}

func nonempty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
