package ancestor

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func TestFind(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	start := filepath.Join(child, "start")
	probeErr := errors.New("probe failed")
	for _, tt := range []struct {
		name, match, want string
		stop              error
		visited           []string
	}{
		{name: "starting directory", match: start, want: start, visited: []string{start}},
		{name: "parent", match: child, want: child, visited: []string{start, child}},
		{name: "probe error before match", match: parent, want: child, stop: probeErr, visited: []string{start, child}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var visited []string
			got, matched, err := Find(start, func(dir string) (bool, error) {
				visited = append(visited, dir)
				if dir == child && tt.stop != nil {
					return false, tt.stop
				}
				return dir == tt.match, nil
			})
			if got != tt.want || matched != (tt.stop == nil) || !errors.Is(err, tt.stop) || !slices.Equal(visited, tt.visited) {
				t.Fatalf("Find = %q, %t, %v, visited %q; want %q, %t, %v, visited %q",
					got, matched, err, visited, tt.want, tt.stop == nil, tt.stop, tt.visited)
			}
		})
	}
}
