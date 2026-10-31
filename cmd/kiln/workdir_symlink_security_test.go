package main

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// TestAgentWorkDirRefusesSymlink: a co-user can plant the fixed
// /tmp/kiln-<adapter> name as a symlink to a directory this user owns
// (a ~/public_html). chmod follows the link, so the turn tightened the
// victim's own directory to 0700 and ran inside it. The parent must be
// a real directory.
func TestAgentWorkDirRefusesSymlink(t *testing.T) {
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp() })
	if err != nil {
		t.Fatalf("live.New: %v", err)
	}
	tools := protocol.New(l)

	root := t.TempDir()
	victim := filepath.Join(root, "public_html")
	if err := os.Mkdir(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(root, "kiln-pinned")
	if err := os.Symlink(victim, planted); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "ran")
	adapter := Adapter{
		Name:      "pinned",
		Dir:       planted,
		BuildArgs: func(string) []string { return []string{"/usr/bin/touch", marker} },
	}
	runOneAgentTurn(context.Background(), log.New(io.Discard, "", 0), tools, adapter, "http://127.0.0.1:1", "turn")

	fi, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("the symlink target's mode became %o", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(victim); len(entries) != 0 {
		t.Errorf("the turn wrote %d entries into the symlink target", len(entries))
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the agent ran with a symlinked work dir")
	}
}

// TestAgentWorkDirSwapRefused: a co-user who planted a real directory
// could swap the name for a link to this user's directory after the
// symlink check and before the chmod. The chmod reaches the opened
// directory only, and a name that no longer names it is refused.
func TestAgentWorkDirSwapRefused(t *testing.T) {
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp() })
	if err != nil {
		t.Fatalf("live.New: %v", err)
	}
	tools := protocol.New(l)

	root := t.TempDir()
	victim := filepath.Join(root, "public_html")
	if err := os.Mkdir(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(root, "kiln-swapped")
	if err := os.Mkdir(planted, 0o755); err != nil {
		t.Fatal(err)
	}
	adapterDirOpened = func() {
		if err := os.Rename(planted, planted+".old"); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(victim, planted); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { adapterDirOpened = func() {} })
	marker := filepath.Join(root, "ran")
	adapter := Adapter{
		Name:      "swapped",
		Dir:       planted,
		BuildArgs: func(string) []string { return []string{"/usr/bin/touch", marker} },
	}
	runOneAgentTurn(context.Background(), log.New(io.Discard, "", 0), tools, adapter, "http://127.0.0.1:1", "turn")

	fi, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("the swapped-in target's mode became %o", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(victim); len(entries) != 0 {
		t.Errorf("the turn wrote %d entries into the swapped-in target", len(entries))
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the agent ran in a swapped work dir")
	}
}
