//go:build darwin || linux

package migrate

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestMigrationWritePublishesWholeFile(t *testing.T) {
	const childEnv = "GOFASTR_MIGRATION_WRITE_FAULT"
	if os.Getenv(childEnv) != "1" {
		command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMigrationWritePublishesWholeFile$", "-test.v")
		command.Env = append(os.Environ(), childEnv+"=1")
		command.WaitDelay = time.Second
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("write-fault subprocess: %v\n%s", err, output)
		}
		return
	}
	signal.Ignore(syscall.SIGXFSZ)
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Fatal(err)
	}
	for _, maximum := range []uint64{32, 192} {
		t.Run(fmt.Sprint(maximum), func(t *testing.T) {
			directory := t.TempDir()
			options := MigrationFileOptions{MigrationsDir: directory, Dialect: DialectSQLite}
			plan := Plan{Registry: blogReg(nil)}
			previous, err := LoadSnapshot(filepath.Join(directory, "schema.snapshot.json"))
			if err != nil {
				t.Fatal(err)
			}
			up, down, _, err := GeneratePlan(plan, previous, options.Dialect)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := RenderMigrationFileChecked(1, "initial", up, down)
			if err != nil || uint64(len(expected)) <= maximum {
				t.Fatalf("fault requires migration larger than %d: size=%d, error=%v", maximum, len(expected), err)
			}
			limited := original
			limited.Cur = maximum
			if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
				t.Fatal(err)
			}
			path, writeErr := GenerateMigrationFile(plan, "initial", options)
			if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
				t.Fatal(err)
			}
			if writeErr == nil || path != "" {
				t.Fatalf("limited write = %q, %v; want failure", path, writeErr)
			}
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("failed write published entries: %v", entries)
			}
			path, err = GenerateMigrationFile(plan, "initial", options)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(path) != "0001_initial.sql" {
				t.Fatalf("retry minted %q instead of the first version", path)
			}
			content, err := os.ReadFile(path)
			if err != nil || string(content) != expected {
				t.Fatalf("published migration is incomplete: size=%d, want=%d, error=%v", len(content), len(expected), err)
			}
			path, err = GenerateMigrationFile(plan, "initial", options)
			if err != nil || path != "" {
				t.Fatalf("repeat generation = %q, %v; want no duplicate", path, err)
			}
		})
	}
}
