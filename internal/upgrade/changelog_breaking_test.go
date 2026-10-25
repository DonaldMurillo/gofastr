package upgrade

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/semver"
)

// readChangelog loads the repo's CHANGELOG.md.
//
//gofastr:test-reads /CHANGELOG.md
func readChangelog(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	return string(b)
}

// CHANGELOG.md promises breaking changes are marked **BREAKING**, and the
// registry records the same releases for `gofastr upgrade`. Those two must
// agree: the changelog section is what feeds `gh release create
// --notes-file`, and the registry is what an upgrading project actually
// reads. A release marked breaking in one and not the other means somebody
// reads the wrong story.
//
// This replaced a gate pinned to the Unreleased heading, which stopped
// looking the moment the section was rolled to its version number, the one
// commit in a release where the marker matters most.
func TestChangelogAgreesWithUpgradesOnBreaking(t *testing.T) {
	breakingByVersion := registryBreaking(t)

	for _, ver := range changelogVersions(t) {
		known, ok := breakingByVersion[ver.version]
		if !ok {
			// A release with no migration-relevant change gets no
			// registry file; that is the documented shape, and it is
			// also how a release with no breaking change looks.
			known = false
		}
		marked := marksBreaking(ver.body)
		switch {
		case known && !marked:
			t.Errorf("internal/upgrade/releases/%[1]s.yml marks %[1]s breaking, but its CHANGELOG.md "+
				"section never says BREAKING — the release notes come from that section, "+
				"so an upgrading reader is not warned", ver.version)
		case marked && !known:
			t.Errorf("CHANGELOG.md marks %s BREAKING, but internal/upgrade/releases/ has no "+
				"breaking note for it — `gofastr upgrade` will not surface the change. "+
				"The registry's maintenance rule: a release that lands BREAKING "+
				"changes adds releases/<version>.yml", ver.version)
		}
	}
}

// marksBreaking reports whether a changelog section flags a breaking change.
//
// The marker is the all-caps word, which the file spells four ways across its
// history: `**BREAKING:` at the head of an entry, `BREAKING.**` at the tail
// of one, a `### BREAKING` heading, and plain prose ("Most entries below are
// BREAKING"). Matching the word covers all four. Two uses are not markers and
// are removed first: a section stating it has none, and the v0.26.1 entry
// describing the release process's own "release/BREAKING section".
func marksBreaking(body string) bool {
	for _, notAMarker := range []string{"No BREAKING", "no BREAKING", "release/BREAKING"} {
		body = strings.ReplaceAll(body, notAMarker, "")
	}
	return strings.Contains(body, "BREAKING")
}

type changelogSection struct {
	version string // "v0.52.0"
	body    string
}

// changelogVersions returns every `## [X.Y.Z]` section of CHANGELOG.md with
// its body. `## [Unreleased]` is skipped: it carries no version to compare
// against, and it is empty between releases.
func changelogVersions(t *testing.T) []changelogSection {
	t.Helper()
	lines := strings.Split(readChangelog(t), "\n")
	var out []changelogSection
	cur := -1
	for _, line := range lines {
		if after, ok := strings.CutPrefix(line, "## ["); ok {
			name, _, ok := strings.Cut(after, "]")
			if !ok || name == "Unreleased" {
				cur = -1
				continue
			}
			out = append(out, changelogSection{version: "v" + name})
			cur = len(out) - 1
			continue
		}
		if cur >= 0 {
			out[cur].body += line + "\n"
		}
	}
	if len(out) == 0 {
		t.Fatal("CHANGELOG.md has no `## [X.Y.Z]` sections")
	}
	return out
}

// TestUnreleasedMarksPendingBreaking: between releases the registry's
// one pending file (the release above `through`) is what Unreleased
// will become, so the two agree on BREAKING during the cycle, not only
// once the section is rolled to its version.
func TestUnreleasedMarksPendingBreaking(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("load the migration registry: %v", err)
	}
	pending, breaking := "", false
	for _, r := range reg.Releases {
		if semver.Less(reg.Through, r.Version) {
			pending = r.Version
			breaking = slices.ContainsFunc(r.Notes, func(n *Note) bool { return n.Breaking })
		}
	}
	marked := marksBreaking(changelogUnreleased(t))
	switch {
	case breaking && !marked:
		t.Errorf("internal/upgrade/releases/%s.yml is pending with a breaking note, but CHANGELOG.md's "+
			"Unreleased section never says BREAKING", pending)
	case marked && !breaking:
		t.Errorf("CHANGELOG.md's Unreleased section says BREAKING, but no pending file above through %s "+
			"carries a breaking note — `gofastr upgrade` will not surface the change", reg.Through)
	}
}

// changelogUnreleased returns the body of CHANGELOG.md's `## [Unreleased]`
// section.
func changelogUnreleased(t *testing.T) string {
	t.Helper()
	_, rest, ok := strings.Cut(readChangelog(t), "\n## [Unreleased]\n")
	if !ok {
		t.Fatal("CHANGELOG.md has no `## [Unreleased]` section")
	}
	body, _, _ := strings.Cut(rest, "\n## [")
	return body
}

// registryBreaking reports, per release in the registry, whether any of
// its notes is `breaking: true`.
func registryBreaking(t *testing.T) map[string]bool {
	t.Helper()
	reg, err := Load()
	if err != nil {
		t.Fatalf("load the migration registry: %v", err)
	}
	out := map[string]bool{}
	for _, r := range reg.Releases {
		out[r.Version] = slices.ContainsFunc(r.Notes, func(n *Note) bool { return n.Breaking })
	}
	if len(out) == 0 {
		t.Fatal("the migration registry has no releases")
	}
	return out
}
