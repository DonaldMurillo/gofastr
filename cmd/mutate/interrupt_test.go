//go:build !windows

package main

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const interruptGuard = "package guards\n\nfunc choose(value int) int {\n\tif value > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n"

// The fixture's test holds the run while a mutant is on disk, so the
// interrupt lands with one written.
const interruptGuardTest = "package guards\n\nimport (\n\t\"os\"\n\t\"testing\"\n\t\"time\"\n)\n\n" +
	"func TestChoose(t *testing.T) {\n\tfor i := 0; i < 600; i++ {\n\t\tif b, _ := os.ReadFile(\"guard.go\"); string(b) == " +
	"\"package guards\\n\\nfunc choose(value int) int {\\n\\tif value > 0 {\\n\\t\\treturn 1\\n\\t}\\n\\treturn 0\\n}\\n\" {\n\t\t\tbreak\n\t\t}\n\t\ttime.Sleep(100 * time.Millisecond)\n\t}\n" +
	"\tif choose(1) != 1 || choose(-1) != 0 {\n\t\tt.Fatal(\"wrong branch\")\n\t}\n}\n"

func TestInterruptMidRunRestoresTheSourceAndExits130(t *testing.T) {
	if os.Getenv("GOFASTR_MUTATE_TEST_INTERRUPT") != "" {
		flag.CommandLine = flag.NewFlagSet("mutate", flag.ExitOnError)
		os.Args = []string{"mutate", "."}
		os.Exit(run())
	}
	fixture := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":        "module example.com/guards\n\ngo 1.27\n",
		"guard.go":      interruptGuard,
		"guard_test.go": interruptGuardTest,
	} {
		if err := os.WriteFile(filepath.Join(fixture, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	guardPath := filepath.Join(fixture, "guard.go")
	t.Cleanup(func() { _ = os.WriteFile(guardPath, []byte(interruptGuard), 0o600) })
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-test.run=^TestInterruptMidRunRestoresTheSourceAndExits130$")
	command.Dir = fixture
	command.Env = append(os.Environ(), "GOWORK=off", "GOFASTR_MUTATE_TEST_INTERRUPT=1")
	var output strings.Builder
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if b, _ := os.ReadFile(guardPath); string(b) != interruptGuard {
			break
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			_ = command.Wait()
			t.Fatalf("no mutant was ever written\n%s", output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err = command.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 130 {
		t.Fatalf("interrupted run ended with %v, want exit 130\n%s", err, output.String())
	}
	if b, _ := os.ReadFile(guardPath); string(b) != interruptGuard {
		t.Errorf("the interrupt left the mutant on disk:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(fixture, ".mutate.lock")); err == nil {
		t.Error("the interrupt left the package lock behind")
	}
}

func TestInterruptStopsBeforeItRestores(t *testing.T) {
	dir := t.TempDir()
	original := []byte("package p\n")
	for round := 0; round < 1000; round++ {
		rs := newRestoreSet()
		path := filepath.Join(dir, "late.go")
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		writing := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				if rs.addAndWrite(path, original, []byte("package p // mutant\n")) != nil {
					return
				}
				if i == 0 {
					close(writing)
				}
			}
		}()
		<-writing
		rs.interrupt()
		wg.Wait()
		if b, _ := os.ReadFile(path); string(b) != string(original) {
			t.Fatalf("round %d: a mutant written while the interrupt ran stayed on disk", round)
		}
	}
}
