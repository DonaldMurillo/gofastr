package telemetry

import (
	"encoding/json"
	"log/slog"
	"slices"
	"testing"
	"time"
)

// The Logs screen hides a quiet path's access lines by the same rule the
// collector calls a request quiet by (otelspan's under): the path itself
// and what is below it, never a longer name that starts with the same
// letters.
func TestLogsSkipPathsStopAtAPathSegment(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second)
	for i, path := range []string{"/healthz", "/healthz/db", "/healthz2", "/assets/app.js", "/assets", "/assets2/app.js"} {
		line := `{"time":"` + at.Add(time.Duration(i)*time.Millisecond).Format(time.RFC3339Nano) + `","level":"INFO","msg":"http.request","path":"` + path + `"}`
		if err := w.WriteLog([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Logs(LogQuery{MinLevel: slog.LevelDebug, SkipPaths: []string{"/healthz", "/assets/"}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range got {
		var line struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(e.Line, &line); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, line.Path)
	}
	slices.Sort(paths)
	if want := []string{"/assets", "/assets2/app.js", "/healthz2"}; !slices.Equal(paths, want) {
		t.Fatalf("lines left after skipping /healthz and /assets/: %v, want %v", paths, want)
	}
}
