package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/embed"
)

// covT_unwritableHome points HOME at a regular file so the snapshot dir
// (HOME/.gofastr/embed/...) can't be created → embed.Open fails.
func covT_unwritableHome(t *testing.T) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "homefile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", f)
	t.Setenv("USERPROFILE", f)
	t.Setenv("GOFASTR_URL", "")
	t.Setenv("EMBED_BACKEND", "")
}

func TestEmbedIndexOpenErrorExits(t *testing.T) {
	covT_unwritableHome(t)
	covT_chdir(t, t.TempDir())
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { embedIndex([]string{"."}, false) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestEmbedQueryOpenErrorExits(t *testing.T) {
	covT_unwritableHome(t)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { embedQuery([]string{"text"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestEmbedStatsOpenErrorExits(t *testing.T) {
	covT_unwritableHome(t)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { embedStats() })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestEmbedClearSucceedsOnMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// No snapshot exists yet → RemoveAll is a no-op success.
	covT_capStdout(t, func() { embedClear() })
}

func TestEmbedRemoteQueryStatsConnError(t *testing.T) {
	// Nothing is listening on this loopback port → connection refused.
	const dead = "http://127.0.0.1:1"
	covT_chdir(t, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GOFASTR_URL", dead)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { embedQuery([]string{"q"}) })
	})
	if code != 1 {
		t.Fatalf("remote query conn-error want 1 got %d", code)
	}
	code = covT_capExit(t, func() {
		covT_capStdout(t, func() { embedStats() })
	})
	if code != 1 {
		t.Fatalf("remote stats conn-error want 1 got %d", code)
	}
	if _, err := remoteQuery(dead, embed.Query{Text: "x"}); err == nil {
		t.Fatal("remoteQuery should error on dead host")
	}
	if _, err := remoteGet(dead + "/embed/stats"); err == nil {
		t.Fatal("remoteGet should error on dead host")
	}
}

// ── skill.go write-failure branches ───────────────────────────────────

func TestWriteHostSkillMkdirFailure(t *testing.T) {
	// A regular file where the .claude tree must be created → MkdirAll fails.
	base := t.TempDir()
	blocker := filepath.Join(base, "blk")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeHostSkill(blocker); err == nil {
		t.Fatal("writeHostSkill should fail when target path is a file")
	}
}

// ── init write-failure branches ───────────────────────────────────────

func TestWriteCLAUDEmdFailure(t *testing.T) {
	if err := writeCLAUDEmd(filepath.Join(t.TempDir(), "missing", "deep")); err == nil {
		t.Fatal("writeCLAUDEmd should fail for missing parent dir")
	}
}

func TestRunReinitWriteFailureExits(t *testing.T) {
	// Pass a path whose AGENTS detail dir can't be created.
	blocker := filepath.Join(t.TempDir(), "blk")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runReinit(blocker, false) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}
