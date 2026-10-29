//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// WebKit subprocesses may briefly outlive the GTK test process while retaining
// stdout/stderr. A regular file avoids confusing inherited pipe EOF with the
// child's exit status. Every invocation owns a separate process group; after
// waiting for that child, remove its remaining group members. This is fixture
// cleanup, not a production shutdown guarantee or a general process-tree reaper
// (a descendant that creates a new session/group is outside this group).
func nativeTrayChildOutput(cmd *exec.Cmd) ([]byte, error) {
	if cmd.SysProcAttr != nil || cmd.Stdout != nil || cmd.Stderr != nil || cmd.Process != nil {
		return nil, errors.New("native tray child requires an unstarted command without process/output overrides")
	}
	output, err := os.CreateTemp("", "gofastr-tray-child-*.log")
	if err != nil {
		return nil, err
	}
	defer os.Remove(output.Name())
	defer output.Close()
	cmd.Stdout, cmd.Stderr = output, output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return nativeTrayChildKill(cmd) }
	cmd.WaitDelay = 2 * time.Second
	runErr := cmd.Run()
	cleanupErr := nativeTrayChildKill(cmd)
	if errors.Is(cleanupErr, os.ErrProcessDone) {
		cleanupErr = nil
	}
	const maxOutput = 8 << 20
	// ReadAt leaves the inherited open-file offset alone while SIGKILL is
	// delivered; a final descendant write must not overwrite captured output.
	data, readErr := io.ReadAll(io.NewSectionReader(output, 0, maxOutput+1))
	if len(data) > maxOutput {
		data = data[:maxOutput]
		readErr = errors.New("native tray child output exceeded 8 MiB")
	}
	return data, errors.Join(runErr, cleanupErr, readErr)
}

func nativeTrayChildKill(cmd *exec.Cmd) error {
	if cmd.Process == nil || cmd.Process.Pid <= 1 {
		return os.ErrProcessDone
	}
	// Setpgid above makes the started child the leader of this fixture's group.
	// Never signal zero (the parent's group), -1 (all permitted processes),
	// or enumerate unrelated processes.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	} else {
		return err
	}
}

const trayCaptureFixtureRole = "GOFASTR_TRAY_CAPTURE_FIXTURE"
const trayCaptureFixtureRoot = "GOFASTR_TRAY_CAPTURE_ROOT"

func trayCaptureCommand(ctx context.Context, role, root string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTrayCaptureFixture$", "-test.v")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, trayCaptureFixtureRole+"=") && !strings.HasPrefix(entry, trayCaptureFixtureRoot+"=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, trayCaptureFixtureRole+"="+role, trayCaptureFixtureRoot+"="+root)
	return cmd
}

func TestTrayChildOutput(t *testing.T) {
	for _, role := range []string{"success", "failure", "cancel"} {
		t.Run(role, func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if role == "cancel" {
				go func() {
					defer cancel()
					_ = waitTrayCaptureReady(ctx, root)
				}()
			}
			start := time.Now()
			output, err := nativeTrayChildOutput(trayCaptureCommand(ctx, role, root))
			if role == "success" && err != nil {
				t.Fatalf("inherited output descriptor must not delay successful child: %v\n%s", err, output)
			}
			if role != "success" && err == nil {
				t.Fatalf("%s child was incorrectly accepted", role)
			}
			if time.Since(start) > 10*time.Second {
				t.Fatal("child output/cleanup waited for the long-lived descendant")
			}
			if role != "cancel" && (!bytes.Contains(output, []byte("capture stdout")) || !bytes.Contains(output, []byte("capture stderr"))) {
				t.Fatalf("child output was lost: %s", output)
			}
			ready, readErr := os.ReadFile(filepath.Join(root, "ready"))
			if readErr != nil {
				t.Fatal("descendant did not start: ", readErr)
			}
			pid, parseErr := strconv.Atoi(string(ready))
			if parseErr != nil || pid <= 0 {
				t.Fatal("invalid owned descendant witness")
			}
			// A terminated orphan can remain a zombie until the environment's
			// init reaps it. Assert it is not running; do not claim to reap it.
			deadline := time.Now().Add(3 * time.Second)
			for {
				stat, statErr := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
				closed := bytes.LastIndexByte(stat, ')')
				if os.IsNotExist(statErr) || (closed >= 0 && strings.HasPrefix(string(stat[closed+1:]), " Z ")) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("owned descendant still running after fixture cleanup")
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

func waitTrayCaptureReady(ctx context.Context, root string) error {
	for {
		if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestTrayCaptureFixture(t *testing.T) {
	role, root := os.Getenv(trayCaptureFixtureRole), os.Getenv(trayCaptureFixtureRoot)
	if role == "" {
		t.Skip("owned subprocess output fixture")
	}
	if role == "descendant" {
		if err := os.WriteFile(filepath.Join(root, "ready.tmp"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(root, "ready.tmp"), filepath.Join(root, "ready")); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Minute)
		return
	}
	if role != "success" && role != "failure" && role != "cancel" {
		t.Fatal("unknown subprocess output fixture role")
	}
	if stat, err := os.Stdout.Stat(); err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm() != 0600 {
		t.Fatal("capture is not a private regular file")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Deliberately leave this descendant running with inherited descriptors
	// after the direct child exits. Only the outer fixture's group cleanup
	// should terminate it, reproducing WebKit's retained-output behavior.
	child := trayCaptureCommand(context.Background(), "descendant", root)
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if err := waitTrayCaptureReady(ctx, root); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "capture stdout")
	fmt.Fprintln(os.Stderr, "capture stderr")
	switch role {
	case "failure":
		t.Fatal("intentional child failure")
	case "cancel":
		time.Sleep(time.Minute)
	}
}
