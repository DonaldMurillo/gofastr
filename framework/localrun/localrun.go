// Package localrun is the machine-local registry of running gofastr apps:
// Register writes one owner-only record per run in [Dir], and List returns
// the live ones so local tools can find each run and its MCP server.
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
	// EnvRuns "off" stops this process registering and "on" forces it.
	EnvRuns = "GOFASTR_RUNS"
	// EnvOff is EnvRuns under its older name.
	EnvOff = EnvRuns

	recordVersion  = 1
	maxRecordBytes = 16 << 10
	maxStartBytes  = 20

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
	// ProcStart is the OS start value for PID, kept in a sidecar file.
	ProcStart uint64 `json:"-"`
	// URL is the base URL a local client dials.
	URL string `json:"url"`
	// MCPPath is the HTTP MCP path, empty when the run serves none.
	MCPPath string `json:"mcp_path,omitempty"`
	// MCPCommand is an argv that speaks stdio MCP to this run.
	MCPCommand []string `json:"mcp_command,omitempty"`
	// Service is the battery/service Config.ID of a service run.
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

const envUnderTest = "GOFASTR_RUNS_UNDER_TEST"

func init() {
	if testing.Testing() {
		_ = os.Setenv(envUnderTest, "1")
	}
}

// Disabled reports whether this process must not register: EnvRuns says
// off, or it is or descends from a test binary and EnvDir is unset.
func Disabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvRuns))) {
	case "off", "0", "false", "no":
		return true
	}
	return UnderTest() && os.Getenv(EnvDir) == ""
}

// UnderTest reports whether this process is or descends from a test binary.
func UnderTest() bool {
	return testing.Testing() || os.Getenv(envUnderTest) != ""
}

// Forced reports whether EnvRuns asks this process to register wherever
// it binds.
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
	if len(body) > maxRecordBytes {
		return nil, fmt.Errorf("localrun: record is %d bytes, over the %d List reads", len(body), maxRecordBytes)
	}
	if err := recordMetaErr(rec); err != nil {
		return nil, err
	}

	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	root, err := openDirAt(dir, true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := rec.ID + recordExt
	start := rec.ID + startExt
	if rec.ProcStart != 0 {
		if err := writeFile(root, start, strconv.FormatUint(rec.ProcStart, 10)); err != nil {
			return nil, err
		}
	}
	if err := writeFile(root, name, string(body)); err != nil {
		_ = root.Remove(start)
		return nil, err
	}
	// The registration owns this directory: a later change to the
	// environment override must not strand a live record.
	return func() {
		root, err := openDirAt(dir, false)
		if err != nil {
			return
		}
		defer root.Close()
		_ = root.Remove(name)
		_ = root.Remove(start)
	}, nil
}

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

// List returns the records of live runs, newest first, removing those of
// runs that are gone. A missing directory is an empty list.
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

var loadRecord = readRecord

func readRecord(root *os.Root, name, id string) (Record, bool) {
	body, ok := readOwned(root, name, maxRecordBytes)
	if !ok {
		return Record{}, false
	}
	var rec Record
	if err := handler.UnmarshalStrict(body, &rec); err != nil {
		return Record{}, false
	}
	if rec.Version != recordVersion || rec.ID != id || !strings.HasPrefix(id, fmt.Sprintf("%d-", rec.PID)) {
		return Record{}, false
	}
	if recordMetaErr(rec) != nil {
		return Record{}, false
	}
	return rec, true
}

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

// recordMetaErr reports why rec breaks the URL and MCP path rules
// every reader applies.
func recordMetaErr(rec Record) error {
	u, err := url.Parse(rec.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("localrun: a record needs an http or https URL with a host, got %q", rec.URL)
	}
	if rec.MCPPath != "" && !strings.HasPrefix(rec.MCPPath, "/") {
		return fmt.Errorf("localrun: MCP path %q must start with a slash", rec.MCPPath)
	}
	return nil
}

// readOwned reads at most max bytes from name when it is a regular
// file this process owns.
func readOwned(root *os.Root, name string, max int) ([]byte, bool) {
	f, err := root.OpenFile(name, openRecordFlags, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() || !ownFile(f, fi) {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil || len(body) > max {
		return nil, false
	}
	return body, true
}

func readStart(root *os.Root, name string) uint64 {
	body, ok := readOwned(root, name, maxStartBytes)
	if !ok {
		return 0
	}
	v, err := strconv.ParseUint(string(body), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

var openRoot = os.OpenRoot

func openDir(create bool) (*os.Root, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return openDirAt(dir, create)
}

func openDirAt(dir string, create bool) (*os.Root, error) {
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
