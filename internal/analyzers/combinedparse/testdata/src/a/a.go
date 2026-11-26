// Package a reduces the deploy runner before and after 94e342b64, with
// the postures the rule leaves alone.
package a

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// sshMerged is the pre-fix runner: one buffer on both streams, handed back.
func sshMerged(command string) (string, int, error) {
	var out bytes.Buffer
	cmd := exec.Command("ssh", command)
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), cmd.ProcessState.ExitCode(), err // want `merged stdout and stderr returned as a value`
}

// discover is the pre-fix caller shape, inline.
func discover() (string, string, error) {
	var out bytes.Buffer
	cmd := exec.Command("ssh", "uname -m && mktemp -d")
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", "", err
	}
	lines := strings.Fields(out.String()) // want `Fields reads the merged stdout and stderr`
	if len(lines) != 2 {
		return "", "", errors.New("bad answer")
	}
	return lines[0], lines[1], nil
}

// sshSplit is the fix: stdout is the value, stderr goes to the error.
func sshSplit(command string) (string, error) {
	var out, stderr bytes.Buffer
	cmd := exec.Command("ssh", command)
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ssh: %w: %s", err, stderr.String())
	}
	return out.String(), nil
}

func version() (string, error) {
	out, err := exec.Command("tool", "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tool --version: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil // want `merged stdout and stderr returned as a value`
}

type report struct{ Version string }

func store(r *report) {
	out, _ := exec.Command("tool", "--version").CombinedOutput()
	v := strings.TrimSpace(string(out))
	r.Version = v // want `merged stdout and stderr stored as a value`
}

func parses() {
	out, _ := exec.Command("tool", "count").CombinedOutput()
	n, _ := strconv.Atoi(strings.TrimSpace(string(out))) // want `Atoi reads the merged stdout and stderr`
	_ = n
	var v map[string]any
	_ = json.Unmarshal(out, &v)                  // want `Unmarshal reads the merged stdout and stderr`
	sc := bufio.NewScanner(bytes.NewReader(out)) // want `NewScanner reads the merged stdout and stderr`
	_ = sc
	if string(out) == "ok\n" { // want `compared as a value`
		return
	}
	switch strings.TrimSpace(string(out)) { // want `used as a switch value`
	case "ok":
	}
}

func messages() error {
	out, err := exec.Command("go", "build").CombinedOutput()
	if err != nil {
		log.Printf("build failed: %s", out)
		fmt.Fprintf(os.Stderr, "%s\n", out)
		return fmt.Errorf("go build: %w\n%s", err, out)
	}
	if strings.Contains(string(out), "warning:") {
		return errors.New("build warned")
	}
	if len(out) > 0 || string(out) == "" {
		return nil
	}
	return nil
}

func plainOutput() (string, error) {
	out, err := exec.Command("tool", "--version").Output()
	return strings.TrimSpace(string(out)), err
}

func closures() func() string {
	out, _ := exec.Command("tool").CombinedOutput()
	return func() string {
		_ = strings.Split(string(out), "\n") // want `Split reads the merged stdout and stderr`
		return ""
	}
}

// runTool is a package-level seam returning the merged text.
var runTool = func(name string) (string, error) {
	out, err := exec.Command(name).CombinedOutput()
	return string(out), err // want `merged stdout and stderr returned as a value`
}

type runner struct{ buf bytes.Buffer }

func (r *runner) fieldBuffer() []string {
	cmd := exec.Command("tool")
	cmd.Stdout, cmd.Stderr = &r.buf, &r.buf
	_ = cmd.Run()
	return strings.Fields(r.buf.String()) // want `Fields reads the merged stdout and stderr`
}

func sharedWriter() string {
	var b bytes.Buffer
	cmd := exec.Command("tool")
	cmd.Stdout = &b
	cmd.Stderr = cmd.Stdout
	_ = cmd.Run()
	return strings.TrimSpace(b.String()) // want `merged stdout and stderr`
}

type capture struct{ buf bytes.Buffer }

// twoValues keeps stdout and stderr in two values of one type.
func twoValues() []string {
	var out, errOut capture
	cmd := exec.Command("tool")
	cmd.Stdout, cmd.Stderr = &out.buf, &errOut.buf
	_ = cmd.Run()
	return strings.Fields(out.buf.String())
}
