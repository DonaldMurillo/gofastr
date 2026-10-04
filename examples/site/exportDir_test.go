package main

import "testing"

func TestExportDir_FlagWithSpace(t *testing.T) {
	if got := exportDir([]string{"--export", "/tmp/site"}); got != "/tmp/site" {
		t.Errorf("got %q, want /tmp/site", got)
	}
}

func TestExportDir_FlagEquals(t *testing.T) {
	if got := exportDir([]string{"--export=/tmp/site"}); got != "/tmp/site" {
		t.Errorf("got %q, want /tmp/site", got)
	}
}

func TestExportDir_Absent(t *testing.T) {
	if got := exportDir([]string{"--port", "8083"}); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExportDir_TrailingFlagWithoutValue(t *testing.T) {
	// `--export` with no following arg must not panic / overshoot; returns "".
	if got := exportDir([]string{"--export"}); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestSiteURLForListenAddr(t *testing.T) {
	for addr, want := range map[string]string{
		":8083":           "http://localhost:8083",
		"127.0.0.1:10083": "http://127.0.0.1:10083",
	} {
		if got := siteURL(addr); got != want {
			t.Errorf("siteURL(%q) = %q, want %q", addr, got, want)
		}
	}
}
