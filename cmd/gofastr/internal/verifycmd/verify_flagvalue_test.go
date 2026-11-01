package verifycmd

import (
	"strings"
	"testing"
)

// `gofastr verify --sarif --json` took `--json` as the SARIF file name
// and wrote a file called `--json` (2026-10-31 bug hunt S63). A valued
// flag in the space form now refuses a next token that looks like a flag,
// and a valued flag with nothing after it says so.
func TestValuedFlagRefusesFlagValue(t *testing.T) {
	for _, flag := range []string{"--config", "--explain", "--sarif", "--analyzer", "--rule", "--root", "--baseline"} {
		t.Run(flag, func(t *testing.T) {
			_, err := parseVerifyArgs([]string{flag, "--json"})
			if err == nil || !strings.Contains(err.Error(), flag+" needs a value") {
				t.Fatalf("%s --json: err = %v, want %q", flag, err, flag+" needs a value")
			}
			_, err = parseVerifyArgs([]string{flag})
			if err == nil || !strings.Contains(err.Error(), flag+" needs a value") {
				t.Fatalf("bare %s: err = %v, want %q", flag, err, flag+" needs a value")
			}
		})
	}
}

// Both spellings that name a file still do.
func TestSarifPathBothForms(t *testing.T) {
	for _, args := range [][]string{
		{"--sarif", "out.sarif", "--json"},
		{"--sarif=out.sarif", "--json"},
	} {
		opts, err := parseVerifyArgs(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if opts.sarifPath != "out.sarif" || !opts.json {
			t.Fatalf("%v: sarifPath=%q json=%v", args, opts.sarifPath, opts.json)
		}
	}
}
