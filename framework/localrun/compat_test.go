package localrun

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/internal/procalive"
)

// v1Record is Record as released, before the start value: what a
// tracker built against an older framework decodes a record into.
type v1Record struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	PID        int       `json:"pid"`
	URL        string    `json:"url"`
	MCPPath    string    `json:"mcp_path,omitempty"`
	MCPCommand []string  `json:"mcp_command,omitempty"`
	Service    string    `json:"service,omitempty"`
	Dev        bool      `json:"dev,omitempty"`
	Dir        string    `json:"dir,omitempty"`
	Exe        string    `json:"exe,omitempty"`
	Started    time.Time `json:"started"`
}

// A released reader decodes records strictly and refuses an unknown
// field, so a proc_start field in the JSON made every new run vanish
// from older trackers (gofastr-runs, agents). The start value lives in
// a sidecar file those readers never open.
func TestOldReaderDecodesRecord(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	unregister, err := Register(Record{Name: "a", URL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer unregister()
	recs, err := List()
	if err != nil || len(recs) != 1 {
		t.Fatalf("List = %+v, %v", recs, err)
	}
	body, err := os.ReadFile(filepath.Join(dir, recs[0].ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var old v1Record
	if err := handler.UnmarshalStrict(body, &old); err != nil {
		t.Fatalf("a released reader refuses the record: %v\n%s", err, body)
	}
	if old.Version != 1 || old.ID != recs[0].ID {
		t.Fatalf("decoded %+v", old)
	}
	if want := procalive.Identify(os.Getpid()).Start; want != 0 && recs[0].ProcStart != want {
		t.Fatalf("ProcStart = %d, want %d from the sidecar", recs[0].ProcStart, want)
	}
}

// Unregister removes the sidecar with the record, and List removes a
// sidecar a released reader left behind when it deleted a dead run's
// record (it knows nothing of the sidecar).
func TestSidecarRemovedWithRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	unregister, err := Register(Record{Name: "a", URL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	unregister()
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Fatalf("unregister left %v", left)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(dir, fmt.Sprintf("%d-0000beef.start", cmd.Process.Pid))
	if err := os.WriteFile(orphan, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A live run's sidecar, written just before its record, stays.
	fresh := filepath.Join(dir, fmt.Sprintf("%d-0000cafe.start", os.Getpid()))
	if err := os.WriteFile(fresh, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := List(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("a dead run's orphan sidecar survived List: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("a live run's sidecar was removed: %v", err)
	}
}
