package harnesscmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
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

// hostileKey is a key name a cloned repo's .harness-secrets/env can
// carry: it ends in _API_KEY, which is all the secrets package checks,
// and before that it erases the line (CSI 2K), returns the cursor and
// writes the operator's clipboard (OSC 52). Printed live in the consent
// prompt it overwrote the warning D-34 exists to show.
const hostileKey = "\x1b[2K\r\x1b]52;c;cHdu\aZAI_API_KEY" // no "=": the parser cuts the name at the first one

// assertNoLiveEscape fails when out carries anything a terminal acts on
// rather than prints: C0 controls but tab and newline, DEL, C1, bidi and
// zero-width runes.
func assertNoLiveEscape(t *testing.T, what, out string) {
	t.Helper()
	for i, r := range out {
		if r == '\n' || r == '\t' {
			continue
		}
		if textsafe.IsUnsafe(r) {
			t.Errorf("SECURITY: [harness-consent-escape] %s printed %U at byte %d live to the terminal:\n%q", what, r, i, out)
			return
		}
	}
}

// writeRepoEnv replaces the cloned repo's secrets file.
func writeRepoEnv(t *testing.T, repo, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, ".harness-secrets", "env"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A key that is not an environment variable name is refused before the
// operator is asked anything: the prompt never shows, a "y" typed ahead
// approves nothing, no key from the file is set or remembered, and what
// is printed instead carries no live escape.
func TestRepoSecretsHostileKeyNameRefusedBeforePrompt(t *testing.T) {
	repo, xdg := clonedRepo(t)
	writeRepoEnv(t, repo, "OPENROUTER_API_KEY=sk-author\n"+hostileKey+"=sk-hostile\n")
	t.Cleanup(func() { _ = os.Unsetenv(hostileKey) })
	for _, interactive := range []bool{true, false} {
		_ = os.Unsetenv("OPENROUTER_API_KEY")
		_ = os.Unsetenv(hostileKey)
		var out bytes.Buffer
		loadRepoSecrets(xdg, false, interactive, strings.NewReader("y\n"), &out)
		if strings.Contains(out.String(), "[y/N]") {
			t.Errorf("SECURITY: interactive=%v: the consent prompt was shown for a file with a key that is not an environment variable name:\n%q", interactive, out.String())
		}
		if _, ok := os.LookupEnv("OPENROUTER_API_KEY"); ok {
			t.Errorf("SECURITY: interactive=%v: a file naming a hostile key still set OPENROUTER_API_KEY", interactive)
		}
		if _, ok := os.LookupEnv(hostileKey); ok {
			t.Errorf("SECURITY: interactive=%v: the hostile key name was set in the environment", interactive)
		}
		if !strings.Contains(out.String(), "environment variable name") {
			t.Errorf("interactive=%v: the refusal must say why:\n%q", interactive, out.String())
		}
		assertNoLiveEscape(t, "the refusal", out.String())
	}
	if _, err := os.Stat(filepath.Join(xdg, secrets.ApprovalsFile)); !os.IsNotExist(err) {
		t.Errorf("SECURITY: a refused file left an approval behind: %v", err)
	}

	// --allow-repo-secrets asks nobody, so the secrets package sets the
	// keys before this command sees their names; they come back out.
	var out bytes.Buffer
	loadRepoSecrets(xdg, true, false, strings.NewReader(""), &out)
	if _, ok := os.LookupEnv("OPENROUTER_API_KEY"); ok {
		t.Error("SECURITY: --allow-repo-secrets: a file naming a hostile key still set OPENROUTER_API_KEY")
	}
	if _, ok := os.LookupEnv(hostileKey); ok {
		t.Error("SECURITY: --allow-repo-secrets: the hostile key name was set in the environment")
	}
	if !strings.Contains(out.String(), "environment variable name") {
		t.Errorf("--allow-repo-secrets: the refusal must say why:\n%q", out.String())
	}
	assertNoLiveEscape(t, "the --allow-repo-secrets refusal", out.String())
}

// The names a file may set are environment variable names; the
// ordinary ones pass.
func TestRepoSecretsKeyNameShape(t *testing.T) {
	for k, want := range map[string]bool{
		"ZAI_API_KEY": true, "OPENROUTER_API_KEY": true, "GITHUB_TOKEN": true, "A1_TOKEN": true,
		"": false, "1_API_KEY": false, "_API_KEY": false, "zai_api_key": false, "ZAI-API_KEY": false,
		hostileKey: false, "ZAI_API_KEY\u202e": false, "ZAİ_API_KEY": false,
		"ZAi_API_KEY": false, "ZAI_API_KEY\n": false, "ZAI API_KEY": false,
	} {
		if got := checkKeyNames([]string{"OPENAI_API_KEY", k}) == nil; got != want {
			t.Errorf("checkKeyNames(%q) passes = %v, want %v", k, got, want)
		}
	}
}

// The prompt and the refusal print the secrets file's path and the
// repository directory, both named by whoever made the checkout. A
// directory name carrying escapes reaches the terminal inert.
func TestRepoSecretsPromptAndRefusalCarryNoLiveEscapes(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "evil\x1b]52;c;cHduZWQ=\a\x1b[2J\u009b31m\u202e")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".harness-secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeRepoEnv(t, repo, "OPENROUTER_API_KEY=sk-author\n")
	t.Chdir(repo)
	t.Setenv("OPENROUTER_API_KEY", "")
	if err := os.Unsetenv("OPENROUTER_API_KEY"); err != nil {
		t.Fatal(err)
	}
	xdg := t.TempDir()

	var prompt bytes.Buffer
	loadRepoSecrets(xdg, false, true, strings.NewReader("n\n"), &prompt)
	if !strings.Contains(prompt.String(), "[y/N]") || !strings.Contains(prompt.String(), "cHduZWQ=") {
		t.Fatalf("setup broken: the prompt did not show the repository path:\n%q", prompt.String())
	}
	assertNoLiveEscape(t, "the consent prompt", prompt.String())

	var refusal bytes.Buffer
	loadRepoSecrets(xdg, false, false, strings.NewReader(""), &refusal)
	if !strings.Contains(refusal.String(), "--allow-repo-secrets") || !strings.Contains(refusal.String(), "cHduZWQ=") {
		t.Fatalf("setup broken: the refusal did not name the file:\n%q", refusal.String())
	}
	assertNoLiveEscape(t, "the refusal line", refusal.String())

	// A malformed file reaches the error line with its path.
	writeRepoEnv(t, repo, "no equals sign\n")
	var errLine bytes.Buffer
	loadRepoSecrets(xdg, false, false, strings.NewReader(""), &errLine)
	if !strings.Contains(errLine.String(), "cHduZWQ=") {
		t.Fatalf("setup broken: the error line did not name the file:\n%q", errLine.String())
	}
	assertNoLiveEscape(t, "the error line", errLine.String())
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
