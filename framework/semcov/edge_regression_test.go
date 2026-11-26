package semcov

import "testing"

func TestEnableMovedRootFlushesAccumulatedCoverage(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	Enable(t.TempDir())
	RecordEvent("done")
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	next := t.TempDir()
	Enable(next)
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	m, err := Read(next)
	if err != nil || !m.CoveredEvent("done") {
		t.Fatalf("coverage at new root = %v, %v", m, err)
	}
}

func TestCoveredRouteTrimsMethod(t *testing.T) {
	m := &Manifest{Routes: map[string][]string{"/test": {"GET"}}}
	if !m.CoveredRoute(" get ", "/test") {
		t.Fatal("route lookup disagrees with recording normalization")
	}
}

func TestCoveredStringsTrimLikeRecorder(t *testing.T) {
	m := &Manifest{Events: []string{"event"}, Roles: []string{"role"}, Permissions: []string{"permission"}}
	if !m.CoveredEvent(" event ") || !m.CoveredRole(" role ") || !m.CoveredPermission(" permission ") {
		t.Fatal("string lookups disagree with recording normalization")
	}
}
