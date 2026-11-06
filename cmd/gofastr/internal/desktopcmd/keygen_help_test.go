package desktopcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeygenHelpShortCircuitsLaterArguments(t *testing.T) {
	for _, help := range []string{"--help", "-h"} {
		for _, tail := range [][]string{{"--bogus"}, {"-o"}, {"unexpected"}} {
			path := filepath.Join(t.TempDir(), "update.key")
			args := append([]string{"-o", path, help}, tail...)
			output, code := verbOutput(t, func() { runDesktopKeygen(args) })
			if code != 0 || !strings.Contains(output, "Usage: gofastr desktop") {
				t.Errorf("keygen %v exited %d instead of printing help: %s", args, code, output)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("help created a private key: %v", err)
			}
		}
	}
}

func TestKeygenHelpPreservesEarlierFlagErrors(t *testing.T) {
	for _, args := range [][]string{{"--bogus", "--help"}, {"-o", "--help"}} {
		_, code := verbOutput(t, func() { parseDesktopKeygenFlags(args) })
		if code != 1 {
			t.Errorf("keygen %v exited %d, want refusal before help", args, code)
		}
	}
}
