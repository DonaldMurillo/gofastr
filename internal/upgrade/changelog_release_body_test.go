package upgrade

import (
	"strings"
	"testing"
)

// releaseBodyLimit is GitHub's cap on a release body, in characters.
// The release workflow publishes a version's CHANGELOG.md section as
// its release body, so a longer section fails the publish with HTTP 422
// after the gate has passed, as v0.96.0 did.
const releaseBodyLimit = 125_000

// Every released section of CHANGELOG.md fits in a GitHub release body.
// It counts bytes, which are never fewer than characters, so a section
// that passes fits whatever unit GitHub counts in.
func TestChangelogSectionsFitReleaseBody(t *testing.T) {
	var name string
	var body strings.Builder
	check := func() {
		if name == "" || strings.HasPrefix(name, "## [Unreleased]") {
			return
		}
		if n := body.Len(); n > releaseBodyLimit {
			t.Errorf("%s is %d bytes; a GitHub release body takes at most %d characters. Summarize its Fixed and Security lists as v0.96.0 did (commit 535852ce0).",
				name, n, releaseBodyLimit)
		}
	}
	for line := range strings.Lines(readChangelogRoot(t)) {
		if strings.HasPrefix(line, "## [") {
			check()
			name, body = strings.TrimSpace(line), strings.Builder{}
			continue
		}
		body.WriteString(line)
	}
	check()
}
