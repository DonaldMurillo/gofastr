//go:build linux && (amd64 || arm64)

package linux

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

type browserProfile struct {
	dir, appID, addr string
	lock             *os.File
	native           bool
}

// WebKit shuts down its network process asynchronously. GTK returning does not
// acknowledge its last write. A context that has used a profile keeps this file
// descriptor alive until process exit; flock also releases on a crash, without
// stale-PID heuristics or deleting another process's lock file.
var profileLeases struct {
	sync.Mutex
	files []*os.File
}

func (s *linuxShell) PrepareProfile(appID, dataDir string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.profile != nil {
		return "", errors.New("desktop/linux: profile already prepared or shell started")
	}
	p, err := openBrowserProfile(appID, dataDir)
	if err != nil {
		return "", err
	}
	s.profile = p
	return p.addr, nil
}
func (s *linuxShell) CommitProfile(addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.profile == nil {
		return errors.New("desktop/linux: profile was not prepared")
	}
	return s.profile.commitOrigin(addr)
}
func (s *linuxShell) ReleaseProfile() {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.profile
	if p == nil || p.lock == nil {
		return
	}
	if p.native {
		profileLeases.Lock()
		profileLeases.files = append(profileLeases.files, p.lock)
		profileLeases.Unlock()
	} else {
		_ = p.lock.Close()
	}
	p.lock = nil
}
func openBrowserProfile(appID, dataDir string) (*browserProfile, error) {
	// The shell is callable without a Battery. Never accept a title-derived ID
	// or arbitrary relative path through that lower-level entry point.
	if len(appID) > 253 || !strings.Contains(appID, ".") || strings.Trim(appID, ".-") == "" || strings.ContainsAny(appID, "/\\\x00") {
		return nil, errors.New("desktop/linux: persistent profile requires a stable reverse-DNS AppID")
	}
	for _, c := range appID {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-') {
			return nil, errors.New("desktop/linux: invalid profile AppID")
		}
	}
	if !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) == "/" || !utf8.ValidString(dataDir) || strings.IndexByte(dataDir, 0) >= 0 {
		return nil, errors.New("desktop/linux: profile DataDir must be an absolute UTF-8 directory path")
	}
	if err := privateProfileDirectory(filepath.Clean(dataDir)); err != nil {
		return nil, err
	}
	dir := filepath.Join(dataDir, "browser-profiles", appID)
	if err := privateProfileDirectory(dir); err != nil {
		return nil, err
	}
	lock, err := openPrivateProfileFile(filepath.Join(dir, "profile.lock"))
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("desktop/linux: browser profile is in use; exit the owning app process before reopening: %w", err)
	}
	p := &browserProfile{dir: dir, appID: appID, addr: "127.0.0.1:0", lock: lock}
	ok := false
	defer func() {
		if !ok {
			_ = lock.Close()
		}
	}()
	if err := inspectProfileTree(dir); err != nil {
		return nil, err
	}
	if err := p.readOrigin(); err != nil {
		return nil, err
	}
	if p.addr == "127.0.0.1:0" {
		if err := p.writeOrigin(0); err != nil {
			return nil, err
		}
	}
	for _, child := range []string{"data", "cache"} {
		if err := privateProfileDirectory(filepath.Join(dir, child)); err != nil {
			return nil, err
		}
	}
	cookies := filepath.Join(dir, "cookies.sqlite")
	f, err := openPrivateProfileFile(cookies)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if info.Size() > 0 {
		if err := checkCookieDatabase(cookies); err != nil {
			return nil, err
		}
	}
	// An actual private create/write/sync catches read-only mounts and quotas
	// now. It is not an acknowledgement of WebKit's later asynchronous writes.
	probe, err := os.CreateTemp(dir, ".write-check-")
	if err != nil {
		return nil, fmt.Errorf("desktop/linux: profile is not writable: %w", err)
	}
	name := probe.Name()
	_, writeErr := probe.Write([]byte("profile preflight\n"))
	if writeErr == nil {
		writeErr = probe.Sync()
	}
	closeErr = probe.Close()
	removeErr := os.Remove(name)
	if err := errors.Join(writeErr, closeErr, removeErr); err != nil {
		return nil, fmt.Errorf("desktop/linux: profile write preflight: %w", err)
	}
	ok = true
	return p, nil
}

// No symlink is accepted, even in an ancestor. Existing directories are never
// chmodded. The private leaf contains native files that may have broader modes;
// they must still be owned, regular, and not group/world-writable. This is a
// startup preflight, not protection from a hostile same-UID rename race.
func privateProfileDirectory(path string) error {
	parts := strings.Split(strings.TrimPrefix(path, string(os.PathSeparator)), string(os.PathSeparator))
	current := string(os.PathSeparator)
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err = os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("desktop/linux: unsafe profile directory %s", current)
		}
		if st.Uid != uint32(os.Geteuid()) && st.Uid != 0 {
			return fmt.Errorf("desktop/linux: profile ancestor has another owner: %s", current)
		}
		if info.Mode().Perm()&0o022 != 0 && !(info.Mode()&os.ModeSticky != 0 && st.Uid == 0) {
			return fmt.Errorf("desktop/linux: writable profile ancestor: %s", current)
		}
		if current == path && (st.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != 0o700) {
			return fmt.Errorf("desktop/linux: profile directory must be owned by this user with mode0700: %s", current)
		}
	}
	return nil
}
func openPrivateProfileFile(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("desktop/linux: open private profile file: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err == nil {
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
			err = errors.New("desktop/linux: profile file must be a private owned regular file without hard links")
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// A regular file may carry more than one hard link only when every link is
// inside the profile: WebKit's network cache stores a body once under
// Blobs/ and links it into each record. The walk counts the names it finds
// for each such inode and refuses one whose link count is higher, a name
// outside the profile the browser would write through. Cookie, origin and
// lock files keep a single name.
func inspectProfileTree(dir string) error {
	type inode struct{ dev, ino uint64 }
	type linked struct {
		rel         string
		nlink, seen uint64
	}
	var order []inode
	links := map[inode]*linked{}
	count := 0
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		count++
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if count > 100000 || strings.Count(rel, string(os.PathSeparator)) > 32 {
			return errors.New("desktop/linux: profile exceeds startup inspection bounds")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != uint32(os.Geteuid()) || !(info.IsDir() || info.Mode().IsRegular()) || info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("desktop/linux: unsafe file in browser profile: %s", rel)
		}
		//gofastr:allow(routing/prefix-without-segment-boundary) cookie filename family, not a route/tree: SQLite sidecars and any backup suffix must all remain owner-only
		if (strings.HasPrefix(rel, "cookies.sqlite") || rel == "origin.json" || rel == "profile.lock") && (!info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || st.Nlink != 1) {
			return fmt.Errorf("desktop/linux: browser profile metadata/cookie file is not private: %s", rel)
		}
		if info.Mode().IsRegular() && st.Nlink != 1 {
			key := inode{dev: uint64(st.Dev), ino: st.Ino}
			l := links[key]
			if l == nil {
				l = &linked{rel: rel, nlink: uint64(st.Nlink)}
				links[key] = l
				order = append(order, key)
			}
			l.seen++
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, key := range order {
		if l := links[key]; l.seen != l.nlink {
			return fmt.Errorf("desktop/linux: browser profile file is linked outside the browser profile: %s", l.rel)
		}
	}
	return nil
}

type profileOrigin struct {
	Version int    `json:"version"`
	AppID   string `json:"app_id"`
	Port    int    `json:"port"`
}

func (p *browserProfile) readOrigin() error {
	f, err := os.Open(filepath.Join(p.dir, "origin.json"))
	if os.IsNotExist(err) {
		// Native data without its identity/origin is not a new profile. Never
		// quietly create a new origin on top of a partially deleted profile.
		entries, e := os.ReadDir(p.dir)
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if entry.Name() != "profile.lock" {
				return errors.New("desktop/linux: profile origin is missing; restore it or reset the whole browser profile while the app is stopped")
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1025))
	if err != nil || len(data) > 1024 {
		return errors.New("desktop/linux: browser profile origin exceeds its size limit or cannot be read")
	}
	var decoded struct {
		Version int    `json:"version"`
		AppID   string `json:"app_id"`
		Port    *int   `json:"port"`
	}
	if err := handler.UnmarshalStrict(data, &decoded); err != nil {
		return errors.New("desktop/linux: corrupt browser profile origin")
	}
	if decoded.Port == nil {
		return errors.New("desktop/linux: browser profile origin requires an explicit port")
	}
	origin := profileOrigin{Version: decoded.Version, AppID: decoded.AppID, Port: *decoded.Port}
	if origin.Version != 1 || origin.AppID != p.appID || origin.Port != 0 && origin.Port < 1024 || origin.Port > 65535 {
		return errors.New("desktop/linux: invalid browser profile origin")
	}
	if origin.Port == 0 {
		// A pending profile can survive app initialization failure or an
		// interrupted first launch. It must not already contain native data.
		return filepath.WalkDir(p.dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if path == filepath.Join(p.dir, "profile.lock") || path == filepath.Join(p.dir, "origin.json") {
				return nil
			}
			if path == filepath.Join(p.dir, "cookies.sqlite") {
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if info.Size() == 0 {
					return nil
				}
			}
			return errors.New("desktop/linux: uncommitted browser profile contains native data; refusing a new origin")
		})
	}
	p.addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(origin.Port))
	return nil
}
func (p *browserProfile) commitOrigin(addr string) error {
	host, portText, err := net.SplitHostPort(addr)
	port, portErr := strconv.Atoi(portText)
	if err != nil || portErr != nil || host != "127.0.0.1" || port < 1024 || port > 65535 {
		return errors.New("desktop/linux: persistent profile requires a non-privileged IPv4 loopback origin")
	}
	if p.addr != "127.0.0.1:0" {
		if addr != p.addr {
			return errors.New("desktop/linux: listener changed the persistent browser origin; disable port remapping or stop the conflicting process")
		}
		return nil
	}
	if err := p.writeOrigin(port); err != nil {
		return err
	}
	p.addr = addr
	return nil
}
func (p *browserProfile) writeOrigin(port int) error {
	data, err := json.Marshal(profileOrigin{Version: 1, AppID: p.appID, Port: port})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(p.dir, ".origin-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, writeErr := f.Write(append(data, '\n'))
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(p.dir, "origin.json")); err != nil {
		return err
	}
	dir, err := os.Open(p.dir)
	if err != nil {
		return err
	}
	err = dir.Sync()
	closeErr = dir.Close()
	return errors.Join(err, closeErr)
}

// checkCookieDatabase opens the jar read-write (mode=rw never creates the
// file). A crash mid-write leaves a hot rollback journal, and a read-only
// connection answers SQLITE_READONLY_ROLLBACK instead of recovering it, which
// would refuse every later launch. The caller holds the profile lock, so no
// WebKit process is writing: rolling the journal back here is the same
// recovery WebKit's own first open would run.
func checkCookieDatabase(path string) error {
	u := &url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw"}
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return fmt.Errorf("desktop/linux: open persistent cookies: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&result); err != nil {
		return fmt.Errorf("desktop/linux: cannot validate persistent cookies; refusing to reset them: %w", err)
	}
	if result != "ok" {
		return errors.New("desktop/linux: corrupt persistent cookies; refusing to reset them")
	}
	return checkCookieSchema(ctx, db)
}

// Soup's SQLite cookie jar logs query errors rather than returning them through
// WebKit's void configuration setter. It SELECTs these columns and INSERTs by
// position. Reject a valid SQLite file that would silently become an empty or
// unwritable jar. Soup migrates its legacy nine-column table by adding sameSite.
func checkCookieSchema(ctx context.Context, db *sql.DB) error {
	var kind string
	if err := db.QueryRowContext(ctx, "SELECT type FROM sqlite_schema WHERE name = 'moz_cookies'").Scan(&kind); err != nil || kind != "table" {
		return errors.New("desktop/linux: persistent cookies have no compatible native table; use a compatible runtime or restore a backup while the app is stopped; refusing to reset them")
	}
	rows, err := db.QueryContext(ctx, "PRAGMA table_xinfo(moz_cookies)")
	if err != nil {
		return fmt.Errorf("desktop/linux: inspect persistent cookie schema: %w", err)
	}
	defer rows.Close()
	names := []string{"id", "name", "value", "host", "path", "expiry", "lastAccessed", "isSecure", "isHttpOnly", "sameSite"}
	count := 0
	for rows.Next() {
		var position, notNull, primary, hidden int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&position, &name, &kind, &notNull, &defaultValue, &primary, &hidden); err != nil {
			return err
		}
		wantType, wantPrimary := "INTEGER", 0
		if count >= 1 && count <= 4 {
			wantType = "TEXT"
		}
		if count == 0 {
			wantPrimary = 1
		}
		if count >= len(names) || position != count || !strings.EqualFold(name, names[count]) || !strings.EqualFold(kind, wantType) || primary != wantPrimary || notNull != 0 || hidden != 0 {
			return errors.New("desktop/linux: incompatible native cookie schema; use a compatible runtime or restore a backup while the app is stopped; refusing to reset cookies")
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != 9 && count != 10 {
		return errors.New("desktop/linux: incomplete native cookie schema; restore a backup while the app is stopped; refusing to reset cookies")
	}
	return nil
}
