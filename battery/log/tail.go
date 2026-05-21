package log

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// tailFile returns up to maxLines decoded log entries from the end of
// the file at path, in chronological order (oldest first within the
// returned window). Each entry is parsed as JSON; non-JSON / partial
// lines are skipped.
//
// Used by the log MCP tools for historical queries that fall outside
// the in-memory RingSink's retention window. Caps and seek are
// approximate: we read the last maxLines*8 KiB by default, which
// covers ~typical access entries comfortably. Tune via the second
// parameter if you have unusually large entries.
//
// Returns (nil, nil) when the file doesn't exist — agents asking for
// historical logs against an app that hasn't enabled a file sink should
// get an empty list, not an error.
func tailFile(path string, maxLines int) ([]map[string]any, error) {
	if maxLines <= 0 {
		maxLines = 100
	}
	return tailFileWithWindow(path, maxLines, int64(maxLines)*8192)
}

func tailFileWithWindow(path string, maxLines int, window int64) ([]map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("log: tail open %s: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("log: tail stat %s: %w", path, err)
	}

	// Seek to (size - window), or 0 if the file is smaller.
	offset := info.Size() - window
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("log: tail seek %s: %w", path, err)
	}

	// If we seeked into the middle of a line, discard the partial one.
	scanner := bufio.NewScanner(f)
	// Allow up to 1 MiB per line — the recovery middleware caps stacks
	// at 64 KiB and panic values at 4 KiB, so this is comfortable.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	if offset > 0 && scanner.Scan() {
		// Skipped the (possibly partial) first line.
	}

	// Accumulate. Drop oldest if we exceed maxLines.
	out := make([]map[string]any, 0, maxLines)
	for scanner.Scan() {
		var m map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			continue // ignore malformed lines
		}
		if len(out) == maxLines {
			out = out[1:]
		}
		out = append(out, m)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("log: tail scan %s: %w", path, err)
	}
	return out, nil
}
