package main

import (
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

//go:embed skill.md
var kilnSkillContent string

// runAgent is the turnkey "build with OMP/GLM-5.2" subcommand. It starts a long-
// running `kiln serve` in the cwd, waits for the HTTP server to come
// online, exports KILN_URL so the agent's skill can reach it, ensures
// the Kiln skill is installed under ~/.claude/skills/kiln/, then execs
// OMP with whatever args followed. On OMP exit we send the serve
// subprocess SIGTERM and wait briefly for cleanup.
func runAgent(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[kiln] cwd: %v\n", err)
		return 1
	}

	skillPath, err := installSkill()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[kiln] install skill: %v\n", err)
		// non-fatal; OMP still runs, just without the framework skill
	}

	ompBin, err := exec.LookPath("omp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[kiln] omp not found in PATH. Install Oh My Pi first, then run `omp models` to verify glm-5.2 is available.\n")
		return 1
	}

	exe, err := os.Executable()
	if err != nil {
		exe = "kiln"
	}

	port := pickPort(8765)
	journal := filepath.Join(cwd, ".kiln.session.jsonl")

	// Loopback bind: the spawned serve is only ever reached via
	// localhost (kilnURL below), and its tool API is unauthenticated, so
	// there's no reason to expose it on all interfaces.
	serve := exec.Command(exe, "serve", "--addr", "127.0.0.1:"+strconv.Itoa(port), "--journal", journal)
	serve.Dir = cwd
	serve.Stdout = os.Stderr // panel banner goes to stderr so stdout stays clean
	serve.Stderr = os.Stderr
	if err := serve.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[kiln] start serve: %v\n", err)
		return 1
	}
	// serveDone closes when the serve child exits; readiness races it so
	// a serve that dies before binding (bad flag, missing journal perms)
	// fails the run immediately instead of waiting out the 10s poll, and
	// the deferred cleanup re-uses the one Wait.
	serveDone := make(chan error, 1)
	go func() { serveDone <- serve.Wait() }()
	defer func() {
		if serve.Process == nil {
			return
		}
		_ = serve.Process.Signal(syscall.SIGTERM)
		select {
		case <-serveDone:
		case <-time.After(3 * time.Second):
			_ = serve.Process.Kill()
			<-serveDone
		}
	}()

	kilnURL := fmt.Sprintf("http://localhost:%d", port)
	ready := make(chan error, 1)
	go func() { ready <- waitReady(kilnURL+"/kiln/world", 10*time.Second) }()
	select {
	case err := <-ready:
		if err != nil {
			fmt.Fprintf(os.Stderr, "[kiln] serve never came up: %v\n", err)
			return 1
		}
	case <-serveDone:
		fmt.Fprintf(os.Stderr, "[kiln] serve exited before becoming ready\n")
		return 1
	}

	fmt.Fprintf(os.Stderr, "[kiln] runtime:    %s\n", kilnURL)
	fmt.Fprintf(os.Stderr, "[kiln] panel:      %s/kiln/chat\n", kilnURL)
	fmt.Fprintf(os.Stderr, "[kiln] tool API:   %s/kiln/tool/{name}\n", kilnURL)
	fmt.Fprintf(os.Stderr, "[kiln] journal:    %s\n", journal)
	if skillPath != "" {
		fmt.Fprintf(os.Stderr, "[kiln] skill:      %s\n", skillPath)
	}
	fmt.Fprintf(os.Stderr, "[kiln] launching OMP with GLM-5.2…\n\n")

	ompArgs := []string{
		"--model", "glm-5.2",
		"--tools", "bash",
		"--append-system-prompt", kilnSystemPrompt(),
		"--auto-approve",
	}
	ompArgs = append(ompArgs, args...)
	cmd := exec.Command(ompBin, ompArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "KILN_URL="+kilnURL)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "[kiln] omp: %v\n", err)
		return 1
	}
	return 0
}

// pickPort returns desired if it's free, otherwise scans up by 1s.
// Falls back to 0 (kernel-assigned) if nothing in [desired, desired+10] works.
func pickPort(desired int) int {
	for p := desired; p < desired+10; p++ {
		if portFree(p) {
			return p
		}
	}
	return 0
}

// portFree reports whether TCP port p has no listener. The probe is a
// dial with a deadline, not an HTTP fetch: connection success/refused
// is the whole occupancy signal, so a wedged listener (accepts, never
// responds) reads as busy in bounded time instead of hanging pickPort
// — and every kiln boot behind it — forever.
func portFree(p int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", p), 500*time.Millisecond)
	if err != nil {
		return true // refused / unreachable: nothing is listening
	}
	conn.Close()
	return false
}

// waitReady polls url until the kiln runtime answers with its identity
// marker, or timeout elapses. Any HTTP response is NOT readiness:
// runAgent exports KILN_URL to a spawned agent running with
// --auto-approve, so "ready" must mean "the kiln we started answered",
// not "some server answered". kiln's Live.ServeHTTP stamps every
// response with X-Kiln-Server; a responder without it is an impostor
// that won the pickPort→bind race and keeps waitReady polling until
// the timeout refuses it.
func waitReady(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			kiln := resp.Header.Get("X-Kiln-Server") != ""
			resp.Body.Close()
			if kiln {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", url)
}

// installSkill writes the Kiln skill into ~/.claude/skills/kiln/SKILL.md
// so every adapter can inject the same framework knowledge on every run.
// Idempotent: overwrites only if the embedded version differs from disk.
func installSkill() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".claude", "skills", "kiln")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "SKILL.md")
	if existing, err := os.ReadFile(path); err == nil && string(existing) == kilnSkillContent {
		return path, nil
	}
	if err := os.WriteFile(path, []byte(kilnSkillContent), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
