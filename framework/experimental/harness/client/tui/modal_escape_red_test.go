//go:build red

package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool/builtins"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: external/model-borne text is C0/DEL/CSI/OSC-sanitized before reaching the terminal.
// Surfaces: tui/modal.go::drawModal (writes body lines raw), tui/slash.go::dispatchLocalSlash
// "/tasks" case (builds lines from TaskItem.Content/ActiveForm), harness tool
// builtins/tasks.go::TaskList.Run (stores args.Tasks verbatim).
// Finding: every ingest path in terminal.go wraps sanitizeAgentText, but the modal path is
// the divergence: a model (via prompt injection) calls the TaskList tool with content
// "step one \x1b]0;pwned\x07\x1b[?1049h"; /tasks renders the modal and drawModal writes the
// raw OSC/CSI sequences to t.out — terminal title hijack, alternate-screen flip, and
// mouse-tracking injection in the operator's terminal.
// Fix direction: sanitize at the modal ingest boundary (openModal or the /tasks line
// builder) so modal body lines carry no C0/DEL/CSI/OSC bytes, mirroring sanitizeAgentText.

// TestModalRedEscapesInjectedTasks seeds the task store through the real
// TaskList tool dispatch path (the only write path the model has), opens the
// /tasks modal, draws, and asserts the buffer the terminal would receive is
// free of the injected escape sequences while the visible text survives.
func TestModalRedEscapesInjectedTasks(t *testing.T) {
	tui, buf := newRenderTestTUI(t)
	// The default render-test height (10) clips modal body rows away
	// (drawModal reserves 6 rows); give the modal room to paint the payload.
	tui.height = 30
	t.Cleanup(func() { builtins.ResetTasks(tui.Session) })

	// Control bytes ride the tool input as JSON \uXXXX escapes — the raw
	// byte forms are invalid inside JSON string literals, and this is
	// exactly how a model smuggles them past schema validation.
	input := json.RawMessage(`{"tasks":[` +
		`{"content":"step one \u001b]0;pwned\u0007\u001b[?1049h","status":"pending"},` +
		`{"content":"ok\rEVIL","status":"in_progress","activeForm":"doing \u001b[?1000;1006h"}]}`)
	if _, err := (builtins.TaskList{}).Run(tool.WithSession(context.Background(), tui.Session), tool.ToolCall{Input: input}, nil); err != nil {
		t.Fatalf("setup broken: seed TaskList: %v", err)
	}
	items, _ := builtins.TaskListSnapshot(tui.Session)
	if len(items) != 2 {
		t.Fatalf("setup broken: seeded %d tasks, want 2", len(items))
	}

	handled, _ := tui.dispatchLocalSlash("/tasks")
	if !handled {
		t.Fatalf("setup broken: /tasks was not handled locally")
	}
	tui.draw()
	out := buf.String()

	for _, bad := range []string{"\x1b]0;", "?1049", "?1000", "ok\rEVIL"} {
		if strings.Contains(out, bad) {
			t.Errorf("SECURITY: [tui-modal-escape-injection] model-borne %q reached the terminal via the /tasks modal (drawModal writes body lines raw): terminal title hijack / alternate-screen flip / mouse-tracking / row-overwrite injection in the operator's terminal", bad)
		}
	}
	if !strings.Contains(out, "step one") {
		t.Errorf("SECURITY: [tui-modal-escape-injection] sanitization may not blank the visible task text: \"step one\" must survive the /tasks modal render")
	}
}

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; surface:
// renderEvent tool-name field of the pinned terminal-scrub family).
// Property: terminal-bound output must not carry content-borne terminal
// control sequences — in the tool NAME, not only its args/progress.
// Surfaces: tui/terminal.go renderEvent ToolCallStarted branch
// (fmt.Sprintf("%s(%s)", v.Tool, sanitizeAgentText(summarizeArgs(v.Args)))
// interpolates v.Tool raw) and PermissionRequested branch
// (fmt.Sprintf("[permission] %s requested — ", v.Tool), also raw).
// Finding: the sibling exemption in terminal_security_test.go:26-30
// reasoned only about ARGS being json.RawMessage; Tool is a plain string,
// and the envelope codec round-trips string fields' raw control bytes
// intact (EncodeEvent escapes on the wire, DecodeEvent restores them), so
// a Tool of "bad\x1b]0;pwned\x07name\u009b31m" writes a live OSC-0
// title-rewrite BEL sequence plus a raw C1 CSI introducer (U+009B, honored
// as CSI by 8-bit terminals) into scrollback and out to the operator's
// terminal — and the permission leg renders it in the line a human reads
// to decide whether to approve the call.
// Reachability caveat, stated honestly: today's only event publishers
// register builtin tools with constant names, so no live publisher yet
// carries a dirty name — the sink itself is what this test verifies. The
// designed-in extension seams make the field live: tool.ToolSource is
// implemented by MCP-bridged tools (mcpclient.Source surfaces remote
// server-chosen names like "mcp:kiln.create_entity") and plugin-contributed
// tools, and those names flow into these events unfiltered.
// Fix direction: wrap v.Tool in sanitizeAgentText in both renderEvent
// branches, exactly as the args side already is.

// TestToolEventRedNameScrubbed drives a ToolCallStarted and a
// PermissionRequested whose Tool field carries OSC + C1 bytes through the
// envelope codec into renderEvent (the same delivery path the sibling
// terminal_security_test.go uses), draws, and asserts the buffer the
// terminal would receive is free of the raw sequences while the visible
// name fragments survive.
func TestToolEventRedNameScrubbed(t *testing.T) {
	legs := []struct {
		name string
		ev   func(tool string) control.Event
	}{
		{"ToolCallStarted", func(tool string) control.Event {
			return control.ToolCallStarted{CallID: ids.NewCallID(), Tool: tool, Args: json.RawMessage(`{}`)}
		}},
		{"PermissionRequested", func(tool string) control.Event {
			return control.PermissionRequested{CallID: ids.NewCallID(), Tool: tool, Args: json.RawMessage(`{}`)}
		}},
	}

	for _, leg := range legs {
		// OSC-0 title rewrite framed by visible text, BEL-terminated,
		// plus a C1 CSI introducer rune — the byte classes a terminal
		// interprets rather than prints.
		payload := "bad\x1b]0;pwned\x07name" + "\u009b31m"

		tui, buf := newRenderTestTUI(t)
		render(t, tui, leg.ev(payload))
		if len(tui.scrollback) == 0 {
			t.Fatalf("setup broken: %s produced no scrollback line", leg.name)
		}

		// Storage layer: scrollback rows must never store a C0/DEL byte;
		// storing one hands every future renderer the same bug.
		for i, ln := range tui.scrollback {
			if strings.HasPrefix(ln, spinnerLineMarker) {
				continue
			}
			if c, bad := ctrlByte(ln); bad {
				t.Errorf("SECURITY: [tui-toolname-scrub] %s: scrollback[%d] stores control byte %#02x from the tool NAME: %q",
					leg.name, i, c, ln)
				break
			}
		}

		// Terminal layer: the raw sequences must not reach t.out.
		buf.Reset()
		tui.draw()
		out := buf.String()
		if strings.Contains(out, "\x1b]0;") {
			t.Errorf("SECURITY: [tui-toolname-scrub] %s: raw OSC title-rewrite sequence from the tool NAME reached terminal output: %q",
				leg.name, out)
		}
		if strings.Contains(out, "\u009b") {
			t.Errorf("SECURITY: [tui-toolname-scrub] %s: raw C1 CSI introducer from the tool NAME reached terminal output: %q",
				leg.name, out)
		}
		// The name is the operator's only label for what runs / what is
		// being approved: scrubbing may not blank it.
		if !strings.Contains(out, "bad") || !strings.Contains(out, "name") {
			t.Errorf("SECURITY: [tui-toolname-scrub] %s: scrub may not blank the visible tool name: \"bad\" and \"name\" must survive the render: %q",
				leg.name, out)
		}
	}
}
