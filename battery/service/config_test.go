package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

// Building an app resolves isolation settings: framework/isolation looks
// for gofastr.settings.yml from the package directory up to the repo
// root, so these tests read the root file.
//
//gofastr:test-reads /gofastr.settings.yml

func TestValidateConfig(t *testing.T) {
	ok := Config{ID: "dev.gofastr.hello", Name: "hello", Port: 47811}
	if err := validateConfig(ok); err != nil {
		t.Fatalf("valid config refused: %v", err)
	}
	opened := ok
	opened.OpenPrefixes = []string{"/api", "/hooks/v1"}
	opened.BindAddress = "0.0.0.0"
	if err := validateConfig(opened); err != nil {
		t.Fatalf("valid OpenPrefixes/BindAddress refused: %v", err)
	}
	bad := map[string]func(*Config){
		"empty id":          func(c *Config) { c.ID = "" },
		"no dot":            func(c *Config) { c.ID = "hello" },
		"leading dash":      func(c *Config) { c.ID = "-dev.hello" },
		"leading dot":       func(c *Config) { c.ID = ".dev.hello" },
		"trailing dot":      func(c *Config) { c.ID = "dev.hello." },
		"empty segment":     func(c *Config) { c.ID = "dev..hello" },
		"slash":             func(c *Config) { c.ID = "dev/hello.x" },
		"space":             func(c *Config) { c.ID = "dev.hel lo" },
		"non-ascii":         func(c *Config) { c.ID = "dev.hellö" },
		"long id":           func(c *Config) { c.ID = "a." + strings.Repeat("b", 199) },
		"name with slash":   func(c *Config) { c.Name = "a/b" },
		"name leading dash": func(c *Config) { c.Name = "-x" },
		"long name":         func(c *Config) { c.Name = strings.Repeat("n", 65) },
		"zero port":         func(c *Config) { c.Port = 0 },
		"big port":          func(c *Config) { c.Port = 65536 },
		"negative devport":  func(c *Config) { c.DevPort = -1 },
		"negative ttl":      func(c *Config) { c.SessionTTL = -time.Second },
		"allowhost scheme":  func(c *Config) { c.AllowHosts = []string{"http://x"} },
		"allowhost path":    func(c *Config) { c.AllowHosts = []string{"x/y"} },
		"allowhost empty":   func(c *Config) { c.AllowHosts = []string{""} },
		"open root":         func(c *Config) { c.OpenPrefixes = []string{"/"} },
		"open relative":     func(c *Config) { c.OpenPrefixes = []string{"api"} },
		"open dotdot":       func(c *Config) { c.OpenPrefixes = []string{"/api/.."} },
		"open framework":    func(c *Config) { c.OpenPrefixes = []string{"/__gofastr/sse"} },
		"bind hostname":     func(c *Config) { c.BindAddress = "example.com" },
		"bind with port":    func(c *Config) { c.BindAddress = "0.0.0.0:80" },
		"bind with zone":    func(c *Config) { c.BindAddress = "fe80::1%eth0" },
	}
	for name, mutate := range bad {
		c := ok
		mutate(&c)
		if err := validateConfig(c); err == nil {
			t.Errorf("%s: accepted %+v", name, c)
		}
	}
}

func TestNewDefaults(t *testing.T) {
	b := New(Config{ID: "dev.gofastr.hello", Port: 47811})
	if b.err != nil {
		t.Fatal(b.err)
	}
	if b.cfg.Name != "hello" {
		t.Errorf("Name default = %q; want the last ID segment", b.cfg.Name)
	}
	if b.cfg.SessionTTL != defaultSessionTTL {
		t.Errorf("SessionTTL default = %s", b.cfg.SessionTTL)
	}
}

func TestRunReturnsConfigError(t *testing.T) {
	var out bytes.Buffer
	b := New(Config{ID: "nodot", Port: 1, Stdout: &out, Stderr: &out})
	err := b.Run(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "Config.ID") {
		t.Fatalf("Run error = %v", err)
	}
}

func TestRunRefusesUnknownCommands(t *testing.T) {
	b, _, out := testBattery(t, "darwin", Config{})
	if err := b.Run(context.Background(), []string{"serve"}, nil); err == nil {
		t.Fatal("unknown top-level command accepted")
	}
	if err := b.Run(context.Background(), []string{"service"}, nil); err == nil {
		t.Fatal("missing verb accepted")
	}
	if err := b.Run(context.Background(), []string{"service", "explode"}, nil); err == nil {
		t.Fatal("unknown verb accepted")
	}
	if !strings.Contains(out.String(), "install [--port N]") {
		t.Fatalf("usage not printed: %q", out.String())
	}
}

func TestDaemonRefusesBadPort(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	if err := b.Run(context.Background(), []string{"service", "daemon", "--port", "0"}, nil); err == nil {
		t.Fatal("daemon --port 0 accepted")
	}
	if err := b.Run(context.Background(), []string{"service", "run", "--port", "70000"}, nil); err == nil {
		t.Fatal("run --port 70000 accepted")
	}
}

func TestOpenPrintNeedsHatch(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	err := b.Run(context.Background(), []string{"service", "open", "--print"}, nil)
	if err == nil || !strings.Contains(err.Error(), "InsecureTokenInURL") {
		t.Fatalf("open --print error = %v", err)
	}
}

func TestOpenNotRunning(t *testing.T) {
	b, ft, _ := testBattery(t, "darwin", Config{})
	err := b.Run(context.Background(), []string{"service", "open"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("open error = %v", err)
	}
	if len(ft.got()) != 0 {
		t.Fatalf("launched %q with nothing running", ft.got())
	}
}

func TestDataDirEnvMustBeAbsolute(t *testing.T) {
	t.Setenv(dataDirEnv, "relative/dir")
	if _, err := baseDir(); err == nil {
		t.Fatal("relative data dir accepted")
	}
}

func TestDataDirEnvRefusesControlBytes(t *testing.T) {
	for _, v := range []string{"/opt/a\nUser=root", "/opt/a\rb", "/opt/a\tb", "/opt/a\x7fb"} {
		t.Setenv(dataDirEnv, v)
		if _, err := baseDir(); err == nil {
			t.Errorf("data dir %q accepted", v)
		}
	}
}

func TestLayoutSeparatesDevAndInstalled(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	inst, err := b.layoutFor(false)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := b.layoutFor(true)
	if err != nil {
		t.Fatal(err)
	}
	if inst.dir == dev.dir || filepath.Base(dev.dir) != "dev.gofastr.svctest.dev" {
		t.Fatalf("installed %s, dev %s", inst.dir, dev.dir)
	}
}

func TestSecretMintedOnceOwnerOnly(t *testing.T) {
	dir := shortTempDir(t)
	path := filepath.Join(dir, "secret")
	a, err := loadOrMintSecret(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := loadOrMintSecret(path)
	if err != nil {
		t.Fatal(err)
	}
	if a != b || len(a) < 32 {
		t.Fatalf("secret not stable: %q then %q", a, b)
	}
	filepermtest.AssertOwnerOnly(t, path, 0o600)
	if err := rotateSecret(path); err != nil {
		t.Fatal(err)
	}
	c, err := loadOrMintSecret(path)
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Fatal("rotate kept the secret")
	}
}

func TestShortSecretRefused(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "secret")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrMintSecret(path); err == nil {
		t.Fatal("a 5-byte secret was accepted")
	}
}

func TestEnsurePrivateDirTightens(t *testing.T) {
	dir := filepath.Join(shortTempDir(t), "state")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	filepermtest.AssertOwnerOnly(t, dir, 0o700)
}

func TestTrimLogKeepsOneGeneration(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "service.log")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maxLogSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := trimLog(path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != 0 {
		t.Fatalf("log not truncated: %d bytes", fi.Size())
	}
	old, err := os.Stat(path + ".1")
	if err != nil || old.Size() != maxLogSize+1 {
		t.Fatalf("previous generation missing or short: %v", err)
	}
	if err := os.WriteFile(path, []byte("small"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := trimLog(path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "small" {
		t.Fatalf("a small log was trimmed: %q", b)
	}
}

func TestTailFileLastLinesScrubbed(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "service.log")
	var sb strings.Builder
	for i := range 10 {
		sb.WriteString("line ")
		sb.WriteByte(byte('0' + i))
		sb.WriteString("\n")
	}
	sb.WriteString("evil \x1b]0;pwned\x07 tail\n")
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tailFile(&out, path, 3, false); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if got != "line 8\nline 9\nevil ]0;pwned tail\n" {
		t.Fatalf("tail = %q", got)
	}
}

func TestTailFileMissing(t *testing.T) {
	var out bytes.Buffer
	if err := tailFile(&out, filepath.Join(shortTempDir(t), "none.log"), 10, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no log yet") {
		t.Fatalf("output %q", out.String())
	}
}
