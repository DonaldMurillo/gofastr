package desktop

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/DonaldMurillo/gofastr/battery/desktop/appstate"
)

// Window state that survives a relaunch (Config.RememberWindows). The
// remembered windows are one entry in the app state store: key
// "windows", holding a map from window id to that window's last frame
// and, for the main window, its last path. The store owns the file
// (state.json in the data dir, 0600), the debounced write, and the
// quit-time flush; this file is a thin typed client over it.

// stateFileName is the app state store's file under the battery's data
// dir.
const stateFileName = "state.json"

// windowsStateKey is the app state key holding the remembered windows.
// The state capability confines page keys to the "page." prefix, so
// this key is never reachable from the page.
const windowsStateKey = "windows"

// storedWindow is one window's remembered state. Frame is a pointer so
// a window whose frame was never reported still carries its path (and
// vice versa).
type storedWindow struct {
	Frame *Frame `json:"frame,omitempty"`
	Path  string `json:"path,omitempty"`
}

// windowStateFile is the shape of the windows entry.
type windowStateFile struct {
	Windows map[string]storedWindow `json:"windows"`
}

// windowStore reads and updates the remembered windows in the battery's
// app state store. Safe for concurrent use: the shell reports frames
// from goroutines and setPath arrives on HTTP goroutines, and mu
// serializes the read-modify-write of the one entry (the store
// underneath is itself safe for concurrent use).
type windowStore struct {
	state  *appstate.Store
	logger *slog.Logger

	// mu serializes the read-modify-write of the windows entry.
	mu sync.Mutex
}

// newWindowStore wraps the battery's app state store.
func newWindowStore(s *appstate.Store, logger *slog.Logger) *windowStore {
	return &windowStore{state: s, logger: logger}
}

// windows returns the remembered windows: nil when nothing is stored
// or the stored value does not fit the shape (both mean "start from
// defaults", the same posture as a corrupt file).
func (s *windowStore) windows() map[string]storedWindow {
	m, _, err := appstate.Get[map[string]storedWindow](s.state, windowsStateKey)
	if err != nil {
		s.logger.Warn("desktop: the stored window state does not decode; ignoring it and starting from defaults",
			"key", windowsStateKey, "error", err.Error())
		return nil
	}
	return m
}

// save stores the remembered windows; the store debounces the write.
func (s *windowStore) save(w map[string]storedWindow) {
	if err := s.state.Set(windowsStateKey, w); err != nil {
		s.logger.Warn("desktop: storing the window state failed", "error", err.Error())
	}
}

// frameFor returns the remembered frame for a window id, nil when there
// is none. The caller owns the returned copy.
func (s *windowStore) frameFor(id string) *Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.windows()[id]; ok && e.Frame != nil {
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
	return s.windows()[mainWindowID].Path
}

// setFrame remembers a window's frame; the store schedules the
// debounced write.
func (s *windowStore) setFrame(id string, f Frame) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.windows()
	if w == nil {
		w = make(map[string]storedWindow)
	}
	e := w[id]
	e.Frame = &f
	w[id] = e
	s.save(w)
}

// setMainPath remembers the main window's path; the store schedules
// the debounced write.
func (s *windowStore) setMainPath(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.windows()
	if w == nil {
		w = make(map[string]storedWindow)
	}
	e := w[mainWindowID]
	e.Path = p
	w[mainWindowID] = e
	s.save(w)
}

// readWindowsFile is the test reader: the remembered windows in a data
// dir's state file.
func readWindowsFile(dir string) (windowStateFile, error) {
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		return windowStateFile{}, err
	}
	var f struct {
		Entries map[string]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return windowStateFile{}, fmt.Errorf("state.json: %w", err)
	}
	var w windowStateFile
	if raw, ok := f.Entries[windowsStateKey]; ok {
		if err := json.Unmarshal(raw, &w); err != nil {
			return windowStateFile{}, fmt.Errorf("state.json %s entry: %w", windowsStateKey, err)
		}
	}
	return w, nil
}
