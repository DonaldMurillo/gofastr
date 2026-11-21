package desktop

import (
	"log/slog"
	"sync"

	"github.com/DonaldMurillo/gofastr/battery/desktop/appstate"
)

const stateFileName = "state.json"

const windowsStateKey = "windows"

type storedWindow struct {
	Frame *Frame `json:"frame,omitempty"`
	Path  string `json:"path,omitempty"`
}

type windowStore struct {
	state  *appstate.Store
	logger *slog.Logger

	mu sync.Mutex
}

func newWindowStore(s *appstate.Store, logger *slog.Logger) *windowStore {
	store := &windowStore{state: s, logger: logger}
	windows := store.windows()
	changed := false
	for id := range windows {
		if id == MainWindowID || id == "settings" {
			continue
		}
		delete(windows, id)
		changed = true
	}
	if changed {
		store.save(windows)
	}
	return store
}

func (s *windowStore) windows() map[string]storedWindow {
	m, _, err := appstate.Get[map[string]storedWindow](s.state, windowsStateKey)
	if err != nil {
		s.logger.Warn("desktop: the stored window state does not decode; ignoring it and starting from defaults",
			"key", windowsStateKey, "error", err.Error())
		return nil
	}
	return m
}

func (s *windowStore) save(w map[string]storedWindow) {
	if err := s.state.Set(windowsStateKey, w); err != nil {
		s.logger.Warn("desktop: storing the window state failed", "error", err.Error())
	}
}

func (s *windowStore) frameFor(id string) *Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.windows()[id]; ok && e.Frame != nil {
		f := *e.Frame
		return &f
	}
	return nil
}

func (s *windowStore) mainPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.windows()[MainWindowID].Path
}

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

func (s *windowStore) forget(id string) {
	if id == "" || id == MainWindowID || id == "settings" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.windows()
	if _, ok := w[id]; !ok {
		return
	}
	delete(w, id)
	s.save(w)
}

func (s *windowStore) setMainPath(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.windows()
	if w == nil {
		w = make(map[string]storedWindow)
	}
	e := w[MainWindowID]
	e.Path = p
	w[MainWindowID] = e
	s.save(w)
}
