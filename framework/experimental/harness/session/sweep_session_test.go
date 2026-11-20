package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

// A leftover world-readable .tmp from an earlier export is tightened before the bundle streams into it.
func TestExportTightensLeftoverTmp(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle.zip")
	if err := os.WriteFile(out+".tmp", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(out+".tmp", 0o644); err != nil {
		t.Fatal(err)
	}
	b := &ExportBundle{Store: &fakeStore{}, Session: ids.SessionID("s"), Level: RedactStrict, OutPath: out}
	path, err := b.Write(context.Background())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	filepermtest.AssertOwnerOnly(t, path, 0o600)
}
