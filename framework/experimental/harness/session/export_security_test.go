package session

import (
	"context"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"

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

	filepermtest.AssertOwnerOnly(t, path, 0o600)
}
