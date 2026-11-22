package cliplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// buildTestPlugin compiles a small real Go binary implementing the cliplugin
// wire contract so tests exercise real subprocess execution, argv preservation,
// env filtering, cwd, stdio, exit codes, and signals cross-platform.
func buildTestPlugin(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(dir, "testplugin")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build test plugin: %v\n%s", err, out)
	}
	return binPath
}

const standardPluginSrc = `package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--gofastr-plugin-manifest" {
		fmt.Print(` + "`" + `{"protocol_version":1,"name":"echo","version":"v0.1.0","summary":"Echo test plugin","min_gofastr_version":"v0.90.0","env_passthrough":["ANDROID_*","JAVA_HOME"],"env_from_flags":["--keystore-pass-env"],"hooks":[{"phase":"pre","command":"build"}],"overrides":["deploy"]}` + "`" + `)
		return
	}
	if len(args) == 0 {
		fmt.Println("usage: echo <cmd>")
		return
	}
	switch args[0] {
	case "inspect":
		wd, _ := os.Getwd()
		env := map[string]string{}
		for _, kv := range os.Environ() {
			k, v, ok := strings.Cut(kv, "=")
			if ok {
				env[k] = v
			}
		}
		stdinBytes, _ := io.ReadAll(os.Stdin)
		payload := map[string]any{
			"args":  args,
			"wd":    wd,
			"env":   env,
			"stdin": string(stdinBytes),
		}
		_ = json.NewEncoder(os.Stdout).Encode(payload)
	case "exit":
		code := 1
		if len(args) > 1 {
			if n, err := strconv.Atoi(args[1]); err == nil {
				code = n
			}
		}
		fmt.Fprintf(os.Stderr, "exiting with %d\n", code)
		os.Exit(code)
	case "wait-interrupt":
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		fmt.Println("READY")
		select {
		case <-sigCh:
			fmt.Println("INTERRUPTED")
			os.Exit(130)
		case <-time.After(10 * time.Second):
			os.Exit(9)
		}
	default:
		fmt.Println("unknown:", args[0])
		os.Exit(2)
	}
}
`

func TestProbeAndRunEndToEnd(t *testing.T) {
	bin := buildTestPlugin(t, standardPluginSrc)

	m, err := Probe(context.Background(), []string{bin}, "")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if m.Name != "echo" || m.ProtocolVersion != 1 || m.Version != "v0.1.0" {
		t.Fatalf("unexpected manifest: %+v", m)
	}

	workDir := t.TempDir()
	realWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		realWorkDir = workDir
	}

	// Tricky arguments: whitespace, quotes, shell metacharacters, newlines.
	trickyArgs := []string{
		"inspect",
		"--name", "My App (v2); $(rm -rf /)",
		"--note", "line1\nline2 'quoted' \"double\"",
		"--keystore-pass-env", "RELEASE_KS_PASS",
	}

	var stdout, stderr bytes.Buffer
	code, err := Run(context.Background(), ExecRequest{
		PluginName:    "echo",
		Command:       []string{bin},
		Args:          trickyArgs,
		WorkDir:       workDir,
		CLIVersion:    "v0.98.0",
		CLIBin:        "/usr/local/bin/gofastr",
		ProjectRoot:   realWorkDir,
		Mode:          ModeNamespace,
		AllowEnv:      []string{"ANDROID_*", "JAVA_HOME"},
		AllowEnvFlags: []string{"--keystore-pass-env"},
		Environ: append(os.Environ(),
			"ANDROID_HOME=/opt/android",
			"RELEASE_KS_PASS=my-signing-pass",
			"GOFASTR_SECRET=must-not-leak",
			"DATABASE_URL=postgres://must-not-leak",
		),
		Stdin:  strings.NewReader("hello stdin"),
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil || code != 0 {
		t.Fatalf("Run: code=%d err=%v stderr=%s", code, err, stderr.String())
	}

	var got struct {
		Args  []string          `json:"args"`
		WD    string            `json:"wd"`
		Env   map[string]string `json:"env"`
		Stdin string            `json:"stdin"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode inspect output: %v\nraw: %s", err, stdout.String())
	}
	if len(got.Args) != len(trickyArgs) {
		t.Fatalf("args length = %d, want %d: %#v", len(got.Args), len(trickyArgs), got.Args)
	}
	for i := range trickyArgs {
		if got.Args[i] != trickyArgs[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got.Args[i], trickyArgs[i])
		}
	}
	gotWD, _ := filepath.EvalSymlinks(got.WD)
	if gotWD != realWorkDir {
		t.Errorf("wd = %q, want %q", gotWD, realWorkDir)
	}
	if got.Stdin != "hello stdin" {
		t.Errorf("stdin = %q, want %q", got.Stdin, "hello stdin")
	}
	if got.Env["ANDROID_HOME"] != "/opt/android" || got.Env["RELEASE_KS_PASS"] != "my-signing-pass" {
		t.Errorf("approved env missing: ANDROID_HOME=%q RELEASE_KS_PASS=%q", got.Env["ANDROID_HOME"], got.Env["RELEASE_KS_PASS"])
	}
	if got.Env[EnvProtocolVersion] != "1" || got.Env[EnvPluginName] != "echo" || got.Env[EnvCLIVersion] != "v0.98.0" {
		t.Errorf("protocol vars missing: %+v", got.Env)
	}
	if _, leaked := got.Env["GOFASTR_SECRET"]; leaked {
		t.Errorf("SECURITY: GOFASTR_SECRET leaked to plugin subprocess")
	}
	if _, leaked := got.Env["DATABASE_URL"]; leaked {
		t.Errorf("SECURITY: DATABASE_URL leaked to plugin subprocess")
	}
}

func TestRunExitCodePropagation(t *testing.T) {
	bin := buildTestPlugin(t, standardPluginSrc)
	for _, tc := range []struct {
		arg  string
		want int
	}{{"0", 0}, {"1", 1}, {"2", 2}, {"42", 42}} {
		var stdout, stderr bytes.Buffer
		code, err := Run(context.Background(), ExecRequest{
			PluginName: "echo",
			Command:    []string{bin},
			Args:       []string{"exit", tc.arg},
			Stdout:     &stdout,
			Stderr:     &stderr,
		})
		if err != nil {
			t.Fatalf("Run(exit %s) err = %v", tc.arg, err)
		}
		if code != tc.want {
			t.Fatalf("Run(exit %s) code = %d, want %d", tc.arg, code, tc.want)
		}
		if !strings.Contains(stderr.String(), "exiting with "+tc.arg) {
			t.Fatalf("stderr not propagated: %q", stderr.String())
		}
	}
}

func TestRunForwardsInterruptAndCancelsCleanly(t *testing.T) {
	bin := buildTestPlugin(t, standardPluginSrc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()

	// Cancel once the child announces READY so we know its signal handler is installed.
	go func() {
		buf := make([]byte, 64)
		n, _ := pr.Read(buf)
		if strings.Contains(string(buf[:n]), "READY") {
			cancel()
		}
	}()

	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		code, err := Run(ctx, ExecRequest{
			PluginName: "echo",
			Command:    []string{bin},
			Args:       []string{"wait-interrupt"},
			Stdout:     pw,
			Stderr:     &stderr,
		})
		_ = pw.Close()
		if err != nil {
			t.Errorf("Run err: %v", err)
		}
		done <- code
	}()

	select {
	case code := <-done:
		if code != 130 {
			t.Fatalf("interrupt exit code = %d, want 130", code)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Run hung on context cancellation")
	}
}

func TestProbeRejectsProtocolMismatchAndScrubsStderr(t *testing.T) {
	mismatchBin := buildTestPlugin(t, `package main
import "fmt"
func main() {
	fmt.Print(`+"`"+`{"protocol_version":99,"name":"future","version":"v2.0.0","summary":"Future plugin"}`+"`"+`)
}
`)
	if _, err := Probe(context.Background(), []string{mismatchBin}, ""); !errors.Is(err, ErrProtocolMismatch) {
		t.Fatalf("expected ErrProtocolMismatch, got %v", err)
	}

	noisyBin := buildTestPlugin(t, `package main
import (
	"fmt"
	"os"
)
func main() {
	fmt.Fprint(os.Stderr, "\x1b]0;pwned\x07boom")
	os.Exit(1)
}
`)
	_, err := Probe(context.Background(), []string{noisyBin}, "")
	if err == nil {
		t.Fatal("expected probe error")
	}
	if strings.ContainsAny(err.Error(), "\x1b\x07") {
		t.Fatalf("SECURITY: probe stderr error retained terminal escape bytes: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected scrubbed stderr text, got: %v", err)
	}
}

func TestProbeWaitIsBoundedAndOversizedManifestRefused(t *testing.T) {
	if runtime.GOOS != "windows" {
		prev := ProbeWaitDelay
		ProbeWaitDelay = 300 * time.Millisecond
		t.Cleanup(func() { ProbeWaitDelay = prev })

		done := make(chan error, 1)
		go func() {
			_, err := Probe(context.Background(), []string{"/bin/sh", "-c", `sleep 30 & printf '{"protocol_version":1,"name":"forker","version":"v0.1.0","summary":"Forker"}'`}, "")
			done <- err
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("SECURITY: [hang] Probe never returned when grandchild held output pipe")
		}
	}

	hugeBin := buildTestPlugin(t, `package main
import (
	"fmt"
	"strings"
)
func main() {
	fmt.Printf("{\"protocol_version\":1,\"name\":\"huge\",\"version\":\"v0.1.0\",\"summary\":\"%s\"}", strings.Repeat("a", 300*1024))
}
`)
	if _, err := Probe(context.Background(), []string{hugeBin}, ""); !errors.Is(err, ErrInvalidManifest) || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("SECURITY: oversized manifest not rejected by size cap: %v", err)
	}
}

func TestRunCanceledContextDoesNotStart(t *testing.T) {
	bin := buildTestPlugin(t, standardPluginSrc)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stdout, stderr bytes.Buffer
	code, err := Run(ctx, ExecRequest{
		PluginName: "echo",
		Command:    []string{bin},
		Args:       []string{"inspect"},
		Stdout:     &stdout,
		Stderr:     &stderr,
	})
	if err != nil || code != 130 || stdout.Len() != 0 {
		t.Fatalf("canceled ctx started child: code=%d err=%v out=%q", code, err, stdout.String())
	}
}

func TestPrepareVerifiedExecPreventsTOCTOU(t *testing.T) {
	bin := buildTestPlugin(t, standardPluginSrc)
	sha, err := HashFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	rp := ResolvedPlugin{
		Name:    "echo",
		Source:  SourceProject,
		Status:  StatusReady,
		Command: []string{bin},
		SHA256:  sha,
	}
	execCmd, cleanup, err := PrepareVerifiedExec(rp)
	if err != nil {
		t.Fatalf("PrepareVerifiedExec: %v", err)
	}
	defer cleanup()

	// Overwrite the original checkout binary after verification: the snapshot
	// must still run the verified binary untouched.
	if err := os.WriteFile(bin, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	m, err := VerifyLiveCommand(context.Background(), execCmd, "v0.98.0", "")
	if err != nil || m.Name != "echo" {
		t.Fatalf("verified snapshot affected by post-verify write: m=%+v err=%v", m, err)
	}
}
