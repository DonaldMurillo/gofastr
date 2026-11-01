package localrun

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/procalive"
)

func TestRegisterThenUnregister(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	unregister, err := Register(Record{Name: "a", URL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	recs, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].PID != os.Getpid() || recs[0].Version != recordVersion || recs[0].Started.IsZero() {
		t.Fatalf("records = %+v", recs)
	}
	if want := procalive.Identify(os.Getpid()).Start; recs[0].ProcStart != want {
		t.Fatalf("ProcStart = %d, want this process's start value %d", recs[0].ProcStart, want)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(dir, recs[0].ID+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("record mode = %v, want 0600", fi.Mode().Perm())
		}
	}
	unregister()
	if recs, _ := List(); len(recs) != 0 {
		t.Fatalf("record survived unregister: %+v", recs)
	}
}

// A run that died without unregistering leaves its file; List drops
// the record and deletes the file.
func TestListDropsDeadRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	dead := cmd.Process.Pid
	id := fmt.Sprintf("%d-0000beef", dead)
	writeRaw(t, dir, id, Record{Version: 1, ID: id, PID: dead, URL: "http://127.0.0.1:1"})

	if recs, _ := List(); len(recs) != 0 {
		t.Fatalf("dead run listed: %+v", recs)
	}
	if _, err := os.Stat(filepath.Join(dir, id+".json")); !os.IsNotExist(err) {
		t.Fatalf("dead record not removed: %v", err)
	}
}

// Once a run is gone its pid may go to a newer process. That process is
// alive but is not the run: a record whose start value disagrees with
// the pid's is dropped, so a tool never lists a dead run's URL as live.
func TestListDropsRecycledPID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	self := procalive.Identify(os.Getpid())
	if self.Start == 0 {
		t.Fatal("no start value for this process: the recycled-pid check is vacuous")
	}
	live := fmt.Sprintf("%d-0000aaaa", self.PID)
	writeRaw(t, dir, live, Record{Version: 1, ID: live, PID: self.PID, ProcStart: self.Start, URL: "http://127.0.0.1:1"})
	gone := fmt.Sprintf("%d-0000bbbb", self.PID)
	writeRaw(t, dir, gone, Record{Version: 1, ID: gone, PID: self.PID, ProcStart: self.Start - 1, URL: "http://127.0.0.1:2"})

	recs, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].ID != live {
		t.Fatalf("List = %+v; want only %s", recs, live)
	}
	if _, err := os.Stat(filepath.Join(dir, gone+".json")); !os.IsNotExist(err) {
		t.Fatalf("recycled-pid record not removed: %v", err)
	}
}

// Files that are not this package's records are skipped, not trusted:
// the directory is the user's, but a record names a URL a tool opens.
func TestListSkipsMalformedRecords(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	pid := os.Getpid()
	id := func(n int) string { return fmt.Sprintf("%d-0000000%d", pid, n) }
	good := Record{Version: 1, PID: pid, URL: "http://127.0.0.1:1"}

	r := good
	r.ID = id(1)
	r.URL = "file://127.0.0.1/etc/passwd" // has a host; only the scheme check refuses it
	writeRaw(t, dir, id(1), r)

	r = good
	r.ID = id(9) // disagrees with its file name
	writeRaw(t, dir, id(2), r)

	r = good
	r.ID = id(3)
	r.PID = os.Getppid() // alive, but disagrees with the id's pid
	writeRaw(t, dir, id(3), r)

	// A duplicate key: last-wins decoding would read a valid record.
	dup := fmt.Sprintf(`{"version":1,"id":"%s","pid":%d,"pid":%d,"url":"http://127.0.0.1:1"}`, id(4), os.Getppid(), pid)
	if err := os.WriteFile(filepath.Join(dir, id(4)+".json"), []byte(dup), 0o600); err != nil {
		t.Fatal(err)
	}
	big := `{"version":1,"id":"` + id(5) + `","pid":` + fmt.Sprint(pid) + `,"url":"http://127.0.0.1:1","name":"` + strings.Repeat("x", maxRecordBytes) + `"}`
	if err := os.WriteFile(filepath.Join(dir, id(5)+".json"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	r = good
	r.ID = id(7)
	r.MCPPath = "@evil.com/" // MCPURL would read http://127.0.0.1:1@evil.com/
	writeRaw(t, dir, id(7), r)

	r = good
	r.ID = id(8)
	r.Version = recordVersion + 1 // a layout this reader does not know
	writeRaw(t, dir, id(8), r)

	r = good
	r.ID = id(6)
	r.MCPPath = "/mcp"
	writeRaw(t, dir, id(6), r)

	recs, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].ID != id(6) {
		t.Fatalf("want only the well-formed record, got %+v", recs)
	}
}

func TestListNewestFirst(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	pid := os.Getpid()
	now := time.Now().UTC()
	for i, age := range []time.Duration{time.Hour, 0, 2 * time.Hour} {
		id := fmt.Sprintf("%d-0000000%d", pid, i)
		writeRaw(t, dir, id, Record{Version: 1, ID: id, PID: pid, URL: "http://127.0.0.1:1", Started: now.Add(-age)})
	}
	recs, _ := List()
	if len(recs) != 3 || recs[0].ID != fmt.Sprintf("%d-00000001", pid) || recs[2].ID != fmt.Sprintf("%d-00000002", pid) {
		t.Fatalf("order = %+v", recs)
	}
}

func TestListMissingDirIsEmpty(t *testing.T) {
	t.Setenv(EnvDir, filepath.Join(t.TempDir(), "absent"))
	recs, err := List()
	if err != nil || len(recs) != 0 {
		t.Fatalf("List = %v, %v", recs, err)
	}
}

// An app a test builds and starts is not a test binary, but must stay
// out of the real registry too: the e2e suites kill their apps before
// they can remove a record.
func TestTestChildrenDoNotRegister(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a child binary")
	}
	exe := filepath.Join(t.TempDir(), "child.exe")
	if out, err := exec.Command("go", "build", "-o", exe, "./testdata/child").CombinedOutput(); err != nil {
		t.Fatalf("build child: %v\n%s", err, out)
	}
	t.Setenv(EnvOff, "")
	run := func() string {
		out, err := exec.Command(exe).Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	t.Setenv(EnvDir, "")
	if got := run(); got != "true" {
		t.Fatalf("a test's child process with no %s: Disabled() = %s, want true", EnvDir, got)
	}
	t.Setenv(EnvDir, t.TempDir())
	if got := run(); got != "false" {
		t.Fatalf("a test's child process with %s set: Disabled() = %s, want false", EnvDir, got)
	}
}

func TestDisabled(t *testing.T) {
	t.Setenv(EnvDir, "")
	if !Disabled() {
		t.Fatal("a test binary with no EnvDir must not register into the real registry")
	}
	t.Setenv(EnvDir, t.TempDir())
	if Disabled() {
		t.Fatal("EnvDir set: registration should be on")
	}
	for _, v := range []string{"off", "0", "false", "No"} {
		t.Setenv(EnvOff, v)
		if !Disabled() {
			t.Fatalf("%s=%s did not opt out", EnvOff, v)
		}
	}
}

func writeRaw(t *testing.T, dir, id string, r Record) {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if r.ProcStart != 0 {
		if err := os.WriteFile(filepath.Join(dir, id+".start"), fmt.Appendf(nil, "%d", r.ProcStart), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// Two List calls raced: A read a recycled-pid record's JSON, B found it
// dead and removed the JSON and its sidecar, and A then read the missing
// sidecar as "no start value" and listed the dead run as live on the
// pid alone. List reads the sidecar before the record.
func TestListRaceKeepsRecycledDead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	self := procalive.Identify(os.Getpid())
	if self.Start == 0 {
		t.Fatal("no start value for this process: the recycled-pid check is vacuous")
	}
	gone := fmt.Sprintf("%d-0000cccc", self.PID)
	writeRaw(t, dir, gone, Record{Version: 1, ID: gone, PID: self.PID, ProcStart: self.Start - 1, URL: "http://127.0.0.1:2"})
	prev := loadRecord
	loadRecord = func(root *os.Root, name, id string) (Record, bool) {
		rec, ok := prev(root, name, id)
		// The other List removes both files between this one's reads.
		_ = os.Remove(filepath.Join(dir, id+".json"))
		_ = os.Remove(filepath.Join(dir, id+".start"))
		return rec, ok
	}
	t.Cleanup(func() { loadRecord = prev })
	recs, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("List = %+v; the recycled-pid run was listed live", recs)
	}
}
