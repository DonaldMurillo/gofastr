package generatecmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
)

// cliOriginProbe runs inside the generated CLI's package: sameOrigin
// decides whether the stored token goes with a call.
const cliOriginProbe = `package main

import "testing"

func TestStoredTokenOrigin(t *testing.T) {
	for _, c := range []struct {
		stored, call string
		same         bool
	}{
		{"https://i.test", "https://I.TEST/", true},
		{"https://i.test", "https://i.test:443", true},
		// strings.ToLower folds U+0130 to "i"; net/http dials xn--i-9bb.test.
		{"https://i.test", "https://İ.test", false},
		{"https://İ.test", "https://İ.test", false},
		{"https://k.test", "https://K.test", false},
		{"http://[fe80::1%25eth0]:8080", "http://[FE80::1%25eth0]:8080", true},
		{"http://[fe80::1%25eth0]:8080", "http://[fe80::1%25ETH0]:8080", false},
		{"https://i.test", "http://i.test", false},
	} {
		if got := sameOrigin(c.call, c.stored); got != c.same {
			t.Errorf("sameOrigin(%q, %q) = %v, want %v (originOf %q vs %q)", c.call, c.stored, got, c.same, originOf(c.call), originOf(c.stored))
		}
	}
}
`

// The stored token's origin check folds ASCII case only: a host spelled
// with a character Go folds onto ASCII names another host on the wire.
func TestCLIOriginRefusesUnicodeFold(t *testing.T) {
	dir := cliModuleFromYAML(t, cliWireFixtureYAML)
	appgen.RunGenerateCLI([]string{"--binary=myapp"})
	if err := os.WriteFile(filepath.Join(dir, "cmd", "myapp", "origin_test.go"), []byte(cliOriginProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestStoredTokenOrigin$", "./cmd/myapp")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
