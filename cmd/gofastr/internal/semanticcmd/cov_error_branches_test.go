package semanticcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/semantic"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// covT_unwritableHome points HOME at a regular file so the snapshot dir
// (HOME/.gofastr/semantic/...) can't be created → semantic.Open fails.
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
	clitest.CovT_chdir(t, t.TempDir())
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { embedIndex([]string{"."}, false) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestEmbedQueryOpenErrorExits(t *testing.T) {
	covT_unwritableHome(t)
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { embedQuery([]string{"text"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestEmbedStatsOpenErrorExits(t *testing.T) {
	covT_unwritableHome(t)
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { embedStats() })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestEmbedClearSucceedsOnMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// No snapshot exists yet → RemoveAll is a no-op success.
	clitest.CovT_capStdout(t, func() { embedClear() })
}

func TestEmbedRemoteQueryStatsConnError(t *testing.T) {
	// Nothing is listening on this loopback port → connection refused.
	const dead = "http://127.0.0.1:1"
	clitest.CovT_chdir(t, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GOFASTR_URL", dead)
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { embedQuery([]string{"q"}) })
	})
	if code != 1 {
		t.Fatalf("remote query conn-error want 1 got %d", code)
	}
	code = clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { embedStats() })
	})
	if code != 1 {
		t.Fatalf("remote stats conn-error want 1 got %d", code)
	}
	if _, err := remoteQuery(dead, semantic.Query{Text: "x"}); err == nil {
		t.Fatal("remoteQuery should error on dead host")
	}
	if _, err := remoteGet(dead + "/semantic/stats"); err == nil {
		t.Fatal("remoteGet should error on dead host")
	}
}
