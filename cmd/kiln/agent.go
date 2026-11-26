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

func serveArgs(port int, journal string) []string {
	return []string{"serve", "--addr", "127.0.0.1:" + strconv.Itoa(port), "--journal", journal}
}

// runAgent starts kiln serve on a loopback port, waits until it answers,
// syncs the Kiln skill and runs OMP with args and KILN_URL set. It returns
// OMP's exit code.
func runAgent(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[kiln] cwd: %v\n", err)
		return 1
	}

	skillPath, err := installSkill()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[kiln] install skill: %v\n", err)
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
	if port == 0 {
		fmt.Fprintf(os.Stderr, "[kiln] no free loopback port for serve\n")
		return 1
	}
	journal := filepath.Join(cwd, ".kiln.session.jsonl")

	serve := exec.Command(exe, serveArgs(port, journal)...)
	serve.Dir = cwd
	serve.Stdout = os.Stderr
	serve.Stderr = os.Stderr
	if err := serve.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[kiln] start serve: %v\n", err)
		return 1
	}
	proc := watchServe(serve)
	defer proc.stop(3 * time.Second)

	kilnURL := fmt.Sprintf("http://localhost:%d", port)
	ready := make(chan error, 1)
	go func() { ready <- waitReady(kilnURL+"/kiln/world", 10*time.Second) }()
	select {
	case err := <-ready:
		if err != nil {
			fmt.Fprintf(os.Stderr, "[kiln] serve never came up: %v\n", err)
			return 1
		}
	case <-proc.exited:
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

type serveProc struct {
	cmd    *exec.Cmd
	exited chan struct{}
}

func watchServe(cmd *exec.Cmd) *serveProc {
	p := &serveProc{cmd: cmd, exited: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(p.exited)
	}()
	return p
}

func (p *serveProc) stop(grace time.Duration) {
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.exited:
	case <-time.After(grace):
		_ = p.cmd.Process.Kill()
		<-p.exited
	}
}

func pickPort(desired int) int {
	for p := desired; p < desired+10; p++ {
		if portFree(p) {
			return p
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	p := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return p
}

func portFree(p int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", p), 500*time.Millisecond)
	if err != nil {
		return true
	}
	conn.Close()
	return false
}

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
