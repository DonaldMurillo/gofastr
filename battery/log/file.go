package log

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// FileOpts configures a file sink.
type FileOpts struct {
	// MaxSize is the maximum file size in bytes before rotation.
	MaxSize int64
	// MaxBackups is the number of rotated files kept.
	MaxBackups int
	// FileMode is the permission for the log file.
	FileMode os.FileMode
}

type fileSink struct {
	path string
	opts FileOpts

	mu     sync.Mutex
	f      *os.File
	bw     *bufio.Writer
	size   int64
	closed bool

	tightenWarn sync.Once
}

// FileSink opens (creating if needed) the file at path and returns a
// rotating sink.
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
	parent := filepath.Dir(path)
	if err := fileperm.MkdirPrivate(parent); err != nil {
		return nil, fmt.Errorf("log: mkdir %s: %w", parent, err)
	}
	s := &fileSink{path: path, opts: opts}
	if err := s.open(); err != nil {
		return nil, err
	}
	if err := fileperm.Restrict(path, false); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("log: restrict file %s: %w", path, err)
	}
	return s, nil
}

// MustFileSink is FileSink that panics on error.
func MustFileSink(path string, opts FileOpts) Sink {
	s, err := FileSink(path, opts)
	if err != nil {
		panic(err)
	}
	return s
}

// DefaultFileSink resolves $XDG_STATE_HOME/<app>/server.log when the
// variable is non-empty, or ~/.local/state/<app>/server.log otherwise.
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
	s.tightenLocked(f, info)
	s.f = f
	s.bw = bufio.NewWriterSize(f, 16<<10)
	s.size = info.Size()
	return nil
}

// Write appends a single JSON-encoded entry plus a trailing '\n'.
func (s *fileSink) Write(entry []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSinkClosed
	}
	if s.f == nil {
		if err := s.open(); err != nil {
			return ErrSinkWedged
		}
	}
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
	_ = s.f.Sync()
	err := s.f.Close()
	s.f = nil
	return err
}

func (s *fileSink) rotateLocked() error {
	if err := s.bw.Flush(); err != nil {
		return err
	}
	if err := s.f.Close(); err != nil {
		return err
	}
	s.f = nil

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
		if err != nil || n < 1 || strconv.Itoa(n) != suffix {
			continue
		}
		existing = append(existing, rotated{path: filepath.Join(dir, name), n: n})
	}
	sort.Slice(existing, func(i, j int) bool { return existing[i].n > existing[j].n })

	for _, r := range existing {
		if r.n >= s.opts.MaxBackups {
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

func (s *fileSink) tightenLocked(f *os.File, info os.FileInfo) {
	perm := info.Mode().Perm()
	if perm&^s.opts.FileMode == 0 {
		return
	}
	uid, ok := fileOwnerUID(info)
	if !ok || uid != processEUID() {
		return
	}
	if err := chmodHandle(f, perm&s.opts.FileMode); err != nil {
		s.tightenWarn.Do(func() {
			fmt.Fprintf(tightenWarnOut, "log: could not tighten %s from %o to %o, appending with its mode unchanged: %v\n",
				s.path, perm, perm&s.opts.FileMode, err)
		})
	}
}

var (
	fileOwnerUID             = ownerUID
	processEUID              = os.Geteuid
	chmodHandle              = (*os.File).Chmod
	tightenWarnOut io.Writer = os.Stderr
)
