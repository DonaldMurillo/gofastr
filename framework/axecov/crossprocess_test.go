package axecov

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
)

// The manifest lives at the module root, so `go test ./...` runs every
// package's axe suite as a separate binary writing the same file at
// once. The in-process mutex cannot serialize those. A shared temp name
// once let one writer rename another's freshly truncated temp file into
// place: a zero-byte manifest that strict mode then refused to parse,
// taking down every strict app in the run. Read-merge-write across
// processes also dropped pages.
func TestRecordAcrossProcessesKeepsEveryPage(t *testing.T) {
	if dir := os.Getenv("AXECOV_CHILD_DIR"); dir != "" {
		id := os.Getenv("AXECOV_CHILD_ID")
		n, _ := strconv.Atoi(os.Getenv("AXECOV_CHILD_N"))
		for i := range n {
			if err := Record(dir, fmt.Sprintf("/p%s-%d", id, i), "light"); err != nil {
				t.Fatalf("child %s record %d: %v", id, i, err)
			}
		}
		return
	}
	const procs, per = 8, 25
	dir := t.TempDir()
	var wg sync.WaitGroup
	for p := range procs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestRecordAcrossProcessesKeepsEveryPage$", "-test.count=1")
			cmd.Env = append(os.Environ(),
				"AXECOV_CHILD_DIR="+dir,
				"AXECOV_CHILD_ID="+strconv.Itoa(p),
				"AXECOV_CHILD_N="+strconv.Itoa(per))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("child %d: %v\n%s", p, err, out)
			}
		}()
	}
	wg.Wait()
	m, err := Read(dir)
	if err != nil {
		t.Fatalf("manifest unreadable after concurrent writers: %v", err)
	}
	if got := len(m.Pages); got != procs*per {
		t.Fatalf("manifest holds %d pages, want %d: concurrent writers dropped pages", got, procs*per)
	}
}
