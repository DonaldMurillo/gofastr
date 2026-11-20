package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractArchiveReportsWhatGitSaid(t *testing.T) {
	repo := gitRepo(t, 0)
	err := extractArchive(context.Background(), repo, t.TempDir(), "no-such-ref")
	if err == nil || !strings.Contains(err.Error(), "no-such-ref") {
		t.Fatalf("err = %v, want git's complaint naming the ref", err)
	}
}

func TestExtractArchiveKeepsTheLastLines(t *testing.T) {
	repo := gitRepo(t, 20)
	dir := t.TempDir()
	for i := range 20 {
		if err := os.MkdirAll(filepath.Join(dir, "sub", fmt.Sprintf("f%d", i), "keep"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	err := extractArchive(context.Background(), repo, dir, "HEAD")
	if err == nil {
		t.Fatal("extract over directories where its files go succeeded")
	}
	if n := strings.Count(err.Error(), "\n") + 1; n > 6 {
		t.Fatalf("error carries %d lines, want the last few: %v", n, err)
	}
	if !strings.Contains(err.Error(), "sub/") {
		t.Fatalf("err = %v, want tar's complaint", err)
	}
}

// gitRepo is a repository with one commit holding files sub/f0..sub/f<n-1>.
func gitRepo(t *testing.T, n int) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if err := os.WriteFile(filepath.Join(repo, "sub", fmt.Sprintf("f%d", i)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", "x"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return repo
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd\n", 2); got != "c\nd" {
		t.Fatalf("lastLines = %q", got)
	}
	if got := lastLines("", 3); got != "" {
		t.Fatalf("lastLines of nothing = %q", got)
	}
}
