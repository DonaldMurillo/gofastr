package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool/permission"
)

// Pins: internal error text never reaches control-plane clients
// CONTRACT-QUESTION red: the trigger is environmental (disk state) and
// the recipient an authenticated operator — delete if the harness treats
// operator-diagnosability as the contract for control events. The
// counter-precedent sits two layers up in the same tree: rest.go:196
// answers internal store failures with a generic "store operation
// failed" and no path, and this bus feeds ws/SSE/MCP clients verbatim.
// Property: internal error text never reaches control-plane clients
// through event envelopes.
// Surfaces: engine/permission_mw.go select-allow arm :129-137 — a failed
// "Allow always" persist publishes control.Error{Reason:
// "PermissionPersistFailed", Message: err.Error()} on the per-session
// bus, which the ws/SSE/MCP transports carry to every attached client;
// source: tool/permission/persistent.go::savePersistent :81-99 wraps
// absolute paths ("write tmp: open <abs>.tmp: is a directory", "mkdir
// <dir>: ...").
// Finding: planting a directory at <path>.tmp (any full disk or unwritable
// XDG_CONFIG_HOME reaches the same arm) publishes the operator's absolute
// config path to every client on the session.
// Fix direction: generic message at the publish site ("persisting
// permission rule failed"), keep the stable Reason code and log the
// detail server-side — rest.go's convention.

func TestPermissionPersistRedGeneric(t *testing.T) {
	dir := t.TempDir()
	persistPath := filepath.Join(dir, "permissions.json")
	// Plant a DIRECTORY at the temp-rename path so the atomic write
	// fails deterministically (EISDIR) with the absolute path inside.
	if err := os.Mkdir(persistPath+".tmp", 0o700); err != nil {
		t.Fatal("setup broken: plant directory at <path>.tmp:", err)
	}

	bus := NewBus(ids.NewSessionID())
	t.Cleanup(bus.Close)
	events := bus.Subscribe(context.Background())

	eng := permission.New(nil)
	eng.PersistencePath = persistPath

	// Ask-mode call: mutating Bash whose argv is not quiet-mode allowed
	// (the middleware_security_test shape), answered allow + always
	// through the stub router — the only path that reaches the persist
	// arm.
	answer := make(chan PermissionAnswer, 1)
	answer <- PermissionAnswer{Allow: true, Scope: control.ScopeAlways}

	invoked := false
	mw := PermissionMiddleware(bus, eng, stubRouter{ch: answer}, bus.Session(), time.Minute)
	call := tool.ToolCall{ID: ids.NewCallID(), Name: "Bash", Input: []byte(`{"cmd":"rm -rf /tmp/x"}`)}
	res, err := mw(WithMutatingFlag(context.Background(), true), call, nil,
		func(context.Context, tool.ToolCall, tool.EventSink) (*tool.ToolResult, error) {
			invoked = true
			return &tool.ToolResult{Content: []control.ContentBlock{{Type: "text", Text: "ran"}}}, nil
		})
	if err != nil {
		t.Fatal("setup broken: middleware returned error:", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("setup broken: allow-always answer must run the tool: %+v", res)
	}
	if !invoked {
		t.Fatal("setup broken: persist failure must not block an approved call (mw comment pins this)")
	}

	msg, sawPersistFail := drainPersistFailure(t, events)
	if !sawPersistFail {
		t.Fatal("setup broken: no PermissionPersistFailed event published — the .tmp directory plant did not force the persist arm")
	}
	if strings.Contains(msg, dir) || strings.Contains(msg, ".tmp") {
		t.Errorf("SECURITY: [permission-persist-path] the PermissionPersistFailed control.Error published on the session bus carries internal error text (%q): savePersistent wraps the operator's absolute persistence path and permission_mw forwards err.Error() verbatim to every ws/SSE/MCP client — rest.go:196 two layers up answers the same failure class with a generic message", msg)
	}

	// Control: on a clean persistence path the same allow-always answer
	// persists the rule and publishes NO PermissionPersistFailed event —
	// proving the red leg's event came from the planted directory, not
	// from the answer flow itself.
	controlBus := NewBus(ids.NewSessionID())
	t.Cleanup(controlBus.Close)
	controlEvents := controlBus.Subscribe(context.Background())

	cleanEng := permission.New(nil)
	cleanPath := filepath.Join(t.TempDir(), "permissions.json")
	cleanEng.PersistencePath = cleanPath
	controlAnswer := make(chan PermissionAnswer, 1)
	controlAnswer <- PermissionAnswer{Allow: true, Scope: control.ScopeAlways}
	controlMW := PermissionMiddleware(controlBus, cleanEng, stubRouter{ch: controlAnswer}, controlBus.Session(), time.Minute)
	if _, err := controlMW(WithMutatingFlag(context.Background(), true), call, nil,
		func(context.Context, tool.ToolCall, tool.EventSink) (*tool.ToolResult, error) {
			return &tool.ToolResult{}, nil
		}); err != nil {
		t.Fatal("setup broken: control middleware returned error:", err)
	}
	if _, err := os.Stat(cleanPath); err != nil {
		t.Fatal("setup broken: control leg did not persist the rule:", err)
	}
	if _, failed := drainPersistFailure(t, controlEvents); failed {
		t.Error("SECURITY: [permission-persist-path] clean-path control leg published a PermissionPersistFailed event — the red leg's trigger was not the planted directory")
	}
}

// drainPersistFailure drains a subscription and reports the Message of
// the first PermissionPersistFailed control.Error, if any was published.
func drainPersistFailure(t *testing.T, events <-chan control.EventEnvelope) (string, bool) {
	t.Helper()
	for {
		select {
		case env := <-events:
			e, err := control.DecodeEvent(env)
			if err != nil {
				continue
			}
			if ce, ok := e.(control.Error); ok && ce.Reason == "PermissionPersistFailed" {
				return ce.Message, true
			}
		default:
			return "", false
		}
	}
}
