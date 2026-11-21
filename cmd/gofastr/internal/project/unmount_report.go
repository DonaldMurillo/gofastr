package project

import (
	"fmt"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/core/settings"
	"golang.org/x/mod/semver"
)

// UnmountedCompareURL is the upstream diff between a copy's version and
// a newer one.
func UnmountedCompareURL(from, to string) string {
	return "https://github.com/DonaldMurillo/gofastr/compare/" + from + "..." + to
}

// UnmountedBehind reports whether a copy made at version is older than
// the release against. Both must be release versions: a dev copy came
// from a checkout and names no release to diff.
func UnmountedBehind(version, against string) bool {
	return semver.IsValid(version) && semver.IsValid(against) && IsReleaseTag(version) &&
		IsReleaseTag(against) && semver.Compare(version, against) < 0
}

// DescribeUnmounted is the one-line account of a copy that verify,
// upgrade and audit print.
func DescribeUnmounted(e settings.Unmounted) string {
	return fmt.Sprintf("%s %s at %s (unmounted %s)", e.Source, e.Version, e.Path, e.Date)
}

// UnmountedSettings reads the unmounted section of the settings file that
// governs root, returning the path it came from; with no file it answers
// no copies and the path root's settings file would have, for the caller
// that writes one back.
func UnmountedSettings(root string) (string, []settings.Unmounted, error) {
	f, ok, err := settings.Find(root, "unmounted")
	if err != nil || !ok {
		return filepath.Join(root, settings.FileName), nil, err
	}
	entries, err := f.Unmounted()
	return f.Path, entries, err
}
