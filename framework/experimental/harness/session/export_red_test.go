//go:build red

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only;
// no fix applied).
//
// Property: secret-bearing files the framework writes get restrictive modes —
// the repo's own discipline (freeze world.json 0600, battery/log 0600, DEK
// 0600, uploads 0600).
//
// Surfaces: harness session ExportBundle.Write (export.go:55) — os.Create
// on the tmp zip, i.e. umask-default 0644, then rename. The sibling session
// store already enforces the discipline: sqlite store dir 0700 / files 0600
// (session/sqlite/sqlite.go), DEK 0700/0600 (dek.go), both pinned.
//
// Finding: the export zip is the session event log carried off-box — even at
// RedactStrict the manifest names the session, profile, and model, and the
// operator chose an OutPath (often a shared dir or /tmp) believing the
// bundle inherits the store's owner-only posture. It does not: 0644.
//
// Fix direction: os.OpenFile(tmp, O_WRONLY|O_CREATE|O_TRUNC, 0o600) instead
// of os.Create at export.go:55.
//
// Severity: low-medium, and honestly labeled: this is framework/experimental
// (the harness is experimental surface), and content is redaction-gated.
// The parity gap with the sibling store is the finding.

package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

func TestHarnessExportRedRestrictsZip(t *testing.T) {
	b := &ExportBundle{
		Store:   &fakeStore{}, // from export_test.go — no events needed to pin the mode
		Session: ids.SessionID("red-session"),
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
			"data owner-only (0700 dir / 0600 files, session/sqlite); the export is the outlier via os.Create at "+
			"export.go:55. Fix: create the tmp zip 0600.",
			fi.Mode().Perm())
	}
}
