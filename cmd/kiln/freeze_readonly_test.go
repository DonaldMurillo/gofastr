package main

import (
	"os"
	"path/filepath"
	"testing"
)

// kiln freeze runs beside a live kiln serve. Bytes past the last newline
// can be an Append serve has not finished, so freeze must read the
// journal without healing it: the torn-tail truncate in OpenJSONL could
// cut off an entry serve committed between freeze's scan and its Stat.
func TestFreezeDoesNotTouchJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "j.jsonl")
	body := `{"id":"1","ts":"2026-01-01T00:00:00Z","kind":"chat_user","payload":{"text":"hi"}}` + "\n" + `{"id":"2","kind":"chat_us`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runFreeze([]string{"--journal", path, "--diff"}); code != 0 {
		t.Fatalf("runFreeze --diff = %d, want 0", code)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("freeze rewrote the journal:\nwant %q\ngot  %q", body, got)
	}
}
