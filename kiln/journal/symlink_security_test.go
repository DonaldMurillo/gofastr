package journal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Pins symlinked-leaf refusal at the journal's opens, found by the
// 2026-09-05 red-probe round (round 4); fixed by openJournalFile, which
// Lstats the leaf and refuses a symlink before every os.OpenFile on a
// journal path (open, the TruncateAfter tmp, and the reopen).
// Property: opening the session journal inside the current directory
// must not write to (or truncate) any file outside that directory — a
// checkout-supplied symlinked journal path is untrusted input, not
// developer configuration.

// TestOpenJournalRefusesSymlinkLeaf pins that the checkout-controlled
// journal path cannot make kiln write outside its directory. The victim
// deliberately ends WITHOUT a trailing newline so both write shapes are
// armed: the torn-tail heal's Truncate at open, and Append splicing
// journal lines after it. Either fix shape passes: refusing the open,
// or opening without following the symlink.
func TestOpenJournalRefusesSymlinkLeaf(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlinks are required for this probe")
	}
	checkout := t.TempDir()
	outside := t.TempDir() // a different tree: nothing in it is kiln's to touch
	victim := filepath.Join(outside, "victim-config.txt")
	content := "PRESERVE-ME line one\nPRESERVE-ME line two, no trailing newline"
	if err := os.WriteFile(victim, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	jl := filepath.Join(checkout, ".kiln.session.jsonl")
	if err := os.Symlink(victim, jl); err != nil {
		t.Fatal(err)
	}

	j, err := OpenJSONL(jl)
	if err == nil {
		e, nerr := NewEntry("symlink-probe", time.Now().UTC(), KindWorldEdit, OpSetAppConfig,
			SetAppConfigPayload{Config: world.AppConfig{Name: "probe"}})
		if nerr != nil {
			t.Fatalf("NewEntry: %v", nerr)
		}
		if _, aerr := j.Append(e); aerr != nil {
			t.Logf("Append through symlinked journal failed: %v", aerr)
		}
		_ = j.Close()
	}

	got, rerr := os.ReadFile(victim)
	if rerr != nil {
		t.Fatalf("victim unreadable after journal open/append: %v", rerr)
	}
	if string(got) != content {
		t.Errorf("SECURITY: [journal-symlink] .kiln.session.jsonl symlinked to a file outside the checkout changed it: got %q, want %q. Attack: an untrusted checkout ships a symlinked journal; OpenJSONL follows it, the torn-tail heal truncates the victim's last (newline-less) line, and every Append splices kiln journal JSON into the victim — silent corruption of any user-writable file the checkout names.", string(got), content)
	}
}
