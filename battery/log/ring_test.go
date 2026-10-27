package log

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRingSinkAppendsUnderCap(t *testing.T) {
	r := NewRingSink(3)
	for i := byte('A'); i <= byte('C'); i++ {
		if err := r.Write([]byte{i}); err != nil {
			t.Fatal(err)
		}
	}
	got := r.Snapshot()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, want := range []byte{'A', 'B', 'C'} {
		if got[i][0] != want {
			t.Errorf("got[%d]=%q want %q", i, got[i], want)
		}
	}
}

func TestRingSinkWrapsAtCap(t *testing.T) {
	r := NewRingSink(3)
	for i := byte('A'); i <= byte('E'); i++ {
		_ = r.Write([]byte{i})
	}
	got := r.Snapshot()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (oldest dropped on wrap)", len(got))
	}
	// After A,B,C,D,E with cap=3, we should see C,D,E.
	for i, want := range []byte{'C', 'D', 'E'} {
		if got[i][0] != want {
			t.Errorf("got[%d]=%q want %q (chronological after wrap)", i, got[i], want)
		}
	}
}

func TestRingSinkWriteAfterClose(t *testing.T) {
	r := NewRingSink(2)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Write([]byte("x")); err != ErrSinkClosed {
		t.Fatalf("Write after Close = %v, want ErrSinkClosed", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal("Close not idempotent")
	}
}

func TestRingSinkSnapshotDecoded(t *testing.T) {
	r := NewRingSink(4)
	_ = r.Write([]byte(`{"msg":"a","level":"INFO"}`))
	_ = r.Write([]byte(`{"msg":"b","level":"ERROR"}`))
	_ = r.Write([]byte(`not json`))
	_ = r.Write([]byte(`{"msg":"c","level":"INFO"}`))

	got := r.SnapshotDecoded()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (non-JSON entry skipped)", len(got))
	}
	if got[0]["msg"] != "a" || got[2]["msg"] != "c" {
		t.Errorf("decoded entries out of order: %v", got)
	}
}

// TestLogToolsKeepBigIntegers pins that log_recent and the historical
// file tail hand back a logged integer past 2^53 with its digits: both
// decoded entries through json.Unmarshal into map[string]any, which
// rounded 9007199254740993 to 9007199254740992.
func TestLogToolsKeepBigIntegers(t *testing.T) {
	_, p := newLogMCPApp(t)
	p.logger.Info("big.entry", "user_id", int64(9007199254740993))
	res, err := p.toolRecent(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"user_id":9007199254740993`) {
		t.Errorf("log_recent = %s, want user_id 9007199254740993", b)
	}

	path := filepath.Join(t.TempDir(), "app.log")
	line := `{"msg":"big.entry","user_id":9007199254740993,"status":200}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := tailFile(path, 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("tailFile = %v, %v", entries, err)
	}
	if b, _ := json.Marshal(entries[0]); !strings.Contains(string(b), `"user_id":9007199254740993`) {
		t.Errorf("tailed entry = %s, want user_id 9007199254740993", b)
	}
	if s := entries[0]["status"]; s != float64(200) {
		t.Errorf("ordinary number decodes as %T %v, want float64 200", s, s)
	}
}
