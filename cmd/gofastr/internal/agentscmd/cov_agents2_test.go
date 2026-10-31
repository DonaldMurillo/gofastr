package agentscmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework/agentsinv"
)

func TestRunAgentsInitWriteFailureExits(t *testing.T) {
	// Target dir is a regular file → writing dir/AGENTS.md fails.
	blocker := filepath.Join(t.TempDir(), "blk")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runAgentsInit([]string{blocker}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunAgentsSkillFailureExits(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blk")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runAgentsSkill([]string{blocker}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunAgentsSyncCorruptMarkersExits(t *testing.T) {
	dir := t.TempDir()
	// AGENTS.md present but with duplicate markers → refreshAgentsMD errors.
	body := "preamble\n" + agentsAutoStart + "\nx\n" + agentsAutoEnd + "\n" + agentsAutoStart + "\ny\n" + agentsAutoEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runAgentsSync([]string{dir}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunAgentsSyncChangedRewrites(t *testing.T) {
	dir := t.TempDir()
	// Fresh init, then mutate the auto section so sync detects a change.
	clitest.CovT_capStdout(t, func() { runAgentsInit([]string{dir}) })
	body, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Replace the content between markers with a stale value.
	stale := string(body)
	if i := indexOf(stale, agentsAutoStart); i >= 0 {
		stale = stale[:i] + agentsAutoStart + "\nSTALE\n" + agentsAutoEnd + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sync replaces AGENTS.md and the detail files through a temporary
	// file: a handle opened before still reads what it opened.
	held, err := os.Open(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	detail := filepath.Join(dir, agentDetailRelPath(agentsinv.All()[0]))
	before := autoGenSentinel + " STALE\n"
	if err := os.WriteFile(detail, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	heldDetail, err := os.Open(detail)
	if err != nil {
		t.Fatal(err)
	}
	defer heldDetail.Close()
	clitest.CovT_capStdout(t, func() { runAgentsSync([]string{dir}) })
	if got, _ := io.ReadAll(held); string(got) != stale {
		t.Fatal("AGENTS.md was rewritten in place")
	}
	if got, _ := io.ReadAll(heldDetail); string(got) != before {
		t.Fatal("a detail file was rewritten in place")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md")); string(got) == stale {
		t.Fatal("sync did not refresh AGENTS.md")
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
