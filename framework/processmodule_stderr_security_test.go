package framework

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

func TestModuleFailuresLogScrubbedStderr(t *testing.T) {
	for _, transition := range []string{"exit", "spawn", "failed"} {
		t.Run(transition, func(t *testing.T) {
			var logs []string
			sup := &ProcessModuleSupervisor{logf: func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }}
			sink := moduleproto.NewRingSink(64)
			sink.Write([]byte(strings.Repeat("old", 100)))
			sink.Write([]byte("diagnostic\x1b[31m\rforged\x00\u202etail"))
			child := &exitedChild{}
			sl := &moduleSlot{name: "demo", sup: sup, wake: make(chan struct{}, 1), stderr: sink, child: child, instanceID: "one", state: StateReady}
			switch transition {
			case "exit":
				sl.watchExit(child, "one")
			case "spawn":
				sl.failSpawn(0, "handshake refused", false)
			case "failed":
				sl.failSpawn(0, "digest mismatch", true)
			}
			got := strings.Join(logs, "\n")
			if !strings.Contains(got, "stderr") || !strings.Contains(got, "diagnostic") || !strings.Contains(got, "tail") {
				t.Fatalf("missing child diagnostic: %q", got)
			}
			if strings.ContainsAny(got, "\x1b\r\x00\u202e") {
				t.Fatalf("unsafe child bytes in logs: %q", got)
			}
			if strings.Contains(got, strings.Repeat("old", 100)) {
				t.Fatal("unbounded child history logged")
			}
		})
	}
}

func TestAppLogsModuleStderr(t *testing.T) {
	db := newStoreDB(t)
	defer db.Close()
	app := NewApp(WithDB(db))
	d := descriptorForChild(t, childModeEcho)
	app.RegisterProcessModule(d, ApprovedGrants{"articles:read"})
	var logs bytes.Buffer
	app.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	slot := app.ProcessModules().slots[d.Name]
	slot.stderr = moduleproto.NewRingSink(64)
	slot.stderr.Write([]byte("operator diagnostic\x1b[31m"))
	slot.failSpawn(slot.spawnSeq, "failed handshake", true)
	if got := logs.String(); !strings.Contains(got, "operator diagnostic") || strings.Contains(got, "\x1b") {
		t.Fatalf("app module log=%q", got)
	}
}
