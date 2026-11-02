package harnesscmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/secrets"
)

// clonedRepo makes a repository whose .harness-secrets/env carries the
// author's provider key, chdirs into it, and unsets the operator's own.
func clonedRepo(t *testing.T) (repo, xdg string) {
	t.Helper()
	repo = t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".harness-secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".harness-secrets", "env"), []byte("OPENROUTER_API_KEY=sk-author\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	t.Setenv("OPENROUTER_API_KEY", "")
	if err := os.Unsetenv("OPENROUTER_API_KEY"); err != nil {
		t.Fatal(err)
	}
	return repo, t.TempDir()
}

// A cloned repo's .harness-secrets/env supplied provider keys whenever
// the operator had none, so the session ran on the repo author's
// account with no word to the operator. A run with nobody to ask (MCP
// over stdio, a pipe, CI) refuses them and says so.
func TestRepoSecretsRefusedWhenNobodyCanBeAsked(t *testing.T) {
	_, xdg := clonedRepo(t)
	var out bytes.Buffer
	loadRepoSecrets(xdg, false, false, strings.NewReader(""), &out)
	if v, ok := os.LookupEnv("OPENROUTER_API_KEY"); ok {
		t.Fatalf("SECURITY: a non-interactive run used the repository's key %q with no approval", v)
	}
	if !strings.Contains(out.String(), "OPENROUTER_API_KEY") || !strings.Contains(out.String(), "--allow-repo-secrets") {
		t.Errorf("the refusal must name the key and the flag:\n%s", out.String())
	}
}

// Interactive: the operator is asked once; "n" leaves the key unset,
// "y" sets it and is remembered for this repository, so the next run
// neither asks nor refuses.
func TestRepoSecretsAskOnceAndRemember(t *testing.T) {
	_, xdg := clonedRepo(t)
	var out bytes.Buffer
	loadRepoSecrets(xdg, false, true, strings.NewReader("n\n"), &out)
	if _, ok := os.LookupEnv("OPENROUTER_API_KEY"); ok {
		t.Fatal("SECURITY: answering no still set the repository's key")
	}
	if !strings.Contains(out.String(), "OPENROUTER_API_KEY") {
		t.Errorf("the prompt must name the key:\n%s", out.String())
	}

	loadRepoSecrets(xdg, false, true, strings.NewReader("y\n"), &out)
	if got := os.Getenv("OPENROUTER_API_KEY"); got != "sk-author" {
		t.Fatalf("answering yes: key = %q", got)
	}
	if err := os.Unsetenv("OPENROUTER_API_KEY"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	loadRepoSecrets(xdg, false, false, strings.NewReader(""), &out)
	if got := os.Getenv("OPENROUTER_API_KEY"); got != "sk-author" {
		t.Fatalf("a remembered approval was not honoured: key = %q\n%s", got, out.String())
	}
	if _, err := os.Stat(filepath.Join(xdg, secrets.ApprovalsFile)); err != nil {
		t.Errorf("approval not stored: %v", err)
	}
}

// --allow-repo-secrets uses the keys for one run without remembering.
func TestRepoSecretsFlagAllowsOneRun(t *testing.T) {
	_, xdg := clonedRepo(t)
	var out bytes.Buffer
	loadRepoSecrets(xdg, true, false, strings.NewReader(""), &out)
	if got := os.Getenv("OPENROUTER_API_KEY"); got != "sk-author" {
		t.Fatalf("--allow-repo-secrets: key = %q", got)
	}
	if _, err := os.Stat(filepath.Join(xdg, secrets.ApprovalsFile)); !os.IsNotExist(err) {
		t.Errorf("the flag must not remember an approval: %v", err)
	}
}
