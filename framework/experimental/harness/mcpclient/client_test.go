package mcpclient

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeStubServer creates a tiny MCP server fixture that answers initialize
// + tools/list + tools/call for one tool. The platform-specific implementation
// uses a shell script on Unix and a compiled helper on Windows.
func writeStubServer(t *testing.T) string {
	return writeMCPStub(t, false)
}

func TestSpawnAndListTools(t *testing.T) {
	server := writeStubServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Spawn(ctx, server, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestCallTool(t *testing.T) {
	server := writeStubServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Spawn(ctx, server, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	out, err := c.CallTool(ctx, "echo", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "hello from stub") {
		t.Errorf("result = %s", string(out))
	}
}

func TestSHA256Mismatch(t *testing.T) {
	server := writeStubServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Spawn(ctx, server, nil, "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected sha256 mismatch")
	}
	if _, ok := err.(*SHA256MismatchError); !ok {
		t.Errorf("err type = %T", err)
	}
}

func TestSourceWraps(t *testing.T) {
	server := writeStubServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Spawn(ctx, server, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	src := NewSource("stub", "eager", c)
	tools, err := src.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("got %d tools", len(tools))
	}
	if tools[0].Name() != "stub.echo" {
		t.Errorf("name = %q", tools[0].Name())
	}
}

// writeEnvDumpingStub writes an MCP server fixture that dumps its
// received environment to its first argument ($1, via `env`) before
// answering the initialize handshake, so Spawn returns only after the
// dump is on disk. The dump path is threaded through ARGV, not env, so
// the same stub works whether or not the caller scrubs/passes env. The
// script needs only PATH (to resolve env/sed/printf), which the default
// allowlist provides.
func writeEnvDumpingStub(t *testing.T) string {
	return writeMCPStub(t, true)
}

// readDump reads the env dump the child wrote, failing if it is missing.
func readDump(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read env dump %s: %v", p, err)
	}
	return string(data)
}

// TestSpawn_ScrubsInheritedEnv is the core security fix: the default Spawn
// must NOT hand the child the host's os.Environ(). A planted canary secret
// must be absent, while the minimal allowlist (PATH) the child genuinely
// needs to exec must survive. With the pre-fix nil-Env Spawn this fails
// (the canary leaks). The dump path rides on argv ($1) so it is available
// regardless of env scrubbing.
func TestSpawn_ScrubsInheritedEnv(t *testing.T) {
	t.Setenv("MCPCLIENT_CANARY_SECRET", "super-secret-value")

	dump := filepath.Join(t.TempDir(), "env.txt")
	stub := writeEnvDumpingStub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Spawn(ctx, stub, []string{dump}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got := readDump(t, dump)
	if strings.Contains(got, "MCPCLIENT_CANARY_SECRET") {
		t.Errorf("scrubbed child env leaked canary:\n%s", got)
	}
	if !strings.Contains(got, "PATH=") {
		t.Errorf("scrubbed child env missing allowlisted PATH:\n%s", got)
	}
}

// TestSpawnWithConfig_AllowlistAndExtras covers the explicit opt-in: caller
// KEY=VALUE pairs in Env appear verbatim, InheritEnv names copy host vars
// through, and a host var that is neither allowlisted nor named in
// InheritEnv is still scrubbed. The dump path rides on argv ($1).
func TestSpawnWithConfig_AllowlistAndExtras(t *testing.T) {
	t.Setenv("MCPCLIENT_CANARY_SECRET", "super-secret-value")
	t.Setenv("MCPCLIENT_INHERITED_HOST", "hostval")
	t.Setenv("MCPCLIENT_NOT_INHERITED", "should-vanish")

	dump := filepath.Join(t.TempDir(), "env.txt")
	stub := writeEnvDumpingStub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg := SpawnConfig{
		Env:        []string{"MCPCLIENT_TEST_EXTRA=present"},
		InheritEnv: []string{"MCPCLIENT_INHERITED_HOST"},
	}
	c, err := SpawnWithConfig(ctx, stub, []string{dump}, "", cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got := readDump(t, dump)
	for _, want := range []string{
		"MCPCLIENT_TEST_EXTRA=present",
		"MCPCLIENT_INHERITED_HOST=hostval",
		"PATH=",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("child env missing %q:\n%s", want, got)
		}
	}
	for _, banned := range []string{
		"MCPCLIENT_CANARY_SECRET",
		"MCPCLIENT_NOT_INHERITED",
	} {
		if strings.Contains(got, banned) {
			t.Errorf("child env leaked %q:\n%s", banned, got)
		}
	}
}
