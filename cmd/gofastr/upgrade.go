package main

import (
	_ "embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// gofastrModule is the module path both go.mod inspection and the
// mechanical upgrade steps operate on.
const gofastrModule = "github.com/DonaldMurillo/gofastr"

// upgradesYML is the migration registry: one entry per release that
// carries migration-relevant changes, maintained alongside CHANGELOG.md
// (a release PR with a BREAKING change adds its entry here in the same
// PR). `gofastr upgrade` reads it to guide a project from its current
// version to a target.
//
//go:embed upgrades.yml
var upgradesYML string

// upgradeNote is one migration-relevant change within a release.
type upgradeNote struct {
	Change   string // one-line summary
	Breaking bool
	Guidance string // one-line, actionable
	Detect   string // optional Go regex run per-line over the project's .go files
}

// upgradeRelease groups the notes for one tagged release.
type upgradeRelease struct {
	Version string // vX.Y.Z
	Title   string
	Notes   []upgradeNote
}

// loadUpgradeRegistry parses the embedded registry. Returned releases
// keep file order, which the registry test pins to ascending semver.
// through is the release the registry is complete up to — releases at
// or below it with no entry genuinely had no migration-relevant
// changes, while targets beyond it are newer than this CLI's knowledge.
func loadUpgradeRegistry() ([]upgradeRelease, error) {
	rel, _, err := loadUpgradeRegistryFull()
	return rel, err
}

func loadUpgradeRegistryFull() ([]upgradeRelease, string, error) {
	root, err := coreyaml.Parse(upgradesYML)
	if err != nil {
		return nil, "", fmt.Errorf("upgrades.yml: %w", err)
	}
	through := yamlString(root.Map["through"])
	if _, err := parseSemver(through); err != nil {
		return nil, "", fmt.Errorf("upgrades.yml: %w", err)
	}
	list := root.Map["releases"]
	if list == nil || list.Kind != coreyaml.List {
		return nil, "", fmt.Errorf("upgrades.yml: missing releases list")
	}
	var out []upgradeRelease
	for _, item := range list.List {
		if item.Kind != coreyaml.Map {
			return nil, "", fmt.Errorf("upgrades.yml: release entry is not a map (line %d)", item.Line)
		}
		rel := upgradeRelease{
			Version: yamlString(item.Map["version"]),
			Title:   yamlString(item.Map["title"]),
		}
		if notes := item.Map["notes"]; notes != nil && notes.Kind == coreyaml.List {
			for _, n := range notes.List {
				if n.Kind != coreyaml.Map {
					continue
				}
				rel.Notes = append(rel.Notes, upgradeNote{
					Change:   yamlString(n.Map["change"]),
					Breaking: yamlBool(n.Map["breaking"]),
					Guidance: yamlString(n.Map["guidance"]),
					Detect:   yamlString(n.Map["detect"]),
				})
			}
		}
		out = append(out, rel)
	}
	return out, through, nil
}

func yamlString(n *coreyaml.Node) string {
	if n == nil || n.Kind != coreyaml.Scalar {
		return ""
	}
	switch v := n.Value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	default:
		return ""
	}
}

func yamlBool(n *coreyaml.Node) bool {
	if n == nil || n.Kind != coreyaml.Scalar {
		return false
	}
	b, _ := n.Value.(bool)
	return b
}

// parseSemver parses a strict vMAJOR.MINOR.PATCH tag.
func parseSemver(v string) ([3]int, error) {
	var out [3]int
	if !strings.HasPrefix(v, "v") {
		return out, fmt.Errorf("version %q must look like vX.Y.Z", v)
	}
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("version %q must look like vX.Y.Z", v)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, fmt.Errorf("version %q must look like vX.Y.Z", v)
		}
		out[i] = n
	}
	return out, nil
}

// semverLess reports a < b. Malformed versions compare as lowest so an
// unknown current version includes every registry entry up to target.
func semverLess(a, b string) bool {
	av, aerr := parseSemver(a)
	bv, berr := parseSemver(b)
	if aerr != nil {
		return berr == nil
	}
	if berr != nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if av[i] != bv[i] {
			return av[i] < bv[i]
		}
	}
	return false
}

// releasesInRange returns the registry entries in (current, target],
// i.e. everything the project crosses when moving current → target.
// An empty/unknown current includes everything up to target.
func releasesInRange(reg []upgradeRelease, current, target string) []upgradeRelease {
	var out []upgradeRelease
	for _, r := range reg {
		if current != "" && !semverLess(current, r.Version) {
			continue
		}
		if semverLess(target, r.Version) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// goModGofastrVersion reads root/go.mod and returns the required
// gofastr version plus whether a replace directive overrides it (a
// local replace means the version in go.mod may not be what actually
// builds).
func goModGofastrVersion(root string) (version string, replaced bool, err error) {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", false, fmt.Errorf("read go.mod: %w", err)
	}
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "replace "+gofastrModule+" ") || strings.HasPrefix(line, "replace "+gofastrModule+"=>") {
			replaced = true
		}
		// Matches both the block form ("\tmodule vX.Y.Z") and the
		// single-line form ("require module vX.Y.Z").
		fields := strings.Fields(strings.TrimPrefix(line, "require "))
		if len(fields) >= 2 && fields[0] == gofastrModule {
			version = fields[1]
		}
	}
	if version == "" {
		return "", replaced, fmt.Errorf("go.mod does not require %s — is this a GoFastr app?", gofastrModule)
	}
	return version, replaced, nil
}

// detectHits runs one note's regex over the project's non-test .go
// files and returns "file:line" hits (root-relative), capped so one
// pervasive pattern doesn't drown the report.
func detectHits(root, pattern string) []string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	const maxHits = 20
	var hits []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(hits) >= maxHits {
			return filepath.SkipAll
		}
		if d.IsDir() {
			name := d.Name()
			switch name {
			case "vendor", ".git", "node_modules", "dist", "bin", "build", "tmp":
				return fs.SkipDir
			}
			if strings.HasPrefix(name, ".") && name != "." {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(body), "\n") {
			if re.MatchString(line) {
				hits = append(hits, fmt.Sprintf("%s:%d", rel, i+1))
				if len(hits) >= maxHits {
					break
				}
			}
		}
		return nil
	})
	return hits
}

// formatUpgradeNotes renders the migration notes for the releases a
// project crosses, running each note's detector against root so the
// report points at the exact lines that need attention.
func formatUpgradeNotes(root string, releases []upgradeRelease) string {
	if len(releases) == 0 {
		return "No migration notes between these versions — the mechanical steps below are all there is.\n"
	}
	var b strings.Builder
	for _, r := range releases {
		if r.Title != "" {
			fmt.Fprintf(&b, "%s — %s\n", r.Version, r.Title)
		} else {
			fmt.Fprintf(&b, "%s\n", r.Version)
		}
		for _, n := range r.Notes {
			marker := "•"
			if n.Breaking {
				marker = "! BREAKING:"
			}
			fmt.Fprintf(&b, "  %s %s\n", marker, n.Change)
			fmt.Fprintf(&b, "      %s\n", n.Guidance)
			if n.Detect != "" {
				if hits := detectHits(root, n.Detect); len(hits) > 0 {
					b.WriteString("      found in your project:\n")
					for _, h := range hits {
						fmt.Fprintf(&b, "        %s\n", h)
					}
				}
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// resolveLatestVersion asks the module proxy for the newest tagged
// release. Needs network; callers fall back to requiring --to.
func resolveLatestVersion() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-versions", gofastrModule).Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		return "", fmt.Errorf("no tagged versions reported for %s", gofastrModule)
	}
	return fields[len(fields)-1], nil
}

type upgradeOpts struct {
	root  string
	to    string
	apply bool
}

// parseUpgradeArgs resolves `gofastr upgrade` arguments: an optional
// positional root, --to in both `--to=v` and `--to v` spellings,
// --apply, and --help. badFlag carries the first unknown flag.
func parseUpgradeArgs(args []string) (upgradeOpts, string) {
	opts := upgradeOpts{root: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--to" && i+1 < len(args) {
			arg = "--to=" + args[i+1]
			i++
		}
		switch {
		case strings.HasPrefix(arg, "--to="):
			opts.to = strings.TrimPrefix(arg, "--to=")
		case arg == "--apply":
			opts.apply = true
		case arg == "--help" || arg == "-h":
			return opts, "--help"
		case !strings.HasPrefix(arg, "-"):
			opts.root = arg
		default:
			return opts, arg
		}
	}
	return opts, ""
}

// runUpgrade is the `gofastr upgrade` entry point.
func runUpgrade(args []string) {
	opts, bad := parseUpgradeArgs(args)
	if bad == "--help" {
		fmt.Println("Usage: gofastr upgrade [root] [--to vX.Y.Z] [--apply]")
		fmt.Println()
		fmt.Println("Guides an app from its current GoFastr release to a newer one: reads")
		fmt.Println("the project's go.mod, shows every migration note between the two")
		fmt.Println("versions (from the registry embedded in this CLI), and points at the")
		fmt.Println("exact lines in your code that known breaking changes affect.")
		fmt.Println()
		fmt.Println("Without --to the newest tagged release is resolved via the module")
		fmt.Println("proxy. With --apply the mechanical steps run for you: go get, go mod")
		fmt.Println("tidy, go build ./..., go test ./….")
		fmt.Println()
		fmt.Println("Install the TARGET version of this CLI first — an older binary's")
		fmt.Println("registry can't know about newer releases:")
		fmt.Println("    go install github.com/DonaldMurillo/gofastr/cmd/gofastr@vX.Y.Z")
		osExit(0)
	}
	if bad != "" {
		fmt.Fprintf(os.Stderr, "upgrade: unknown flag %s\n", bad)
		osExit(2)
	}

	reg, through, err := loadUpgradeRegistryFull()
	if err != nil {
		fmt.Fprintf(os.Stderr, "upgrade: %v\n", err)
		osExit(1)
	}

	current, replaced, err := goModGofastrVersion(opts.root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "upgrade: %v\n", err)
		osExit(1)
	}

	target := opts.to
	if target == "" {
		target, err = resolveLatestVersion()
		if err != nil {
			fmt.Fprintf(os.Stderr, "upgrade: could not resolve the latest release (offline?): %v\nPass the target explicitly: gofastr upgrade --to vX.Y.Z\n", err)
			osExit(1)
		}
	}
	if _, err := parseSemver(target); err != nil {
		fmt.Fprintf(os.Stderr, "upgrade: %v\n", err)
		osExit(1)
	}

	fmt.Printf("Current: %s (go.mod)\n", current)
	if replaced {
		fmt.Println("         NOTE: go.mod has a replace directive for gofastr — the")
		fmt.Println("         version above may not be what actually builds.")
	}
	fmt.Printf("Target:  %s\n\n", target)

	if !semverLess(current, target) {
		if current == target {
			fmt.Println("Already on the target release — nothing to do.")
			return
		}
		fmt.Println("Target is OLDER than the current version. Downgrades aren't guided;")
		fmt.Println("the notes below describe what you'd be undoing.")
		fmt.Println()
	}
	if semverLess(through, target) {
		fmt.Printf("NOTE: this CLI's migration registry is complete through %s — the\n", through)
		fmt.Println("target is newer, so it may carry notes this binary doesn't know.")
		fmt.Println("Install the target CLI first and re-run:")
		fmt.Printf("    go install %s/cmd/gofastr@%s\n\n", gofastrModule, target)
	}

	lo, hi := current, target
	if semverLess(target, current) {
		lo, hi = target, current
	}
	fmt.Print(formatUpgradeNotes(opts.root, releasesInRange(reg, lo, hi)))
	fmt.Println("Consult the release notes for the full story:")
	fmt.Printf("    https://github.com/DonaldMurillo/gofastr/releases\n\n")

	steps := [][]string{
		{"go", "get", gofastrModule + "@" + target},
		{"go", "mod", "tidy"},
		{"go", "build", "./..."},
		{"go", "test", "./..."},
	}
	if !opts.apply {
		fmt.Println("Steps (re-run with --apply to execute them):")
		for i, s := range steps {
			fmt.Printf("  %d. %s\n", i+1, strings.Join(s, " "))
		}
		fmt.Println("  5. go install " + gofastrModule + "/cmd/gofastr@" + target + "   (the CLI doesn't update with go.mod)")
		fmt.Println("  6. review the go.mod / go.sum diff before committing")
		return
	}
	for _, s := range steps {
		fmt.Printf("→ %s\n", strings.Join(s, " "))
		cmd := exec.Command(s[0], s[1:]...)
		cmd.Dir = opts.root
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "upgrade: %q failed: %v\nFix the errors above (see the migration notes), then re-run.\n", strings.Join(s, " "), err)
			osExit(1)
		}
	}
	fmt.Println()
	fmt.Println("Upgraded. Two manual steps remain:")
	fmt.Println("  • go install " + gofastrModule + "/cmd/gofastr@" + target + "   (the CLI doesn't update with go.mod)")
	fmt.Println("  • review the go.mod / go.sum diff before committing")
}
