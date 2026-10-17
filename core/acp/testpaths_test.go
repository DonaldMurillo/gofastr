package acp_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"runtime"
)

func testAbsoluteCWD(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		panic(err)
	}
	return abs
}

// Keep the raw frame's duplicate keys and malformed fields intact. Only
// the known valid absolute cwd fixtures need a drive prefix on Windows.
func normalizeTestCWDs(frame []byte) []byte {
	if runtime.GOOS != "windows" {
		return frame
	}
	for _, path := range []string{"/tmp/p", "/tmp/proj", "/tmp/safe"} {
		old, _ := json.Marshal(path)
		replacement, err := json.Marshal(testAbsoluteCWD(path))
		if err != nil {
			panic(err)
		}
		frame = bytes.ReplaceAll(frame, old, replacement)
	}
	return frame
}
