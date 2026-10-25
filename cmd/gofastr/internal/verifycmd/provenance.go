package verifycmd

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/codegen"
)

// provenanceMaxBytes bounds the files the provenance scan reads: a
// generated source file is far smaller, and a large asset is not one.
const provenanceMaxBytes = 4 << 20

// provenanceEntry is one marked file the scan found.
type provenanceEntry struct {
	Path   string
	Marker codegen.Marker
	Edited bool
}

// ScanProvenance walks root for files carrying a provenance marker,
// skipping the directories the go tool skips (vendor, testdata,
// node_modules, and names starting with "." or "_").
func ScanProvenance(root string) ([]provenanceEntry, error) {
	var found []provenanceEntry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (name == "vendor" || name == "testdata" || name == "node_modules" ||
				strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !codegen.Stampable(name) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > provenanceMaxBytes {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		m, body, ok := codegen.ReadMarker(name, string(data))
		if !ok {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = p
		}
		found = append(found, provenanceEntry{Path: filepath.ToSlash(rel), Marker: m, Edited: codegen.Sum(body) != m.Sum})
		return nil
	})
	return found, err
}

// reportProvenance is verify's account of generated code: how many marked
// files each generator wrote, at which version, and which derived files
// were edited since (the next generate discards those edits). Edited
// scaffold files are counted, not flagged: the user owns them. It returns
// the marked files for the unmounted report.
func reportProvenance(root string, note, noteWarn func(string, ...any)) []provenanceEntry {
	entries, err := ScanProvenance(root)
	if err != nil {
		noteWarn("could not scan for generated files: %v", err)
		return nil
	}
	if len(entries) == 0 {
		return nil
	}
	counts := map[string]int{}
	scaffoldEdited := 0
	for _, e := range entries {
		label := string(e.Marker.Kind) + " " + e.Marker.Generator
		if e.Marker.Version != "" {
			label += " " + e.Marker.Version
		}
		counts[label]++
		if !e.Edited {
			continue
		}
		if e.Marker.Kind == codegen.KindDerived {
			noteWarn("%s was edited since `gofastr generate %s` wrote it; the next generate rebuilds it and the edit is lost", e.Path, e.Marker.Generator)
		} else {
			scaffoldEdited++
		}
	}
	var parts []string
	for _, label := range slices.Sorted(maps.Keys(counts)) {
		parts = append(parts, fmt.Sprintf("%s: %d", label, counts[label]))
	}
	msg := "Generated files: " + strings.Join(parts, ", ")
	if scaffoldEdited > 0 {
		msg += fmt.Sprintf("; %d scaffold file(s) edited since, which is expected: they are yours", scaffoldEdited)
	}
	note("%s", msg)
	return entries
}
