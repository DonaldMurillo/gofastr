package harnesscmd

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/secrets"
)

// loadRepoSecrets loads the provider keys a repository's
// .harness-secrets/env supplies, but only with the operator's consent:
// an approval remembered for this directory and key set, the
// --allow-repo-secrets flag (allow), or a yes to the prompt when the
// run is interactive, which is remembered. A run with nobody to ask
// refuses the keys and says how to allow them. Keys the environment
// already holds are never asked about: the shell always wins.
//
// A key that is not an environment variable name never reaches the
// prompt or the environment: the secrets package's own
// isProviderCredentialKey checks the whole name at the source (#669).
// Every value printed here (key names, the file's path, the repository
// directory) is scrubbed too (cli.Fprintf).
func loadRepoSecrets(xdgConfig string, allow, interactive bool, in io.Reader, out io.Writer) {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	var ask secrets.Ask
	if interactive {
		reader := bufio.NewReader(in)
		ask = func(req secrets.Request) (bool, error) {
			cli.Fprintf(out, "This repository's %s would set %s, which your environment does not.\n"+
				"A session on that key runs on that owner's account, and the owner can read your prompts.\n"+
				"Use it for %s from now on? [y/N] ",
				req.Path, strings.Join(req.Keys, ", "), req.Dir)
			line, err := reader.ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return false, err
			}
			answer := strings.ToLower(strings.TrimSpace(line))
			return answer == "y" || answer == "yes", nil
		}
	}
	res, err := secrets.LoadWithConsent(wd, filepath.Join(xdgConfig, secrets.ApprovalsFile), allow, ask)
	switch {
	case errors.Is(err, secrets.ErrRefused):
		cli.Fprintf(out, "gofastr harness: not using %s from %s: this repository's keys are not approved. "+
			"Approve them in an interactive `gofastr harness` run, or pass --allow-repo-secrets for this run.\n",
			strings.Join(res.Keys, ", "), res.Path)
	case err != nil:
		cli.Fprintf(out, "gofastr harness: %s: %v\n", res.Path, err)
	}
}
