package upgrade

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/semver"
)

// readChangelogRoot loads the repo's CHANGELOG.md: Unreleased and the
// current ten minor versions.
//
//gofastr:test-reads /CHANGELOG.md
func readChangelogRoot(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	return string(b)
}

// changelogArchives lists the files under changelog/ that hold the
// releases moved out of CHANGELOG.md, in name order.
//
//gofastr:test-reads /changelog/
func changelogArchives(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "changelog", "*.md"))
	if err != nil {
		t.Fatalf("list changelog/: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("changelog/ holds no archived releases")
	}
	return files
}

// readChangelog loads every release the project has shipped: CHANGELOG.md
// followed by each archive under changelog/.
func readChangelog(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(readChangelogRoot(t))
	for _, f := range changelogArchives(t) {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		b.WriteString("\n")
		b.Write(body)
	}
	return b.String()
}

var releaseHeading = regexp.MustCompile(`(?m)^## \[(\d+)\.(\d+)\.(\d+)\]`)

type releaseVersion struct{ major, minor int }

// releaseHeadings returns the `## [X.Y.Z]` headings of a changelog file,
// top to bottom, keyed by their full version.
func releaseHeadings(body string) ([]string, map[string]releaseVersion) {
	var order []string
	vers := map[string]releaseVersion{}
	for _, m := range releaseHeading.FindAllStringSubmatch(body, -1) {
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		v := m[1] + "." + m[2] + "." + m[3]
		order = append(order, v)
		vers[v] = releaseVersion{major, minor}
	}
	return order, vers
}

var archiveName = regexp.MustCompile(`^v(\d+)\.(\d+)-v(\d+)\.(\d+)\.md$`)

// CHANGELOG.md holds Unreleased and the current ten minor versions (v0.90
// to v0.99 while v0.9x ships), and each earlier ten lives unchanged in
// changelog/v0.A0-v0.A9.md (v0.3-v0.9.md for the single-digit minors), so
// no one file grows without bound. This holds that layout, so the move at
// each v0.N0.0 release cannot be skipped: a release left behind, filed in
// the wrong range, kept in two places, or archived without a link from
// CHANGELOG.md fails here.
func TestChangelogArchiveLayout(t *testing.T) {
	root := readChangelogRoot(t)
	order, rootVers := releaseHeadings(root)
	if len(order) == 0 {
		t.Fatal("CHANGELOG.md has no `## [X.Y.Z]` sections")
	}
	latest := rootVers[order[0]]
	floor := latest.minor - latest.minor%10

	where := map[string]string{}
	for _, v := range order {
		where[v] = "CHANGELOG.md"
		if rv := rootVers[v]; rv.major != latest.major || rv.minor < floor {
			t.Errorf("CHANGELOG.md still holds %s; releases below v%d.%d move to changelog/ unchanged once v%d.%d.0 ships",
				v, latest.major, floor, latest.major, floor)
		}
	}

	for _, f := range changelogArchives(t) {
		name := filepath.Base(f)
		m := archiveName.FindStringSubmatch(name)
		if m == nil || m[1] != m[3] {
			t.Errorf("changelog/%s does not name a range in one major version, like v0.80-v0.89.md", name)
			continue
		}
		major, _ := strconv.Atoi(m[1])
		lo, _ := strconv.Atoi(m[2])
		hi, _ := strconv.Atoi(m[4])
		if !strings.Contains(root, "(changelog/"+name+")") {
			t.Errorf("CHANGELOG.md does not link changelog/%s", name)
		}
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		vs, vers := releaseHeadings(string(body))
		if len(vs) == 0 {
			t.Errorf("changelog/%s holds no releases", name)
		}
		for _, v := range vs {
			rv := vers[v]
			if rv.major != major || rv.minor < lo || rv.minor > hi {
				t.Errorf("changelog/%s holds %s, outside its range", name, v)
			}
			if rv.major == latest.major && rv.minor >= floor {
				t.Errorf("changelog/%s holds %s, which belongs in CHANGELOG.md until v%d.%d.0 ships",
					name, v, latest.major, floor+10)
			}
			if prev, dup := where[v]; dup {
				t.Errorf("%s appears in both %s and changelog/%s", v, prev, name)
			}
			where[v] = "changelog/" + name
		}
	}
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
	_, rest, ok := strings.Cut(readChangelogRoot(t), "\n## [Unreleased]\n")
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
