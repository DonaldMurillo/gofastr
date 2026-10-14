// Package localrun is the machine-local registry of running gofastr
// apps. Every App writes one record when its listener binds and removes
// it when the server stops, so a tool on the same machine (the
// gofastr-runs example, an agent) can list what is running, where, and
// how to reach its MCP server, with no port scanning.
//
// Records live in [Dir], one owner-only JSON file per run. A run that
// dies without shutting down leaves its file behind; [List] drops a
// record whose pid is gone. Registration is best-effort: a run that
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
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/internal/procalive"
)

const (
	// EnvDir overrides the registry directory.
	EnvDir = "GOFASTR_RUNS_DIR"
	// EnvOff set to "off" (or 0/false/no) stops this process from
	// registering. Listing is unaffected.
	EnvOff = "GOFASTR_RUNS"

	// recordVersion is the Record layout this package writes.
	recordVersion = 1
	// maxRecordBytes bounds one record read; a real record is well
	// under 2 KiB.
	maxRecordBytes = 16 << 10
)

// Record is one running app.
type Record struct {
	Version int `json:"version"`
	// ID names the record file: "<pid>-<8 hex>".
	ID   string `json:"id"`
	Name string `json:"name"`
	PID  int    `json:"pid"`
	// URL is the base URL a local client dials: http://127.0.0.1:<port>
	// for a wildcard bind.
	URL string `json:"url"`
	// MCPPath is the path the app serves MCP over HTTP on ("/mcp"),
	// empty when it serves none. Under `gofastr dev` it answers on
	// loopback with no credential; elsewhere the app's own auth
	// applies.
	MCPPath string `json:"mcp_path,omitempty"`
	// MCPCommand, when set, is an argv that speaks stdio MCP to this
	// run (battery/service sets `<exe> service mcp`). An agent should
	// prefer it over MCPPath: it carries its own credential.
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
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvOff))) {
	case "off", "0", "false", "no":
		return true
	}
	underTest := testing.Testing() || os.Getenv(envUnderTest) != ""
	return underTest && os.Getenv(EnvDir) == ""
}

// Register writes rec and returns the func that removes it. It fills
// ID, PID (this process), Version and a zero Started. URL is required.
func Register(rec Record) (unregister func(), err error) {
	if rec.URL == "" {
		return nil, errors.New("localrun: a record needs a URL")
	}
	if rec.PID == 0 {
		rec.PID = os.Getpid()
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
	name := rec.ID + ".json"
	tmp := rec.ID + ".tmp"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("localrun: %w", err)
	}
	_, werr := f.Write(body)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = root.Rename(tmp, name)
	}
	if werr != nil {
		_ = root.Remove(tmp)
		return nil, fmt.Errorf("localrun: %w", werr)
	}
	return func() { _ = remove(name) }, nil
}

// List returns the records of live runs, newest first. A record whose
// pid has exited is removed on the way. Files that are not records are
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
		id, ok := strings.CutSuffix(name, ".json")
		if !ok || !idPattern.MatchString(id) {
			continue
		}
		rec, ok := readRecord(root, name, id)
		if !ok {
			continue
		}
		if !procalive.Alive(rec.PID) {
			_ = root.Remove(name)
			continue
		}
		out = append(out, rec)
	}
	slices.SortStableFunc(out, func(a, b Record) int {
		return b.Started.Compare(a.Started)
	})
	return out, nil
}

// readRecord decodes one record file, refusing an oversized file, a
// record whose ID does not match its file name or whose pid disagrees
// with it, and a URL that is not plain http(s).
func readRecord(root *os.Root, name, id string) (Record, bool) {
	// Only a regular file: opening a FIFO with no writer blocks, and
	// would hang every reader of the registry.
	if fi, err := root.Lstat(name); err != nil || !fi.Mode().IsRegular() {
		return Record{}, false
	}
	f, err := root.Open(name)
	if err != nil {
		return Record{}, false
	}
	defer f.Close()
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
// directory, and it is left owner-only (secureDir).
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
	if err := secureDir(dir); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("localrun: %w", err)
	}
	return root, nil
}
