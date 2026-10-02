package main

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scan"
)

// gofastrModule is the module path both go.mod inspection and the
// mechanical upgrade steps operate on.
const gofastrModule = "github.com/DonaldMurillo/gofastr"

// scanRun is the engine entry point; a var so tests can stub the scan
// while exercising the renderer.
var scanRun = scan.Run

// maxNoteHits caps the hits rendered per note (and the unexplained
// compile errors) so one pervasive pattern doesn't drown the report.
const maxNoteHits = 20

// goModGofastrVersion reads root/go.mod and returns the required
// gofastr version plus whether a replace directive overrides it (a
// local replace means the version in go.mod may not be what actually
// builds).
func goModGofastrVersion(root string) (version string, replaced bool, err error) {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", false, fmt.Errorf("read go.mod: %w", err)
	}
	inReplaceBlock := false
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "replace ("):
			inReplaceBlock = true
			continue
		case inReplaceBlock && line == ")":
			inReplaceBlock = false
			continue
		case inReplaceBlock:
			// Block-form replace: the module sits on its own line
			// ("github.com/… => ../local"). Never parse versions here;
			// the "=>" token would be misread as one.
			if fields := strings.Fields(line); len(fields) > 0 && fields[0] == gofastrModule {
				replaced = true
			}
			continue
		case strings.HasPrefix(line, "replace "+gofastrModule+" ") || strings.HasPrefix(line, "replace "+gofastrModule+"=>"):
			replaced = true
			continue
		}
		// Matches both the require-block form ("\tmodule vX.Y.Z") and
		// the single-line form ("require module vX.Y.Z").
		fields := strings.Fields(strings.TrimPrefix(line, "require "))
		if len(fields) >= 2 && fields[0] == gofastrModule {
			version = fields[1]
		}
	}
	if version == "" {
		return "", replaced, fmt.Errorf("go.mod does not require %s: is this a GoFastr app?", gofastrModule)
	}
	return version, replaced, nil
}

// upgradeReport scans root once for every note in range and renders
// the whole notes block: the not-type-checked NOTE, the releases with
// their guidance and hits, and the unexplained compile errors. When
// the engine cannot run at all the notes still print, with no hits:
// guidance never depends on the scan succeeding.
func upgradeReport(root string, inRange []upgrade.Release, sinks upgrade.MarkerSinks) string {
	var notes []*upgrade.Note
	for _, r := range inRange {
		notes = append(notes, r.Notes...)
	}
	if len(notes) == 0 {
		return formatUpgradeNotes(nil, inRange)
	}
	result, err := scanRun(root, notes, sinks)
	if err != nil {
		var b strings.Builder
		fmt.Fprintf(&b, "NOTE: could not scan this project (%v); no lines are\n", err)
		b.WriteString("      pointed at below, but every note still carries its guidance.\n\n")
		b.WriteString(formatUpgradeNotes(nil, inRange))
		return b.String()
	}
	var b strings.Builder
	if !result.TypeChecked {
		b.WriteString("NOTE: the app did not type-check against its current gofastr version, so\n")
		b.WriteString("      the Go matches below come from its compile errors, not the type\n")
		b.WriteString("      checker. Packages that failed: " + brokenList(result.Broken) + ".\n\n")
	}
	if len(result.Unscanned) > 0 {
		files := "files"
		if len(result.Unscanned) == 1 {
			files = "file"
		}
		fmt.Fprintf(&b, "NOTE: %d %s could not be scanned, so no line in them is pointed at\n", len(result.Unscanned), files)
		b.WriteString("      below. Read them against every note by hand:\n")
		for i, f := range result.Unscanned {
			if i == maxNoteHits {
				fmt.Fprintf(&b, "  … and %d more\n", len(result.Unscanned)-maxNoteHits)
				break
			}
			b.WriteString("  " + f + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(formatUpgradeNotes(result, inRange))
	if len(result.Unexplained) > 0 {
		b.WriteString("Compile errors no note explains:\n")
		for i, h := range result.Unexplained {
			if i == maxNoteHits {
				fmt.Fprintf(&b, "  … and %d more\n", len(result.Unexplained)-maxNoteHits)
				break
			}
			fmt.Fprintf(&b, "  %s  %s\n", hitPos(h), strings.ReplaceAll(h.Why, "\n", "\n      "))
		}
	}
	return b.String()
}

// brokenList renders the broken-package list, capped: at most five
// names, then "and N more".
func brokenList(broken []string) string {
	const max = 5
	if len(broken) == 0 {
		return "none reported"
	}
	shown, extra := broken, 0
	if len(broken) > max {
		shown, extra = broken[:max], len(broken)-max
	}
	s := strings.Join(shown, ", ")
	if extra > 0 {
		s += fmt.Sprintf(", and %d more", extra)
	}
	return s
}

// formatUpgradeNotes renders the migration notes for the releases a
// project crosses in three sections: the lines that must change, the
// lines to check, and the notes the scan found nothing for. A nil
// result (no scan ran) renders every note once, with no hits.
func formatUpgradeNotes(result *scan.Result, releases []upgrade.Release) string {
	if len(releases) == 0 {
		return "No migration notes between these versions: the mechanical steps below are all there is.\n"
	}
	if result == nil {
		return renderNotes(releases, func(*upgrade.Note) ([]scan.Hit, bool) { return nil, true })
	}
	sections := []struct {
		head string
		pick func(*upgrade.Note) ([]scan.Hit, bool)
	}{
		{"Edit these: each line spells something the target release no longer accepts.", func(n *upgrade.Note) ([]scan.Hit, bool) {
			hits := hitsFor(result, n, true)
			return hits, len(hits) > 0
		}},
		{"Check these: each line still builds; the behaviour behind it changed.", func(n *upgrade.Note) ([]scan.Hit, bool) {
			hits := hitsFor(result, n, false)
			return hits, len(hits) > 0
		}},
		{"Nothing found for these: read them in case the code is shaped in a way no scan sees.", func(n *upgrade.Note) ([]scan.Hit, bool) {
			return nil, len(result.Hits[n]) == 0
		}},
	}
	var b strings.Builder
	for _, s := range sections {
		if body := renderNotes(releases, s.pick); body != "" {
			b.WriteString(s.head + "\n\n" + body)
		}
	}
	return b.String()
}

// hitsFor returns a note's hits for one report section, sorted. A hit
// is an edit when its note says every hit is a dead spelling, or when
// it was read from a compile error: that line no longer builds.
func hitsFor(result *scan.Result, n *upgrade.Note, edit bool) []scan.Hit {
	var out []scan.Hit
	for _, h := range sortedHits(result, n) {
		if (!n.Review || h.Err != "") == edit {
			out = append(out, h)
		}
	}
	return out
}

// renderNotes renders the notes pick selects, grouped under their
// release, each followed by the hits pick returns. It returns "" when
// pick selects nothing.
func renderNotes(releases []upgrade.Release, pick func(*upgrade.Note) ([]scan.Hit, bool)) string {
	var b strings.Builder
	for _, r := range releases {
		headed := false
		for _, n := range r.Notes {
			hits, ok := pick(n)
			if !ok {
				continue
			}
			if !headed {
				headed = true
				if r.Title != "" {
					fmt.Fprintf(&b, "%s: %s\n", r.Version, r.Title)
				} else {
					fmt.Fprintf(&b, "%s\n", r.Version)
				}
			}
			marker := "•"
			if n.Breaking {
				marker = "! BREAKING:"
			}
			fmt.Fprintf(&b, "  %s %s\n", marker, n.Change)
			fmt.Fprintf(&b, "      %s\n", n.Guidance)
			for i, h := range hits {
				if i == maxNoteHits {
					fmt.Fprintf(&b, "        … and %d more\n", len(hits)-maxNoteHits)
					break
				}
				fmt.Fprintf(&b, "        %s  %s\n", hitPos(h), h.Why)
				if h.Err != "" {
					fmt.Fprintf(&b, "          compile error: %s\n", strings.ReplaceAll(h.Err, "\n", "\n          "))
				}
			}
		}
		if headed {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// sortedHits copies a note's hits out of the scan result in the
// report's stable order: file, line, column.
func sortedHits(result *scan.Result, note *upgrade.Note) []scan.Hit {
	if result == nil {
		return nil
	}
	hits := append([]scan.Hit(nil), result.Hits[note]...)
	slices.SortStableFunc(hits, func(a, b scan.Hit) int {
		if c := strings.Compare(a.File, b.File); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Line, b.Line); c != 0 {
			return c
		}
		return cmp.Compare(a.Col, b.Col)
	})
	return hits
}

// hitPos renders a hit's position; the column is dropped for matchers
// that have none (gomod, config), and an error the go command reported
// without a position reads "(no position)".
func hitPos(h scan.Hit) string {
	if h.File == "" {
		return "(no position)"
	}
	if h.Col > 0 {
		return fmt.Sprintf("%s:%d:%d", h.File, h.Line, h.Col)
	}
	return fmt.Sprintf("%s:%d", h.File, h.Line)
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
	from  string
	to    string
	apply bool
}

// parseUpgradeArgs resolves `gofastr upgrade` arguments: an optional
// positional root, --from and --to in both `--to=v` and `--to v`
// spellings, --apply, and --help. badFlag carries the first unknown flag.
func parseUpgradeArgs(args []string) (upgradeOpts, string) {
	opts := upgradeOpts{root: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if (arg == "--to" || arg == "--from") && i+1 < len(args) {
			arg += "=" + args[i+1]
			i++
		}
		switch {
		case strings.HasPrefix(arg, "--from="):
			opts.from = strings.TrimPrefix(arg, "--from=")
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
		fmt.Println("Usage: gofastr upgrade [root] [--from vX.Y.Z] [--to vX.Y.Z] [--apply]")
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
		fmt.Println("--from names the release the code was written for when go.mod no")
		fmt.Println("longer says so: run go get first and go.mod already names the")
		fmt.Println("target, which hides every note in between.")
		fmt.Println()
		fmt.Println("Install the TARGET version of this CLI first: an older binary's")
		fmt.Println("registry can't know about newer releases:")
		fmt.Println("    go install github.com/DonaldMurillo/gofastr/cmd/gofastr@vX.Y.Z")
		osExit(0)
	}
	if bad != "" {
		fmt.Fprintf(os.Stderr, "upgrade: unknown flag %s\n", bad)
		osExit(2)
	}

	reg, err := upgrade.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "upgrade: %v\n", err)
		osExit(1)
	}

	current, replaced, err := goModGofastrVersion(opts.root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "upgrade: %v\n", err)
		osExit(1)
	}
	currentFrom := "go.mod"
	if opts.from != "" {
		if err := upgrade.ValidateSemver(opts.from); err != nil {
			fmt.Fprintf(os.Stderr, "upgrade: --from: %v\n", err)
			osExit(1)
		}
		current, currentFrom = opts.from, "--from"
	}

	target := opts.to
	if target == "" {
		target, err = resolveLatestVersion()
		if err != nil {
			fmt.Fprintf(os.Stderr, "upgrade: could not resolve the latest release (offline?): %v\nPass the target explicitly: gofastr upgrade --to vX.Y.Z\n", err)
			osExit(1)
		}
	}
	if err := upgrade.ValidateSemver(target); err != nil {
		fmt.Fprintf(os.Stderr, "upgrade: %v\n", err)
		osExit(1)
	}

	fmt.Printf("Current: %s (%s)\n", current, currentFrom)
	if replaced && opts.from == "" {
		fmt.Println("         NOTE: go.mod has a replace directive for gofastr: the")
		fmt.Println("         version above may not be what actually builds.")
	}
	fmt.Printf("Target:  %s\n\n", target)

	if !upgrade.SemverLess(current, target) {
		if current == target {
			fmt.Println("Already on the target release: nothing to do.")
			if opts.from == "" {
				fmt.Println("If go.mod was bumped before this run, re-run with the release the")
				fmt.Println("code was written for to see the notes and the lines they affect:")
				fmt.Printf("    gofastr upgrade --from vX.Y.Z --to %s\n", target)
			}
			return
		}
		fmt.Println("Target is OLDER than the current version. Downgrades aren't guided;")
		fmt.Println("the notes below describe what you'd be undoing.")
		fmt.Println()
	}
	lo, hi := current, target
	if upgrade.SemverLess(target, current) {
		lo, hi = target, current
	}
	// Key the staleness warning on the UPPER bound of the inspected
	// range rather than only the target. A downgrade FROM a version newer
	// than the registry also spans releases this binary doesn't know.
	if upgrade.SemverLess(reg.Through, hi) {
		fmt.Printf("NOTE: this CLI's migration registry is complete through %s: the\n", reg.Through)
		fmt.Printf("range shown reaches %s, so it may cross notes this binary doesn't\n", hi)
		fmt.Println("know. Install the newest involved CLI first and re-run:")
		fmt.Printf("    go install %s/cmd/gofastr@%s\n\n", gofastrModule, hi)
	}
	fmt.Print(upgradeReport(opts.root, upgrade.ReleasesInRange(reg, lo, hi), reg.MarkerSinks))
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
