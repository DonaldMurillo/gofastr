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
