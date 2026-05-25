package logging

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogJSONShape(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelInfo).WithComponent("engine")
	l.Info("hello", "k", "v", "n", 7)
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("missing newline: %q", out)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rec); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	if rec["msg"] != "hello" || rec["component"] != "engine" {
		t.Errorf("rec = %v", rec)
	}
	if rec["k"] != "v" {
		t.Errorf("kv lost: %v", rec)
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelWarn).WithComponent("engine")
	l.Debug("hidden")
	l.Warn("visible")
	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Error("debug leaked through warn level")
	}
	if !strings.Contains(out, "visible") {
		t.Error("warn dropped")
	}
}

func TestPerComponentLevel(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelWarn)
	if err := l.ApplyOverrides("engine=debug,provider.openrouter=trace"); err != nil {
		t.Fatal(err)
	}
	l.WithComponent("engine").Debug("engine-debug")
	l.WithComponent("provider.openrouter").Trace("openrouter-trace")
	l.WithComponent("mcpclient").Debug("mcp-debug")
	out := buf.String()
	if !strings.Contains(out, "engine-debug") {
		t.Error("engine debug missing")
	}
	if !strings.Contains(out, "openrouter-trace") {
		t.Error("openrouter trace missing")
	}
	if strings.Contains(out, "mcp-debug") {
		t.Error("mcpclient debug leaked (no override)")
	}
}

func TestDailyFileWriter(t *testing.T) {
	dir := t.TempDir()
	w, err := NewDailyFileWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Format("20060102")
	got, err := os.ReadFile(filepath.Join(dir, "harness-"+today+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Errorf("file content = %q", string(got))
	}
}

func TestParseLevel(t *testing.T) {
	for _, name := range []string{"trace", "debug", "info", "warn", "warning", "error"} {
		if _, err := ParseLevel(name); err != nil {
			t.Errorf("ParseLevel(%q): %v", name, err)
		}
	}
	if _, err := ParseLevel("bogus"); err == nil {
		t.Error("expected error for bogus")
	}
}
