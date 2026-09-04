package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// The export bundle is written owner-only: it is the session event log
// carried off-box, and the operator may point OutPath at a shared dir
// believing the bundle inherits the store's posture. The sibling sqlite
// store already writes this data owner-only (0700 dir / 0600 files).
func TestExportZipIsOwnerOnly(t *testing.T) {
	b := &ExportBundle{
		Store:   &fakeStore{}, // from export_test.go — no events needed to pin the mode
		Session: ids.SessionID("owner-only-session"),
		Profile: "test",
		Level:   RedactStrict,
		OutPath: filepath.Join(t.TempDir(), "bundle.zip"),
	}
	path, err := b.Write(context.Background())
	if err != nil {
		t.Fatalf("ExportBundle.Write: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat bundle: %v", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: session export bundle is mode %o — the session event log carried off-box lands "+
			"group/world-readable wherever the operator pointed OutPath. The sibling session store writes this "+
			"data owner-only (0700 dir / 0600 files, session/sqlite); the tmp zip must be created 0600.",
			fi.Mode().Perm())
	}
}
