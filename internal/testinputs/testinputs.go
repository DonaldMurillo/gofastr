// Package testinputs puts a subprocess's source files into the calling
// test's Go test cache key.
//
// Go keys a cached test result on the test binary and on the files and
// directories the test itself opened. A test that runs
// `go run <module>/cmd/gofastr` depends on the generator's source, but the
// subprocess's reads are not the test's, so after the generator changes Go
// replays the old result. cmd/affected already adds the edge for package
// selection (addCommandRuns); this adds it to the cache key.
package testinputs

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Command lists every package in the main module that importPath builds
// from, itself included, and reads each one's directory, its embedded
// files, and every directory between the two.
// Go's test cache records those reads: a directory's listing carries each
// entry's name, size and mtime, so an edited, added or removed source file
// changes the key. It returns the paths it read.
//
// Call it in the test that runs the command, before or after the run.
func Command(t testing.TB, importPath string) []string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "-json=Dir,Module,EmbedFiles", importPath)
	cmd.WaitDelay = 10 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v\n%s", importPath, err, stderr.String())
	}
	var read []string
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p struct {
			Dir        string
			Module     *struct{ Main bool }
			EmbedFiles []string
		}
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("go list -deps %s output: %v", importPath, err)
		}
		// Standard-library and module-cache packages change only with
		// the toolchain or go.sum, and both already change the test binary.
		if p.Module == nil || !p.Module.Main {
			continue
		}
		if _, err := os.ReadDir(p.Dir); err != nil {
			t.Fatalf("read %s: %v", p.Dir, err)
		}
		read = append(read, p.Dir)
		listed := map[string]bool{p.Dir: true}
		for _, name := range p.EmbedFiles {
			path := filepath.Join(p.Dir, filepath.FromSlash(name))
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("stat %s: %v", path, err)
			}
			read = append(read, path)
			// List each directory between the file and the package, so a
			// file added beside it under an embedded directory changes the
			// key too. The package directory's listing does not see it.
			for dir := filepath.Dir(path); !listed[dir]; dir = filepath.Dir(dir) {
				listed[dir] = true
				if _, err := os.ReadDir(dir); err != nil {
					t.Fatalf("read %s: %v", dir, err)
				}
				read = append(read, dir)
			}
		}
	}
	if len(read) == 0 {
		t.Fatalf("go list -deps %s found no package in the main module", importPath)
	}
	return read
}
