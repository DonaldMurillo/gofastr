package deploycmd

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestUnitKeepsTheAppUpAndInProduction(t *testing.T) {
	unit := plan{name: "shop", port: 8080}.unit()
	for _, want := range []string{
		"StartLimitIntervalSec=0\n", "Restart=always\n", "User=shop\n", "Group=shop\n",
		"StateDirectory=shop\n", "ProtectSystem=strict\n", "ProtectHome=true\n", "NoNewPrivileges=true\n",
		"Environment=HOME=/var/lib/shop\n", "Environment=XDG_STATE_HOME=/var/lib/shop/state\n",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("the unit lacks %q", want)
		}
	}
	prod, envFile := strings.Index(unit, "Environment=GOFASTR_ENV=production\n"), strings.Index(unit, "EnvironmentFile=")
	if prod < 0 || envFile < 0 || prod > envFile {
		t.Errorf("GOFASTR_ENV=production must come before the env file, which may override it:\n%s", unit)
	}
	if strings.Contains(unit, ".socket") {
		t.Errorf("a unit without a held port names a socket:\n%s", unit)
	}
}

func TestUploadsArePrivate(t *testing.T) {
	s := newServer(t)
	if _, err := s.run(t0, "deploy@example.com", "--name", "shop"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.calls[0], "uname -m && umask 077 && mktemp -d") {
		t.Errorf("the upload directory is not asked for private: %q", s.calls[0])
	}
	if !slices.ContainsFunc(s.calls, func(c string) bool {
		return strings.HasPrefix(c, "umask 077 && cat > ") && strings.HasSuffix(c, "/bin'")
	}) {
		t.Errorf("the binary is not copied under umask 077: %q", s.calls)
	}
}

func fakeSSH(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for ssh")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argsFile + "'\n" + body
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}

func TestExecRunnerEndsSSHOptionsBeforeTheTarget(t *testing.T) {
	argsFile := fakeSSH(t, "echo out; echo err >&2; exit 7\n")
	o := &options{target: "deploy@example.com", sshPort: 2222, identity: "key"}
	out, stderr, code, err := execRunner{}.ssh(context.Background(), o, "uptime", nil)
	if err != nil || code != 7 || out != "out\n" || stderr != "err\n" {
		t.Fatalf("a remote exit 7 came back as out=%q stderr=%q code=%d err=%v", out, stderr, code, err)
	}
	raw, _ := os.ReadFile(argsFile)
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "-p", "2222", "-i", "key", "--", "deploy@example.com", "uptime"}
	if !slices.Equal(args, want) {
		t.Fatalf("ssh got %q, want %q", args, want)
	}
}

func TestExecRunnerTreats255AsSSHFailing(t *testing.T) {
	fakeSSH(t, "echo 'Connection refused' >&2; exit 255\n")
	_, _, code, err := execRunner{}.ssh(context.Background(), &options{target: "prod"}, "uptime", nil)
	if err == nil || code != 255 || !strings.Contains(err.Error(), "could not reach prod") || !strings.Contains(err.Error(), "Connection refused") {
		t.Fatalf("ssh's own failure came back as code=%d err=%v", code, err)
	}
}

func TestExecRunnerDoesNotWaitForAChildHoldingThePipe(t *testing.T) {
	fakeSSH(t, "sleep 60 &\nexit 0\n")
	start := time.Now()
	_, _, _, _ = execRunner{}.ssh(context.Background(), &options{target: "prod"}, "uptime", nil)
	waited := time.Since(start)
	if waited > 20*time.Second {
		t.Fatalf("ssh waited %s for a child that kept its output open", waited)
	}
	if waited < 10*time.Second {
		t.Fatalf("ssh gave up on its output after %s, before the 10 seconds it gets", waited)
	}
}
