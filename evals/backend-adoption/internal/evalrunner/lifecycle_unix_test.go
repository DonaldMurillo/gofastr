//go:build unix

package evalrunner

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStartServerReapsDescendantsOnExit(t *testing.T) {
	for _, name := range []string{"before ready", "after ready"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			outside := exec.CommandContext(ctx, "/bin/sleep", "30")
			outside.Dir = dir
			if err := outside.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = outside.Process.Kill(); _ = outside.Wait() })

			source := "#!/bin/sh\n(/bin/sleep 1; : > descendant-ran) &\nexit 3\n"
			if name == "after ready" {
				testBin, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				source = "#!/bin/sh\n" +
					"(until [ -f exit-now ]; do /bin/sleep 0.05; done; /bin/sleep 1; : > descendant-ran) &\n" +
					"GOFASTR_TEST_LIFECYCLE=1 exec " + quoteLifecycleArg(testBin) + " -test.run '^TestLifecycleHTTPHelper$'\n"
			}
			binary := writeLifecycleScript(t, dir, source)
			server, err := startServer(ctx, binary, dir, filepath.Join(dir, "db"), filepath.Join(dir, "server.log"))
			if server != nil {
				defer server.stop()
			}
			if name == "before ready" {
				if err == nil || !strings.Contains(err.Error(), "exited before ready") {
					t.Fatalf("wanted an early-exit error, got %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "exit-now"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
				select {
				case <-server.done:
				case <-ctx.Done():
					t.Fatal("candidate exit was never published")
				}
			}

			assertDescendantDidNotRun(t, dir)
			if err := outside.Process.Signal(syscall.Signal(0)); err != nil {
				t.Fatalf("an unrelated process with the same cwd was killed: %v", err)
			}
		})
	}
}

func TestLifecycleHTTPHelper(t *testing.T) {
	if os.Getenv("GOFASTR_TEST_LIFECYCLE") != "1" {
		return
	}
	server := &http.Server{
		Addr: os.Getenv("PORT"), ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	}
	go func() {
		for {
			if _, err := os.Stat("exit-now"); err == nil {
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	t.Fatal(server.ListenAndServe())
}

func TestBackendCommandsReapDescendantsOnSuccess(t *testing.T) {
	for _, name := range []string{"Codex", "commandOutput"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			binary := writeLifecycleScript(t, dir, "#!/bin/sh\n(/bin/sleep 1; echo leaked; : > descendant-ran) &\nexit 0\n")
			if name == "Codex" {
				err := runCodex(ctx, Config{Codex: binary, Timeout: 6 * time.Second}, dir,
					filepath.Join(dir, "phase", "codex.log"), filepath.Join(dir, "final"), "stdlib", false)
				if err != nil {
					t.Fatal(err)
				}
			} else if output, err := commandOutput(ctx, dir, binary); err != nil || output != "" {
				t.Fatalf("wanted success with no descendant output, got %q, %v", output, err)
			}
			assertDescendantDidNotRun(t, dir)
		})
	}
}

func writeLifecycleScript(t *testing.T, dir, source string) string {
	t.Helper()
	path := filepath.Join(dir, "candidate")
	if err := os.WriteFile(path, []byte(source), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertDescendantDidNotRun(t *testing.T, dir string) {
	t.Helper()
	marker := filepath.Join(dir, "descendant-ran")
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("descendant continued running after the leader exited")
		}
	}
}

func quoteLifecycleArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
