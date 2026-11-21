// Package ancestor searches a directory and its parents.
package ancestor

import "path/filepath"

// Find returns the directory where the search stops, whether it matched,
// and any probe error.
func Find(start string, probe func(dir string) (bool, error)) (string, bool, error) {
	for dir := start; ; {
		match, err := probe(dir)
		if err != nil {
			return dir, false, err
		}
		if match {
			return dir, true, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir, false, nil
		}
		dir = parent
	}
}
