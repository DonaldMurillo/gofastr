package main

import (
	"strings"
	"testing"
)

// TestFuzzySuggestionsIncludeNewAndTheme pins that the unknown-command
// "Did you mean" list covers every top-level subcommand. It used to omit
// `new` and `theme`, so `gofastr ne` and `gofastr them` got no suggestion.
func TestFuzzySuggestionsIncludeNewAndTheme(t *testing.T) {
	for _, tc := range []struct {
		typo string
		want string
	}{
		{"ne", "new"},
		{"them", "theme"},
	} {
		out := covT_capStdout(t, func() {
			_ = covT_capExit(t, func() { dispatch([]string{tc.typo}) })
		})
		want := "Did you mean: " + bold("gofastr "+tc.want) + "?"
		if !strings.Contains(out, want) {
			t.Errorf("typo %q did not suggest %q; output:\n%s", tc.typo, tc.want, out)
		}
	}
}

// TestSubcommandHelpIsCommandSpecific pins that `<cmd> --help` routes to the
// command's own usage instead of the 80-line global overview. ownsHelp used
// to miss 8 dispatch subcommands (new, pack, build, dev, migrate, test,
// harness, agents), so each fell through to printHelp().
func TestSubcommandHelpIsCommandSpecific(t *testing.T) {
	for _, cmd := range []string{"new", "pack", "build", "dev", "migrate", "test", "harness", "agents"} {
		out := covT_capStdout(t, func() { dispatch([]string{cmd, "--help"}) })
		if want := "Usage: gofastr " + cmd; !strings.Contains(out, want) {
			t.Errorf("%s --help missing command-specific %q:\n%s", cmd, want, out)
		}
		// "Start dev server with auto-restart" only appears in the global
		// printHelp() Commands listing — its presence means we fell through.
		if strings.Contains(out, "Start dev server with auto-restart") {
			t.Errorf("%s --help fell back to the global help page:\n%s", cmd, out)
		}
	}
}
