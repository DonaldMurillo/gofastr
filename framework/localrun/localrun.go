// Package localrun is the machine-local registry of running gofastr
// apps. Every App writes one record when its listener binds and removes
// it when the server stops, so a tool on the same machine (the
// gofastr-runs example, an agent) can list what is running, where, and
// how to reach its MCP server, with no port scanning.
//
// Records live in [Dir], one owner-only JSON file per run, with the
// run's process start value in a sidecar file beside it. A run that
// dies without shutting down leaves its file behind; [List] drops a
// record whose process is gone, a pid now held by a newer process
// included. Registration is best-effort: a run that
// cannot write its record serves anyway.
package localrun

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/internal/procalive"
)

const (
	// EnvDir overrides the registry directory.
	EnvDir = "GOFASTR_RUNS_DIR"
	// EnvRuns set to "off" (or 0/false/no) stops this process from
	// registering; "on" (or 1/true/yes) makes it register wherever it
	// binds. Unset, the App decides (see App.Start). Listing is
	// unaffected.
	EnvRuns = "GOFASTR_RUNS"
	// EnvOff is EnvRuns under its older name, from when the variable
	// could only turn registration off.
	EnvOff = EnvRuns

	// recordVersion is the Record layout this package writes.
	recordVersion = 1
	// maxRecordBytes bounds one record read; a real record is well
	// under 2 KiB.
	maxRecordBytes = 16 << 10
	// maxStartBytes bounds a sidecar read: a decimal uint64.
	maxStartBytes = 20

	recordExt = ".json"
	startExt  = ".start"
)

// Record is one running app.
type Record struct {
	Version int `json:"version"`
	// ID names the record file: "<pid>-<8 hex>".
	ID   string `json:"id"`
	Name string `json:"name"`
	PID  int    `json:"pid"`
	// ProcStart is the OS's start value for PID (procalive.Identity),
	// so List can tell the run from a later process that got its pid.
	// Zero when the OS would not say; the pid alone decides then. It is
	// kept in "<id>.start" beside the record, not in the JSON: readers
	// built before it decode records strictly, and an unknown field
	// would make them drop every record.
	ProcStart uint64 `json:"-"`
	// URL is the base URL a local client dials: http://127.0.0.1:<port>
	// for a wildcard bind.
	URL string `json:"url"`
	// MCPPath is the path the app serves MCP over HTTP on ("/mcp"),
	// empty when it serves none. Under `gofastr dev` it answers on
	// loopback with no credential; elsewhere the app's own auth
	// applies.
	MCPPath string `json:"mcp_path,omitempty"`
	// MCPCommand, when set, is an argv that speaks stdio MCP to this
	// run (battery/service sets `<exe> service mcp --state-dir <dir>`).
	// An agent should prefer it over MCPPath: it carries its own
	// credential.
	MCPCommand []string `json:"mcp_command,omitempty"`
	// Service is the battery/service Config.ID when the run is a
	// service daemon or its dev run.
	Service string    `json:"service,omitempty"`
	Dev     bool      `json:"dev,omitempty"`
	Dir     string    `json:"dir,omitempty"`
	Exe     string    `json:"exe,omitempty"`
	Started time.Time `json:"started"`
}

// MCPURL is URL+MCPPath, or "" when the run serves no HTTP MCP.
func (r Record) MCPURL() string {
	if r.MCPPath == "" {
		return ""
	}
	return strings.TrimSuffix(r.URL, "/") + r.MCPPath
}

var idPattern = regexp.MustCompile(`^[1-9][0-9]{0,9}-[0-9a-f]{8}$`)

// Dir is the registry directory: $GOFASTR_RUNS_DIR, else
// <os.UserConfigDir>/gofastr/runs.
func Dir() (string, error) {
	if d := os.Getenv(EnvDir); d != "" {
		return filepath.Abs(d)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("localrun: %w", err)
	}
	return filepath.Join(base, "gofastr", "runs"), nil
}

// envUnderTest marks a process started, directly or further down, by a
// test binary. A test binary that links this package sets it at init,
// so the apps its tests build and run (the dev-loop and quickstart
// e2e suites) inherit it and stay out of the real registry, even when
// the test kills them before they can remove their record.
const envUnderTest = "GOFASTR_RUNS_UNDER_TEST"

func init() {
	if testing.Testing() {
		_ = os.Setenv(envUnderTest, "1")
	}
}

// Disabled reports whether this process must not register: EnvOff says
// so, or it is a test binary, or a process a test binary started, with
// no EnvDir (tests that start an App must not litter the real registry;
// a test that exercises it sets EnvDir to a temp dir).
func Disabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvRuns))) {
	case "off", "0", "false", "no":
		return true
	}
	return UnderTest() && os.Getenv(EnvDir) == ""
}

// UnderTest reports whether this process is a Go test binary or a
// process one started (it inherits the marker a test binary sets at
// init): the dev-loop and quickstart e2e suites run real apps that way.
func UnderTest() bool {
	return testing.Testing() || os.Getenv(envUnderTest) != ""
}

// Forced reports whether the environment asks this process to
// register regardless of where it binds: EnvRuns says "on" (or
// 1/true/yes). EnvDir only says where the registry is. Disabled still
// wins.
func Forced() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvRuns))) {
	case "on", "1", "true", "yes":
		return true
	}
	return false
}

// Register writes rec and returns the func that removes it. It fills
// ID, PID (this process), ProcStart, Version and a zero Started. URL is
// required.
func Register(rec Record) (unregister func(), err error) {
	if rec.URL == "" {
		return nil, errors.New("localrun: a record needs a URL")
	}
	if rec.PID == 0 {
		rec.PID = os.Getpid()
	}
	if rec.ProcStart == 0 {
		rec.ProcStart = procalive.Identify(rec.PID).Start
	}
	if rec.Started.IsZero() {
		rec.Started = time.Now().UTC()
	}
	var nonce [4]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("localrun: %w", err)
	}
	rec.Version = recordVersion
	rec.ID = fmt.Sprintf("%d-%s", rec.PID, hex.EncodeToString(nonce[:]))
	if !idPattern.MatchString(rec.ID) {
		return nil, fmt.Errorf("localrun: pid %d out of range", rec.PID)
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("localrun: %w", err)
	}

	root, err := openDir(true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := rec.ID + recordExt
	start := rec.ID + startExt
	// The sidecar first: a reader that sees the record finds its start
	// value already there.
	if rec.ProcStart != 0 {
		if err := writeFile(root, start, strconv.FormatUint(rec.ProcStart, 10)); err != nil {
			return nil, err
		}
	}
	if err := writeFile(root, name, string(body)); err != nil {
		_ = root.Remove(start)
		return nil, err
	}
	return func() {
		_ = remove(name)
		_ = remove(start)
	}, nil
}

// writeFile writes body to name through an owner-only temp file and a
// rename, so a reader never sees half of it.
func writeFile(root *os.Root, name, body string) error {
	tmp := name + ".tmp"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("localrun: %w", err)
	}
	_, werr := f.WriteString(body)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = root.Rename(tmp, name)
	}
	if werr != nil {
		_ = root.Remove(tmp)
		return fmt.Errorf("localrun: %w", werr)
	}
	return nil
}

// List returns the records of live runs, newest first. A record whose
// process has exited is removed on the way, and so is one whose pid a
// newer process now holds (its start value differs from ProcStart). Files that are not records are
// skipped. A missing directory is an empty list.
func List() ([]Record, error) {
	root, err := openDir(false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	d, err := root.Open(".")
	if err != nil {
		return nil, fmt.Errorf("localrun: %w", err)
	}
	names, err := d.Readdirnames(-1)
	d.Close()
	if err != nil {
		return nil, fmt.Errorf("localrun: %w", err)
	}
	slices.Sort(names)
	var out []Record
	for _, name := range names {
		if id, ok := strings.CutSuffix(name, startExt); ok && idPattern.MatchString(id) {
			dropOrphanStart(root, names, id)
			continue
		}
		id, ok := strings.CutSuffix(name, recordExt)
		if !ok || !idPattern.MatchString(id) {
			continue
		}
		// The sidecar first. A concurrent List that finds this run dead
		// removes the record and then its sidecar: read in the other
		// order, the record could arrive with its sidecar already gone,
		// a missing start value would mean "older tracker, check the pid
		// alone", and a recycled pid would pass. Read first, a sidecar
		// gone by now means the record is gone too, and the read below
		// skips it.
		start := readStart(root, id+startExt)
		rec, ok := loadRecord(root, name, id)
		if !ok {
			continue
		}
		rec.ProcStart = start
		if !(procalive.Identity{PID: rec.PID, Start: rec.ProcStart}).Alive() {
			_ = root.Remove(name)
			_ = root.Remove(id + startExt)
			continue
		}
		out = append(out, rec)
	}
	slices.SortStableFunc(out, func(a, b Record) int {
		return b.Started.Compare(a.Started)
	})
	return out, nil
}

// loadRecord is readRecord, a variable so a test can run another List's
// removals between this one's reads.
var loadRecord = readRecord

// readRecord decodes one record file, refusing an oversized file, a
// record whose ID does not match its file name or whose pid disagrees
// with it, and a URL that is not plain http(s).
func readRecord(root *os.Root, name, id string) (Record, bool) {
	// Only a regular file, checked on the opened handle so nothing can
	// be swapped in after the check. The open does not block on a FIFO
	// with no writer (openRecordFlags), and the check keeps the read
	// from waiting on one that has a writer.
	f, err := root.OpenFile(name, openRecordFlags, 0)
	if err != nil {
		return Record{}, false
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() || !ownFile(f, fi) {
		return Record{}, false
	}
	body, err := io.ReadAll(io.LimitReader(f, maxRecordBytes+1))
	if err != nil || len(body) > maxRecordBytes {
		return Record{}, false
	}
	var rec Record
	if err := handler.UnmarshalStrict(body, &rec); err != nil {
		return Record{}, false
	}
	if rec.Version != recordVersion || rec.ID != id || !strings.HasPrefix(id, fmt.Sprintf("%d-", rec.PID)) {
		return Record{}, false
	}
	u, err := url.Parse(rec.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Record{}, false
	}
	// MCPURL concatenates, so a path like "@evil.com/" would move the
	// host. A leading slash ends the authority and keeps it on the run.
	if rec.MCPPath != "" && !strings.HasPrefix(rec.MCPPath, "/") {
		return Record{}, false
	}
	return rec, true
}

// dropOrphanStart removes the sidecar of a record that is gone (a
// reader that predates sidecars deleted it) once its process has
// exited. A live process's sidecar stays: Register writes it just
// before the record.
func dropOrphanStart(root *os.Root, names []string, id string) {
	if _, found := slices.BinarySearch(names, id+recordExt); found {
		return
	}
	pid, err := strconv.Atoi(id[:strings.IndexByte(id, '-')])
	if err != nil || procalive.Alive(pid) {
		return
	}
	_ = root.Remove(id + startExt)
}

// readStart reads a record's sidecar start value, 0 when there is none
// or it is not one: the pid alone decides then. The same checks as a
// record: a regular file of this user's, read without blocking.
func readStart(root *os.Root, name string) uint64 {
	f, err := root.OpenFile(name, openRecordFlags, 0)
	if err != nil {
		return 0
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() || !ownFile(f, fi) {
		return 0
	}
	body, err := io.ReadAll(io.LimitReader(f, maxStartBytes+1))
	if err != nil || len(body) > maxStartBytes {
		return 0
	}
	v, err := strconv.ParseUint(string(body), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// openRoot is os.OpenRoot, a variable so a test can swap the directory
// between the check and the open.
var openRoot = os.OpenRoot

func remove(name string) error {
	root, err := openDir(false)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Remove(name)
}

// openDir opens the registry directory as an os.Root, creating it
// first when create is set. Either way it must be this user's own
// directory, and it is left owner-only (checkDir, secureOpened).
func openDir(create bool) (*os.Root, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("localrun: %w", err)
		}
	}
	checked, err := checkDir(dir)
	if err != nil {
		return nil, err
	}
	root, err := openRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("localrun: %w", err)
	}
	if err := secureOpened(root, checked, dir); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}
