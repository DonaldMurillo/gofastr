package verifycmd

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/core/settings"
)

// reportUnmounted is verify's account of unmounted copies: each copy the
// settings file lists, a warning for one older than the gofastr release
// go.mod requires, for an entry whose directory holds no files, and for
// a file marked as a copy of a package the settings file does not list.
func reportUnmounted(root string, marked []provenanceEntry, note, noteWarn func(string, ...any)) {
	f, entries, err := project.UnmountedSettings(root)
	if err != nil {
		noteWarn("could not read the unmounted packages: %v", err)
		return
	}
	required, replaced, modErr := project.GoModGofastrVersion(root)
	if modErr != nil && len(entries) > 0 {
		noteWarn("cannot tell whether the unmounted copies are behind the gofastr release: %v", modErr)
	}
	listed := map[string]bool{}
	for _, e := range entries {
		listed[e.Source] = true
		note("Unmounted: %s", project.DescribeUnmounted(e))
		if !replaced && project.UnmountedBehind(e.Version, required) {
			noteWarn("%s was copied at %s and go.mod requires gofastr %s: fixes since then do not reach the copy. Diff: %s", e.Source, e.Version, required, project.UnmountedCompareURL(e.Version, required))
		}
		if dirEntries, err := os.ReadDir(filepath.Join(f.Dir, filepath.FromSlash(e.Path))); err != nil || len(dirEntries) == 0 {
			noteWarn("%s lists %s at %s, but that directory holds no files: run `gofastr remount %s` to drop the entry", settings.FileName, e.Source, e.Path, e.Path)
		}
	}
	var unlisted []string
	for _, m := range marked {
		if m.Marker.Generator == project.UnmountGenerator && !listed[m.Marker.Source] && !slices.Contains(unlisted, m.Marker.Source) {
			unlisted = append(unlisted, m.Marker.Source)
			noteWarn("%s is marked as an unmounted copy of %s, which %s does not list: tools cannot report it. Restore the entry, or remove the marker if the code is yours now", m.Path, m.Marker.Source, settings.FileName)
		}
	}
}
