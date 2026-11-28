package devcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const cleanMain = "package main\n\nfunc main() {}\n"

func TestWatcherPrintsAReportOnceAndAnnouncesClean(t *testing.T) {
	dir := writeWatchTree(t, nil)
	w := NewDevContractWatch(dir)
	if out := captureWatch(t, w); !strings.Contains(out, "GOFASTR1002") {
		t.Fatalf("setup broken: no finding:\n%s", out)
	}
	if again := captureWatch(t, w); again != "" {
		t.Errorf("an unchanged report was printed again:\n%s", again)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(cleanMain), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := captureWatch(t, w); !strings.Contains(out, "contracts: clean") {
		t.Errorf("the findings going away was not announced:\n%s", out)
	}
	if again := captureWatch(t, w); again != "" {
		t.Errorf("a clean tree was announced twice:\n%s", again)
	}
}

func TestWatcherNarrowsToChangedFiles(t *testing.T) {
	dir := writeWatchTree(t, nil)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.email=dev@example.com", "-c", "user.name=dev"}, args...)...)
		cmd.Dir = dir
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v unavailable: %v\n%s", args, err, msg)
		}
	}
	git("init")
	git("add", ".")
	git("commit", "-m", "base")
	w := NewDevContractWatch(dir)
	if out := captureWatch(t, w); strings.Contains(out, "GOFASTR1002") {
		t.Fatalf("a finding in a file nobody changed was reported:\n%s", out)
	}
	src, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), append(src, "\n// edited\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := captureWatch(t, w); !strings.Contains(out, "GOFASTR1002") {
		t.Errorf("a finding in the file just changed was not reported:\n%s", out)
	}
}

func TestDevSummaryScrubsRepoText(t *testing.T) {
	r := devReport(t, 1)
	r.Diagnostics[0].File = "evil\x1b[2J.go"
	r.Diagnostics[0].Message = "msg \x1b]52;c;cHduZWQ=\a\u009b31m"
	out := NewDevContractWatch(".").Summarise(r)
	if strings.ContainsAny(out, "\x1b\a\u009b") {
		t.Errorf("a control byte from the repository reached the summary: %q", out)
	}
}
