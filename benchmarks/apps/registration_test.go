package apps_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

//gofastr:test-reads crud/main.go
//gofastr:test-reads full/main.go
func TestBenchRejectsInvalidEntities(t *testing.T) {
	if testing.Short() {
		t.Skip("builds benchmark binaries")
	}
	for _, fixture := range []struct{ app, receiver, entity string }{
		{"crud", "app", "posts"},
		{"full", "fwApp", "authors"},
		{"full", "fwApp", "posts"},
		{"full", "fwApp", "comments"},
	} {
		t.Run(fixture.app+"/"+fixture.entity, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join(fixture.app, "main.go"))
			if err != nil {
				t.Fatal(err)
			}
			text := string(source)
			call := fixture.receiver + ".Registry.Register(" + fixture.entity + ")"
			position := strings.Index(text, call)
			if position < 0 {
				t.Fatalf("registration missing: %s", call)
			}
			line := strings.LastIndex(text[:position], "\n") + 1
			text = text[:line] + "\t" + fixture.entity + ".Config.Name = \"\"\n" + text[line:]
			directory := t.TempDir()
			mainFile := filepath.Join(directory, "main.go")
			if err := os.WriteFile(mainFile, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			binary := filepath.Join(directory, "benchmark")
			if runtime.GOOS == "windows" {
				binary += ".exe"
			}
			if output, err := exec.Command("go", "build", "-o", binary, mainFile).CombinedOutput(); err != nil {
				t.Fatalf("build fixture: %v\n%s", err, output)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary)
			command.Env = append(os.Environ(), "PORT=0", "GOFASTR_ISOLATION=off")
			output, err := command.CombinedOutput()
			if err == nil || ctx.Err() != nil || !strings.Contains(string(output), "name must not be empty") {
				t.Fatalf("benchmark did not fail at registration: %v\n%s", err, output)
			}
		})
	}
}
