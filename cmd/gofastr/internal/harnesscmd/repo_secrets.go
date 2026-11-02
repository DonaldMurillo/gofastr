package harnesscmd

import (
	"bufio"
	"errors"
	"fmt"
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
// A file naming a key that is not an environment variable name
// (keyNameError) is refused whole, before anyone is asked: the key
// names reach the prompt, and a name such as "\x1b[2K\r\x1b]52;…ZAI_API_KEY"
// ends in _API_KEY, which is all the secrets package checks, while it
// erases the warning the prompt exists to show and writes the operator's
// clipboard. Every value printed here (key names, the file's path, the
// repository directory) is scrubbed too (cli.Fprintf).
func loadRepoSecrets(xdgConfig string, allow, interactive bool, in io.Reader, out io.Writer) {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	var ask secrets.Ask
	if interactive {
		reader := bufio.NewReader(in)
		ask = func(req secrets.Request) (bool, error) {
			if err := checkKeyNames(req.Keys); err != nil {
				return false, err
			}
			cli.Fprintf(out, "This repository's %s would set %s, which your environment does not.\n"+
				"A session on that key runs on its owner's account, and the owner can read your prompts.\n"+
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
	if kerr := checkKeyNames(res.Keys); kerr != nil && err == nil {
		// No prompt ran (--allow-repo-secrets, or a remembered approval),
		// so the keys were set; LoadWithConsent sets only keys the
		// environment lacked, so unsetting them restores it.
		if res.Applied {
			for _, k := range res.Keys {
				_ = os.Unsetenv(k)
			}
		}
		err = kerr
	}
	switch {
	case errors.Is(err, secrets.ErrRefused):
		if kerr := checkKeyNames(res.Keys); kerr != nil {
			cli.Fprintf(out, "gofastr harness: %s: %v\n", res.Path, kerr)
			return
		}
		cli.Fprintf(out, "gofastr harness: not using %s from %s: this repository's keys are not approved. "+
			"Approve them in an interactive `gofastr harness` run, or pass --allow-repo-secrets for this run.\n",
			strings.Join(res.Keys, ", "), res.Path)
	case err != nil:
		cli.Fprintf(out, "gofastr harness: %s: %v\n", res.Path, err)
	}
}

// keyNameError refuses a repository secrets file that names a key which
// is not an environment variable name.
type keyNameError struct{ key string }

func (e keyNameError) Error() string {
	// %q spells every control byte as an escape, so the name is shown,
	// never acted on.
	return fmt.Sprintf("not using this file: it names %q, which is not an environment variable name "+
		"(capital letters, digits and _, starting with a letter)", e.key)
}

// checkKeyNames refuses keys unless every one is an environment variable
// name: ^[A-Z][A-Z0-9_]*$, the shape of every provider key the file
// exists to carry (ZAI_API_KEY, OPENROUTER_API_KEY, GITHUB_TOKEN).
func checkKeyNames(keys []string) error {
	for _, k := range keys {
		if !isEnvName(k) {
			return keyNameError{k}
		}
	}
	return nil
}

func isEnvName(k string) bool {
	if k == "" || k[0] < 'A' || k[0] > 'Z' {
		return false
	}
	for i := 1; i < len(k); i++ {
		c := k[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}
