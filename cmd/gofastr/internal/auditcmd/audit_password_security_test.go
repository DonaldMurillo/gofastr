package auditcmd

import (
	"os"
	"testing"
)

func TestAuditA11yRedPasswordEnv(t *testing.T) {
	t.Setenv("GOFASTR_AUDIT_PASSWORD", "env-secret")
	if got := resolveAuditPassword("-"); got != "env-secret" {
		t.Error("password marker did not resolve from GOFASTR_AUDIT_PASSWORD")
	}
}

func TestAuditA11yRedPasswordStdin(t *testing.T) {
	t.Setenv("GOFASTR_AUDIT_PASSWORD", "")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if _, err := w.WriteString("piped-secret\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	saved := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = saved }()
	if got := resolveAuditPassword("-"); got != "piped-secret" {
		t.Error("password marker did not resolve from stdin")
	}
}
