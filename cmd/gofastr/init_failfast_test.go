package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A failed versioned migration must abort boot. The scaffold used to log
// "Migration warning" and call Start anyway, so a deploy whose committed
// migration did not apply still reported a ready server; the fault then
// surfaces as a later request error instead of a failed rollout. The ready
// banner's own comment promises it fires only after migrations succeeded.
func TestInitMainFailsClosedOnMigrationError(t *testing.T) {
	bin := buildGofastrBin(t)
	work := t.TempDir()
	cmd := exec.Command(bin, "init", "migrfail", "--module=example.com/migrfail")
	cmd.Dir = work
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gofastr init: %v\n%s", err, out)
	}
	src, err := os.ReadFile(filepath.Join(work, "migrfail", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	mainSrc := string(src)
	if strings.Contains(mainSrc, "Migration warning") {
		t.Fatalf("generated main.go warns and boots on a failed migration (fail-open):\n%s", mainSrc)
	}
	if !strings.Contains(mainSrc, `log.Fatalf("migrations: %v", err)`) {
		t.Fatalf("generated main.go does not abort on migrator.Up error:\n%s", mainSrc)
	}
}
