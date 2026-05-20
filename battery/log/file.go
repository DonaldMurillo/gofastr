package log

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// FileOpts configures a file sink.
type FileOpts struct {
	// MaxSize is the maximum file size in bytes before rotation. Default 100MB.
	MaxSize int64
	// MaxBackups is the number of rotated files kept. Older files are
	// removed. Default 5.
	MaxBackups int
	// FileMode is the permission for the log file. Default 0o600 —
	// server logs typically contain request paths, request IDs, and
	// panic stacks; world-readable defaults aren't appropriate on a
	// multi-user host. Override only if you need broader access.
	FileMode os.FileMode
}

// fileSink is the concrete Sink for path-on-disk output. Writes are
// buffered and serialized with a mutex; rotation is triggered when the
// in-flight size crosses MaxSize.
//
// After Close, Write and Close are no-ops returning ErrSinkClosed —
// derived loggers held by long-lived goroutines past shutdown don't
// crash, they just stop persisting. Callers that care can check for
// ErrSinkClosed.
type fileSink struct {
	path string
	opts FileOpts

	mu     sync.Mutex
	f      *os.File
	bw     *bufio.Writer
	size   int64
	closed bool
}

// FileSink opens (creating if needed) the file at path and returns a
// rotating sink. The parent directory is created with mode 0o755.
func FileSink(path string, opts FileOpts) (Sink, error) {
	if opts.MaxSize <= 0 {
		opts.MaxSize = 100 << 20
	}
	if opts.MaxBackups <= 0 {
		opts.MaxBackups = 5
	}
	if opts.FileMode == 0 {
		opts.FileMode = 0o600
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("log: mkdir %s: %w", filepath.Dir(path), err)
	}
	s := &fileSink{path: path, opts: opts}
	if err := s.open(); err != nil {
		return nil, err
	}
	return s, nil
}

// MustFileSink is FileSink that panics on error. Useful in Config struct
// literals where error handling would be awkward.
func MustFileSink(path string, opts FileOpts) Sink {
	s, err := FileSink(path, opts)
	if err != nil {
		panic(err)
	}
	return s
}

// DefaultFileSink resolves a per-app file under the OS state dir.
// On linux this honors $XDG_STATE_HOME; otherwise ~/.local/state/<app>/server.log.
//
// appName must be non-empty. Two apps that both pass an empty name
// would otherwise silently share one log file ("gofastr/server.log")
// and clobber each other — a real footgun when the same binary runs
// for multiple App instances in tests or subcommands.
func DefaultFileSink(appName string, opts FileOpts) (Sink, error) {
	if appName == "" {
		return nil, fmt.Errorf("log: DefaultFileSink requires a non-empty app name (set framework.AppConfig.Name or pass a custom Sink)")
	}
	dir, err := stateDir(appName)
	if err != nil {
		return nil, err
	}
	return FileSink(filepath.Join(dir, "server.log"), opts)
}

func (s *fileSink) open() error {
	// O_NOFOLLOW: if the path is a symlink, fail. A malicious user with
	// write access to the parent directory could otherwise plant a
	// symlink at the log path before the app starts and redirect logs
	// into a file they read.
	flags := os.O_APPEND | os.O_CREATE | os.O_WRONLY | unixNoFollow
	f, err := os.OpenFile(s.path, flags, s.opts.FileMode)
	if err != nil {
		return fmt.Errorf("log: open %s: %w", s.path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("log: stat %s: %w", s.path, err)
	}
	s.f = f
	s.bw = bufio.NewWriterSize(f, 16<<10)
	s.size = info.Size()
	return nil
}

// Write appends a single JSON-encoded entry plus a trailing '\n'.
// Rotation runs if the post-write size would cross MaxSize.
//
// Returns ErrSinkClosed if Close has already run — no panic from a
// derived logger that outlives the App's shutdown.
func (s *fileSink) Write(entry []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSinkClosed
	}
	if s.f == nil {
		// A previous rotation reopen failed (most likely a planted
		// symlink at s.path or a missing parent dir). Attempt one
		// re-open per Write so the sink self-heals once the issue is
		// fixed; surface ErrSinkWedged if the reopen still fails so
		// the fallback emits a useful message.
		if err := s.open(); err != nil {
			return ErrSinkWedged
		}
	}
	// Rotate BEFORE writing if the entry would push us past MaxSize and
	// the current file already has content. (An entry larger than
	// MaxSize alone still gets written to a fresh file; we don't split.)
	if s.size > 0 && s.size+int64(len(entry))+1 > s.opts.MaxSize {
		if err := s.rotateLocked(); err != nil {
			return err
		}
	}
	n, err := s.bw.Write(entry)
	if err != nil {
		return err
	}
	if err := s.bw.WriteByte('\n'); err != nil {
		return err
	}
	s.size += int64(n) + 1
	// Flush after every entry. Buffering exists only to coalesce when
	// the kernel is busy, not to delay durability — server logs are
	// time-critical for debugging.
	return s.bw.Flush()
}

func (s *fileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.f == nil {
		return nil
	}
	if err := s.bw.Flush(); err != nil {
		_ = s.f.Close()
		s.f = nil
		return err
	}
	// fsync before close: bw.Flush only pushes bytes into the kernel
	// write cache. On SIGKILL / OOM / kernel panic mid-Close, anything
	// still in cache is lost — exactly the data you most want to keep
	// at incident time. Sync is one syscall on shutdown; negligible.
	_ = s.f.Sync()
	err := s.f.Close()
	s.f = nil
	return err
}

// rotateLocked renames the active file to "<path>.1", shifts existing
// backups up by one, drops anything past MaxBackups, then reopens.
// Caller holds s.mu.
func (s *fileSink) rotateLocked() error {
	if err := s.bw.Flush(); err != nil {
		return err
	}
	if err := s.f.Close(); err != nil {
		return err
	}
	s.f = nil

	// Walk backups newest→oldest. ".5" -> remove, ".4" -> ".5", … ".1" -> ".2", "" -> ".1".
	dir := filepath.Dir(s.path)
	base := filepath.Base(s.path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type rotated struct {
		path string
		n    int
	}
	var existing []rotated
	prefix := base + "."
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		suffix := name[len(prefix):]
		n, err := strconv.Atoi(suffix)
		if err != nil || n < 1 {
			continue
		}
		existing = append(existing, rotated{path: filepath.Join(dir, name), n: n})
	}
	sort.Slice(existing, func(i, j int) bool { return existing[i].n > existing[j].n })

	for _, r := range existing {
		if r.n >= s.opts.MaxBackups {
			// drop oldest
			_ = os.Remove(r.path)
			continue
		}
		next := filepath.Join(dir, fmt.Sprintf("%s.%d", base, r.n+1))
		if err := os.Rename(r.path, next); err != nil {
			return fmt.Errorf("log: rotate rename %s -> %s: %w", r.path, next, err)
		}
	}
	if err := os.Rename(s.path, filepath.Join(dir, base+".1")); err != nil {
		return fmt.Errorf("log: rotate active: %w", err)
	}

	return s.open()
}
