//go:build red

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only;
// no fix applied).
//
// Property: secret-bearing files the framework writes get restrictive modes —
// the repo's own discipline (freeze world.json 0600, battery/log 0600, DEK
// 0600, uploads 0600).
//
// Surfaces: kiln/journal OpenJSONL file creation (journal.go:110
// os.OpenFile 0o644; the TruncateAfter tmp/reopen pair uses the same 0o644
// at journal.go:224/272) holding SetAppConfigPayload entries, which embed
// world.AppConfig verbatim — Auth.JWTSecret and Admin.SeedPassword values
// included (entry.go SetAppConfigPayload; world/world.go:136,145).
//
// Finding: the journal is the replay source for the exact same credential
// data freeze writes to world.json, and freeze treats that data as
// owner-only: envRef substitution for the YAML plus 0600 + fileperm.Restrict
// for the snapshot (kiln/freeze/freeze.go:51-71, pinned there). The journal
// stores the values verbatim in a 0644 file inside a 0755 dir, so on any
// shared or backed-up worktree the JWT secret and admin seed password are
// group/world-readable. The tool API that journals set_app_config is
// unauthenticated on the loopback listener, so any chat-driven session ends
// with secrets at 0644 on disk.
//
// Fix direction: create the journal 0600 (and the TruncateAfter tmp/reopen
// pair likewise). That is the fix-agnostic minimum asserted below: whatever
// representation the payload keeps, the file must be owner-only. If the
// journal ever adopts freeze-style envRef substitution for the same fields,
// revisit this pin (the second assert documents today's verbatim storage).
//
// Severity: medium. Kiln is a loopback dev tool, but the journal lands in
// the developer's project directory and inherits its sharing/backup
// exposure, and the repo already classified this exact data as 0600-only
// everywhere else it is written.

package journal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestJournalRedRestrictsSecretFile(t *testing.T) {
	const jwtSecret = "red-jwt-secret-0123456789abcdef"
	const seedPassword = "red-seed-password-ghijkl" // not-a-secret: test fixture value asserted to land in the journal

	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	j, err := OpenJSONL(path)
	if err != nil {
		t.Fatalf("OpenJSONL: %v", err)
	}
	e, err := NewEntry("cfg-red", time.Now().UTC(), KindWorldEdit, OpSetAppConfig, SetAppConfigPayload{
		Config: world.AppConfig{
			Name:  "redapp",
			Auth:  world.AuthConfig{Enabled: true, JWTSecret: jwtSecret},
			Admin: world.AdminConfig{Enabled: true, SeedEmail: "ops@red.test", SeedPassword: seedPassword},
		},
	})
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	if _, err := j.Append(e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := j.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Ground the severity: the credential values land in the file verbatim.
	// If this ever fails because the journal substitutes env refs (as freeze
	// does), this pin must be revisited, not blindly kept.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if !strings.Contains(string(data), jwtSecret) || !strings.Contains(string(data), seedPassword) {
		t.Fatalf("journal no longer stores app-config secrets verbatim; revisit this pin (payload=%s)", data)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat journal: %v", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: journal holding Auth.JWTSecret / Admin.SeedPassword is mode %o — group/world readable. "+
			"Attack: on any shared or synced worktree a co-user (or backup process) reads the live JWT secret and the "+
			"admin seed password straight out of .kiln.session.jsonl. Freeze writes this exact data owner-only "+
			"(world.json 0600 + envRef, freeze.go:51-71); the journal is the outlier at 0644 (journal.go:110 OpenFile, "+
			"and the TruncateAfter tmp/reopen pair at :224/:272). Fix: create the journal 0600.",
			fi.Mode().Perm())
	}
}
