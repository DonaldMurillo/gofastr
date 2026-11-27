package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/mutate/guardmut"
)

func TestRunRefusesBeforeAnyMutant(t *testing.T) {
	if args := os.Getenv("GOFASTR_MUTATE_TEST_ARGS"); args != "" {
		flag.CommandLine = flag.NewFlagSet("mutate", flag.ExitOnError)
		os.Args = append([]string{"mutate"}, strings.Fields(args)...)
		os.Exit(run())
	}
	const guard = "package guards\nfunc choose(value int) int {\nif value > 0 { return 1 }; return 0\n}\n"
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"a red suite", map[string]string{
			"guard.go":      guard,
			"guard_test.go": "package guards\nimport \"testing\"\nfunc TestChoose(t *testing.T) { t.Fatal(\"red\") }\n",
		}, "not green"},
		{"a suite with no tests", map[string]string{"guard.go": guard}, "executed no tests"},
		{"a package another run holds", map[string]string{
			"guard.go":      guard,
			"guard_test.go": "package guards\nimport \"testing\"\nfunc TestChoose(t *testing.T) {}\n",
			".mutate.lock":  "",
		}, "another mutate run holds"},
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixture := t.TempDir()
			c.files["go.mod"] = "module example.com/guards\n\ngo 1.27\n"
			for name, content := range c.files {
				if err := os.WriteFile(filepath.Join(fixture, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command(binary, "-test.run=^TestRunRefusesBeforeAnyMutant$")
			command.Dir = fixture
			command.Env = append(os.Environ(), "GOWORK=off", "GOFASTR_MUTATE_TEST_ARGS=.")
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), c.want) || strings.Contains(string(output), "caught") {
				t.Fatalf("got %v\n%s\nwant a refusal saying %q before any verdict", err, output, c.want)
			}
			_, lockErr := os.Stat(filepath.Join(fixture, ".mutate.lock"))
			if _, held := c.files[".mutate.lock"]; !held && lockErr == nil {
				t.Error("a refused run left its lock behind, blocking the next run over the package")
			}
		})
	}
}

func TestEveryGoTestRunIsBounded(t *testing.T) {
	args := testArgs("./sqlite/", "TestX", 90*time.Second)
	for _, want := range []string{"./sqlite/", "-count=1", "-timeout=1m30s", "TestX"} {
		if !slices.Contains(args, want) {
			t.Errorf("go %v lacks %q", args, want)
		}
	}
	cmd := goTest(context.Background(), "/module/root", args)
	if cmd.Dir != "/module/root" {
		t.Errorf("go test runs in %q, want the module root, where a ./pkg/ pattern resolves", cmd.Dir)
	}
	if cmd.WaitDelay <= 0 {
		t.Error("no WaitDelay: a grandchild holding the output pipe would wedge the run after its deadline")
	}
}

func TestBaselineAndMutantRunsGoThroughTheBoundedSeam(t *testing.T) {
	type call struct {
		args     []string
		deadline bool
		left     time.Duration
	}
	var calls []call
	saved := newGoTest
	newGoTest = func(ctx context.Context, moduleRoot string, args []string) *exec.Cmd {
		d, ok := ctx.Deadline()
		calls = append(calls, call{args: args, deadline: ok, left: time.Until(d)})
		return goTest(ctx, moduleRoot, []string{"version"})
	}
	defer func() { newGoTest = saved }()

	dir := t.TempDir()
	path := filepath.Join(dir, "guard.go")
	original := []byte("package guards\n\nfunc choose(v int) int {\n\tif v > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	guards, err := guardmut.Find("guard.go", original, guardmut.Options{})
	if err != nil || len(guards) == 0 {
		t.Fatalf("no guard in the fixture: %v", err)
	}
	_ = checkBaseline("./guards/", "TestX", dir, time.Minute)
	_, _ = runMutant(path, original, guards[0], "./guards/", "TestX", dir, time.Minute, newRestoreSet())
	if len(calls) != 2 {
		t.Fatalf("%d go test runs went through newGoTest, want 2 (the baseline and the mutant): a call site builds its own command", len(calls))
	}
	for i, c := range calls {
		if !c.deadline || !slices.Contains(c.args, "-timeout=1m0s") || !slices.Contains(c.args, "-count=1") {
			t.Errorf("run %d: args %v, deadline %v; want -count=1, -timeout and a context deadline", i, c.args, c.deadline)
		}
		if c.left <= baselineBuildGrace {
			t.Errorf("run %d: %s left on its deadline, want -timeout plus the %s build grace", i, c.left, baselineBuildGrace)
		}
	}
	if back, _ := os.ReadFile(path); string(back) != string(original) {
		t.Errorf("the mutant run left %q on disk", back)
	}
}

func TestInterruptRestoresAndRefusesFurtherMutants(t *testing.T) {
	rs := newRestoreSet()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	original := []byte("package p\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rs.addAndWrite(path, original, []byte("package p // mutant\n")); err != nil {
		t.Fatal(err)
	}
	rs.interrupt()
	if back, _ := os.ReadFile(path); string(back) != string(original) {
		t.Errorf("interrupt left %q on disk", back)
	}
	if err := rs.addAndWrite(path, original, []byte("package p // late\n")); err == nil {
		t.Error("a mutant was written after the interrupt")
	}
}

func TestPackageFilesRefusesAPackageWithOnlyTests(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":    "module onlytests\n\ngo 1.27\n",
		"a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	if _, files, err := packageFiles("."); err == nil {
		t.Fatalf("a package with no source file resolved to %q: the run would test nothing and exit green", files)
	}
}
