package analyzers_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

// The hardcoded-secret rule blanks its snippet so a report never prints
// the credential back out. That property has to hold in EVERY formatter,
// not just the one it was implemented against — a finding routed to CI
// logs through --json or to code scanning through --sarif is more exposed
// than the terminal, not less.
//
// The secret below is deliberately NOT the one in the rule's catalog
// example: searching for that value finds the documentation rather than a
// leak, which is a test that passes for the wrong reason.
func TestHardcodedSecretIsRedactedInEveryFormat(t *testing.T) {
	const secret = "sk-live-QQ77zzWWvvUU11223344aabb"

	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/app\n\ngo 1.26\n")
	write("cfg.go", "package main\n\nvar apiKey = \""+secret+"\"\n")

	pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"security"}})
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, d := range report.Diagnostics {
		if d.RuleID == contracts.RuleHardcodedSecret {
			found = true
			if strings.Contains(d.Snippet, secret) {
				t.Errorf("the diagnostic snippet carries the secret: %q", d.Snippet)
			}
			if strings.Contains(d.Message, secret) {
				t.Errorf("the diagnostic message carries the secret: %q", d.Message)
			}
		}
	}
	if !found {
		t.Fatal("the fixture did not trip the hardcoded-secret rule; the gate would be vacuous")
	}

	jsonOut, err := contracts.FormatJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	sarifOut, err := contracts.FormatSARIF(report, "test")
	if err != nil {
		t.Fatal(err)
	}
	textOut := contracts.FormatText(report, contracts.TextOptions{})

	for _, f := range []struct {
		name string
		body string
	}{
		{"--json", string(jsonOut)},
		{"--sarif", string(sarifOut)},
		{"text", textOut},
	} {
		if strings.Contains(f.body, secret) {
			t.Errorf("%s output echoes the secret", f.name)
		}
	}
}
