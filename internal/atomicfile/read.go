// Package atomicfile supports replacement of complete files on disk.
package atomicfile

import "io"

// ReadFile reads a file while permitting another writer to replace it.
func ReadFile(path string) ([]byte, error) {
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
