package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type profileRecordingShell struct {
	Shell
	id, dir string
	err     error
}

func (s *profileRecordingShell) PrepareProfile(id, dir string) (string, error) {
	s.id, s.dir = id, dir
	return "127.0.0.1:43127", s.err
}
func (*profileRecordingShell) CommitProfile(string) error { return nil }
func (*profileRecordingShell) ReleaseProfile()            {}
func TestProfileDefaultUntouched(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-created")
	s := &profileRecordingShell{Shell: NewUnsupportedShell()}
	b := New(Config{ID: "dev.gofastr.profile", DataDir: dir, Shell: s})
	addr, p, err := b.prepareProfile()
	if err != nil || p != nil || addr != "127.0.0.1:0" || s.id != "" {
		t.Fatalf("default addr=%s profile=%v err=%v", addr, p, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("default touched profile storage")
	}
}
func TestProfileUnsupportedExplicit(t *testing.T) {
	b := New(Config{ID: "dev.gofastr.profile", PersistentProfile: true})
	if _, _, err := b.prepareProfile(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported profile: %v", err)
	}
}
func TestProfileResolvedIdentity(t *testing.T) {
	base := t.TempDir()
	t.Setenv(dataDirEnv, base)
	s := &profileRecordingShell{Shell: NewUnsupportedShell()}
	b := New(Config{ID: "dev.gofastr.profile", Title: "not an identity", PersistentProfile: true, Shell: s})
	addr, p, err := b.prepareProfile()
	if err != nil || p != s || addr != "127.0.0.1:43127" || s.id != b.cfg.ID || s.dir != filepath.Join(base, b.cfg.ID) || b.dataDir != s.dir {
		t.Fatalf("profile preparation: %s %v id=%s dir=%s", addr, err, s.id, s.dir)
	}
	if _, err := os.Stat(s.dir); !os.IsNotExist(err) {
		t.Fatal("path resolution mutated storage before native preflight")
	}
}

type profileLifecycleShell struct {
	launchLifecycleShell
	profilePrepared, profileCommitted, profileReleased atomic.Int32
	commitErr                                          error
}

func (s *profileLifecycleShell) PrepareProfile(_, dir string) (string, error) {
	s.profilePrepared.Add(1)
	return "127.0.0.1:0", os.MkdirAll(dir, 0o700)
}
func (s *profileLifecycleShell) CommitProfile(string) error {
	s.profileCommitted.Add(1)
	return s.commitErr
}
func (s *profileLifecycleShell) ReleaseProfile() { s.profileReleased.Add(1) }
func TestProfileActivationPrecedesLease(t *testing.T) {
	s := &profileLifecycleShell{launchLifecycleShell: launchLifecycleShell{Shell: NewUnsupportedShell(), handled: true}}
	b, app := launchLifecycleApp(t, "notes", s, false)
	b.cfg.PersistentProfile = true
	if err := b.Run(app); err != nil {
		t.Fatal(err)
	}
	if s.prepared.Load() != 1 || s.profilePrepared.Load() != 0 || s.ran.Load() != 0 || b.app != nil {
		t.Fatal("secondary activation attempted profile/app startup")
	}
}
func TestProfileStartupCleanup(t *testing.T) {
	for _, stage := range []string{"app-failure", "origin-failure", "shell-return"} {
		t.Run(stage, func(t *testing.T) {
			s := &profileLifecycleShell{launchLifecycleShell: launchLifecycleShell{Shell: NewUnsupportedShell()}}
			if stage == "origin-failure" {
				s.commitErr = errors.New("fixture origin error")
			}
			b, app := launchLifecycleApp(t, "", s, stage != "app-failure")
			b.cfg.PersistentProfile = true
			err := b.Run(app)
			if stage == "app-failure" && (err == nil || !strings.Contains(err.Error(), "no *uihost.UIHost")) {
				t.Fatalf("startup result: %v", err)
			}
			if stage == "origin-failure" && !errors.Is(err, s.commitErr) {
				t.Fatalf("origin result: %v", err)
			}
			if stage == "shell-return" && err != nil {
				t.Fatal(err)
			}
			wantCommitted, wantRun := int32(1), int32(0)
			if stage == "app-failure" {
				wantCommitted = 0
			}
			if stage == "shell-return" {
				wantRun = 1
			}
			if s.profilePrepared.Load() != 1 || s.profileReleased.Load() != 1 || s.profileCommitted.Load() != wantCommitted || s.ran.Load() != wantRun {
				t.Fatalf("profile prepare/commit/release/run=%d/%d/%d/%d", s.profilePrepared.Load(), s.profileCommitted.Load(), s.profileReleased.Load(), s.ran.Load())
			}
		})
	}
}
