package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/settings"
	"golang.org/x/mod/semver"
)

// unmountedCompareURL is the upstream diff between a copy's version and
// a newer one.
func unmountedCompareURL(from, to string) string {
	return "https://github.com/DonaldMurillo/gofastr/compare/" + from + "..." + to
}

// unmountedBehind reports whether a copy made at version is older than
// the release against. Both must be release versions: a dev copy came
// from a checkout and names no release to diff.
func unmountedBehind(version, against string) bool {
	return semver.IsValid(version) && semver.IsValid(against) && isReleaseTag(version) &&
		isReleaseTag(against) && semver.Compare(version, against) < 0
}

// describeUnmounted is the one-line account of a copy that verify,
// upgrade and audit print.
func describeUnmounted(e settings.Unmounted) string {
	return fmt.Sprintf("%s %s at %s (unmounted %s)", e.Source, e.Version, e.Path, e.Date)
}

// unmountedSettings reads the unmounted section of the settings file that
// governs root. A missing file is no copies.
func unmountedSettings(root string) (settings.File, []settings.Unmounted, error) {
	f, ok, err := settings.Find(root, "unmounted")
	if err != nil || !ok {
		return settings.File{}, nil, err
	}
	entries, err := f.Unmounted()
	return f, entries, err
}

// reportUnmounted is verify's account of unmounted copies: each copy the
// settings file lists, a warning for one older than the gofastr release
// go.mod requires, for an entry whose directory holds no files, and for
// a file marked as a copy of a package the settings file does not list.
func reportUnmounted(root string, marked []provenanceEntry, note, noteWarn func(string, ...any)) {
	f, entries, err := unmountedSettings(root)
	if err != nil {
		noteWarn("could not read the unmounted packages: %v", err)
		return
	}
	required, replaced, modErr := goModGofastrVersion(root)
	if modErr != nil && len(entries) > 0 {
		noteWarn("cannot tell whether the unmounted copies are behind the gofastr release: %v", modErr)
	}
	listed := map[string]bool{}
	for _, e := range entries {
		listed[e.Source] = true
		note("Unmounted: %s", describeUnmounted(e))
		if !replaced && unmountedBehind(e.Version, required) {
			noteWarn("%s was copied at %s and go.mod requires gofastr %s: fixes since then do not reach the copy. Diff: %s", e.Source, e.Version, required, unmountedCompareURL(e.Version, required))
		}
		if dirEntries, err := os.ReadDir(filepath.Join(f.Dir, filepath.FromSlash(e.Path))); err != nil || len(dirEntries) == 0 {
			noteWarn("%s lists %s at %s, but that directory holds no files: run `gofastr remount %s` to drop the entry", settings.FileName, e.Source, e.Path, e.Path)
		}
	}
	var unlisted []string
	for _, m := range marked {
		if m.Marker.Generator == unmountGenerator && !listed[m.Marker.Source] && !slices.Contains(unlisted, m.Marker.Source) {
			unlisted = append(unlisted, m.Marker.Source)
			noteWarn("%s is marked as an unmounted copy of %s, which %s does not list: tools cannot report it. Restore the entry, or remove the marker if the code is yours now", m.Path, m.Marker.Source, settings.FileName)
		}
	}
}

// printUnmountedForUpgrade lists the copies an upgrade does not touch,
// each older one with its upstream diff to the target.
func printUnmountedForUpgrade(root, target string) {
	_, entries, err := unmountedSettings(root)
	if err != nil {
		fmt.Printf("NOTE: could not read the unmounted packages: %v\n\n", err)
		return
	}
	if len(entries) == 0 {
		return
	}
	fmt.Println("Unmounted packages (upgrade never changes them; carry what you need by hand):")
	for _, e := range entries {
		fmt.Printf("  %s\n", describeUnmounted(e))
		switch {
		case unmountedBehind(e.Version, target):
			fmt.Printf("    changes upstream since: %s\n", unmountedCompareURL(e.Version, target))
		case !isReleaseTag(e.Version) || !semver.IsValid(e.Version):
			fmt.Printf("    copied from a checkout, not a release: compare it with %s by hand\n", target)
		}
	}
	fmt.Println()
}
