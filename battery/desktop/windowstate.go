package desktop

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Window state that survives a relaunch (Config.RememberWindows). The
// battery owns the file: windows.json in the data dir, 0600, holding
// every window's last frame and the main window's last path. Writes are
// debounced (the shell reports every move tick) and the last write
// happens on quit, so a drag never hammers the disk and a clean exit
// never loses the final position.

// windowsFileName is the store's file under the battery's data dir.
const windowsFileName = "windows.json"

// windowWriteDelay is the debounce window for store writes. Every
// windowDidMove: tick reschedules; one write lands this long after the
// user stops dragging.
const windowWriteDelay = 500 * time.Millisecond

// storedWindow is one window's remembered state. Frame is a pointer so
// a window whose frame was never reported still carries its path (and
// vice versa).
type storedWindow struct {
	Frame *Frame `json:"frame,omitempty"`
	Path  string `json:"path,omitempty"`
}

// windowStateFile is the on-disk shape.
type windowStateFile struct {
	Windows map[string]storedWindow `json:"windows"`
}

// windowStore loads, updates, and persists windows.json. Safe for
// concurrent use: the shell reports frames from goroutines, setPath
// arrives on HTTP goroutines, and flush runs on the quit path.
type windowStore struct {
	logger *slog.Logger

	// mu guards windows and dirty. The timer and the writers also take
	// it, so a flush always sees the latest state.
	mu      sync.Mutex
	path    string
	windows map[string]storedWindow
	dirty   bool
	timer   *time.Timer

	// writeMu serializes file writes so a debounced write racing the
	// quit flush cannot interleave partial files.
	writeMu sync.Mutex
}

// loadWindowStore reads dir/windows.json. A missing file is an empty
// store; a corrupt one is ignored with a Warn (a truncated quit-time
// write must not brick the next launch).
func loadWindowStore(dir string, logger *slog.Logger) *windowStore {
	s := &windowStore{
		logger:  logger,
		path:    filepath.Join(dir, windowsFileName),
		windows: make(map[string]storedWindow),
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			s.logger.Warn("desktop: reading the window state file failed; starting from defaults",
				"file", windowsFileName, "error", err.Error())
		}
		return s
	}
	var f windowStateFile
	if err := json.Unmarshal(data, &f); err != nil || f.Windows == nil {
		s.logger.Warn("desktop: the window state file is unreadable; ignoring it and starting from defaults",
			"file", windowsFileName)
		return s
	}
	s.windows = f.Windows
	return s
}

// frameFor returns the remembered frame for a window id, nil when there
// is none. The caller owns the returned copy.
func (s *windowStore) frameFor(id string) *Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.windows[id]; ok && e.Frame != nil {
		f := *e.Frame
		return &f
	}
	return nil
}

// mainPath returns the main window's remembered path ("" when none).
// The caller validates it before use: the file is data at rest, and the
// redirect target is checked again on the way out.
func (s *windowStore) mainPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.windows[mainWindowID].Path
}

// setFrame remembers a window's frame and schedules the debounced
// write.
func (s *windowStore) setFrame(id string, f Frame) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.windows[id]
	e.Frame = &f
	s.windows[id] = e
	s.schedule()
}

// setMainPath remembers the main window's path and schedules the
// debounced write.
func (s *windowStore) setMainPath(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.windows[mainWindowID]
	e.Path = p
	s.windows[mainWindowID] = e
	s.schedule()
}

// schedule arms the debounced write; the caller holds mu.
func (s *windowStore) schedule() {
	s.dirty = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(windowWriteDelay, s.write)
}

// write persists the current state (the timer callback).
func (s *windowStore) write() {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	snapshot := s.snapshotLocked()
	s.mu.Unlock()
	s.persist(snapshot)
}

// flush persists any pending change immediately (the quit path).
func (s *windowStore) flush() {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
	}
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	snapshot := s.snapshotLocked()
	s.mu.Unlock()
	s.persist(snapshot)
}

// snapshotLocked copies the state for a write; the caller holds mu.
func (s *windowStore) snapshotLocked() windowStateFile {
	out := windowStateFile{Windows: make(map[string]storedWindow, len(s.windows))}
	for id, e := range s.windows {
		if e.Frame != nil {
			f := *e.Frame
			e.Frame = &f
		}
		out.Windows[id] = e
	}
	s.dirty = false
	return out
}

// persist writes the snapshot. A failed write is a Warn, never a
// launch-killer: the window opens where it did last time instead.
func (s *windowStore) persist(f windowStateFile) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		s.logger.Warn("desktop: encoding the window state failed", "error", err.Error())
		return
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		s.logger.Warn("desktop: writing the window state file failed", "error", err.Error())
	}
}

// readWindowsFile is the test reader: the stored state of a data dir.
func readWindowsFile(dir string) (windowStateFile, error) {
	data, err := os.ReadFile(filepath.Join(dir, windowsFileName))
	if err != nil {
		return windowStateFile{}, err
	}
	var f windowStateFile
	if err := json.Unmarshal(data, &f); err != nil {
		return windowStateFile{}, fmt.Errorf("windows.json: %w", err)
	}
	return f, nil
}
