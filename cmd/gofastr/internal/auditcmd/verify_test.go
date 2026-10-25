package auditcmd

import (
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// TestAuditLintPointsAtVerify pins the pointer between the two overlapping
// commands. `audit lint` predates the contract system and knows nothing
// about gofastr.settings.yml or //gofastr:allow, so it reports findings
// verify deliberately exempts. A project running both gets two answers
// to the same question, and nothing said which one is authoritative.
func TestAuditLintPointsAtVerify(t *testing.T) {
	dir := clitest.WriteModule(t, map[string]string{
		"db.go": `package main

import "database/sql"

func f(db *sql.DB, id string) {
	_, _ = db.Exec("DELETE FROM sessions WHERE id = $1", id)
}
`,
	})
	origExit, origStdout := cli.OsExit, os.Stdout
	cli.OsExit = func(int) {}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, readErr := r.Read(buf)
			b.Write(buf[:n])
			if readErr != nil {
				break
			}
		}
		done <- b.String()
	}()

	RunAudit([]string{"lint", dir})
	w.Close()
	out := <-done
	r.Close()
	cli.OsExit, os.Stdout = origExit, origStdout

	if !strings.Contains(out, "gofastr verify") {
		t.Errorf("audit lint does not point at its replacement:\n%s", out)
	}
	if !strings.Contains(out, "gofastr.settings.yml") {
		t.Errorf("audit lint does not say WHY verify is authoritative (it honours config):\n%s", out)
	}
	// The findings must still print. This is a pointer, not a removal.
	if !strings.Contains(out, "ignored-exec") {
		t.Errorf("audit lint stopped reporting its findings:\n%s", out)
	}
}
