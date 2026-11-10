package desktopcmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The verb stamps the expiry the flag asks for, 30 days when it is
// absent, and tells the publisher when to run it again.
func TestDesktopFeedStampsExpiry(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	if _, code := verbOutput(t, func() { runDesktopKeygen([]string{"-o", key}) }); code != -1 {
		t.Fatalf("keygen exited %d", code)
	}
	archive := buildFeedArchive(t, dir)
	for name, tc := range map[string]struct {
		flag []string
		days int
	}{
		"default":  {nil, 30},
		"one week": {[]string{"--valid-days", "7"}, 7},
	} {
		out := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
		args := append([]string{"--key", key, "--version", "1.2.0", "--platform", "darwin-arm64",
			"--archive", archive, "--url", "https://example.com/Notes.zip", "-o", out}, tc.flag...)
		start := time.Now()
		printed, code := verbOutput(t, func() { runDesktopFeed(args) })
		if code != -1 {
			t.Fatalf("%s: feed exited %d: %s", name, code, printed)
		}
		raw, err := os.ReadFile(filepath.Join(out, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Expires time.Time `json:"expires"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatalf("%s: %v in %s", name, err, raw)
		}
		want := start.Add(time.Duration(tc.days) * 24 * time.Hour)
		if d := manifest.Expires.Sub(want); d < -time.Minute || d > time.Minute {
			t.Errorf("%s: expires %s, want about %s", name, manifest.Expires, want)
		}
		if !strings.Contains(printed, "expires in "+strconv.Itoa(tc.days)+" days") {
			t.Errorf("%s: output does not say when the manifest expires: %s", name, printed)
		}
	}
}

func TestFeedLifetimeFlag(t *testing.T) {
	day := 24 * time.Hour
	for in, want := range map[string]time.Duration{
		"":    0, // SignUpdateFeed's default
		"1":   day,
		"30":  30 * day,
		"365": 365 * day,
	} {
		got, err := feedLifetime(in)
		if err != nil || got != want {
			t.Errorf("--valid-days %q = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"0", "-1", "366", "1.5", "30d", "nine", "99999999999999999999"} {
		if got, err := feedLifetime(in); err == nil {
			t.Errorf("--valid-days %q accepted as %v", in, got)
		}
	}
}
