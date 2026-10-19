package main

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

// stampFile formats a generated file and stamps its provenance marker:
// the generator, its kind from the registry, and for a scaffold file the
// release that wrote it. A file that already carries a marker keeps it: a
// canonical package's styles output is derived from its sheet even when
// the app generator is the one copying it.
func stampFile(generator, name, content string) (string, error) {
	content = formatGenerated(name, content)
	if _, _, ok := codegen.ReadMarker(name, content); ok {
		return content, nil
	}
	g, ok := lookupGenerator(generator)
	if !ok {
		return "", fmt.Errorf("stamp %s: %q is not a registered generator", name, generator)
	}
	m := codegen.Marker{Generator: generator, Kind: codegen.KindDerived}
	if g.kind == scaffoldKind {
		m.Kind = codegen.KindScaffold
		m.Version = markerVersion(cliVersion())
	}
	return codegen.Stamp(name, content, m)
}

// markerVersion is the version a scaffold marker records: a release tag,
// or "dev" for any other build. A build from a checkout carries a
// pseudo-version naming its commit, often +dirty; written into a marker,
// it would change every scaffolded file on each commit and name a build
// nobody can install again.
func markerVersion(v string) string {
	if isReleaseTag(v) {
		return v
	}
	return "dev"
}

// fileEdited reports whether an existing generated file holds changes a
// rewrite would discard. A marked file answers from its own sum, so an
// untouched file an older template wrote is not an edit. An unmarked one
// (written before markers, or by hand) is edited when it differs from
// what is about to replace it.
func fileEdited(name, existing, replacement string) bool {
	if edited, ok := codegen.Edited(name, existing); ok {
		return edited
	}
	return existing != replacement
}

// stampGeneratedFiles stamps every rendered file before anything reads
// them, so a dry run reports the sizes a real run writes and the edit
// checks compare like with like.
func stampGeneratedFiles(generator string, files []generatedFile) ([]generatedFile, error) {
	out := make([]generatedFile, len(files))
	for i, f := range files {
		content, err := stampFile(generator, f.name, f.content)
		if err != nil {
			return nil, err
		}
		f.content = content
		out[i] = f
	}
	return out, nil
}

// warnForceOverwrite lists the edited files a --force run is replacing.
// The user asked to overwrite, so it warns and does not stop.
func warnForceOverwrite(edited []string) {
	if len(edited) == 0 {
		return
	}
	warn("--force is overwriting %d file(s) edited since they were generated: the edits are being discarded:", len(edited))
	for _, name := range edited {
		warn("    %s", name)
	}
	warn("  Edits you never committed are gone. If this code is maintained by hand, restore the committed version with `git restore <file>` and do not regenerate it.")
}

// describeExisting says what made the files a scaffold generator found in
// its way: which generator and version marked them, and how many changed
// since. Unmarked files are counted as such. It returns "" when nothing
// is there to describe.
func describeExisting(root string, names []string) string {
	made := map[string]int{}
	edited, unmarked := 0, 0
	for _, name := range names {
		data, err := readFileUnder(root, name)
		if err != nil {
			continue
		}
		m, body, ok := codegen.ReadMarker(name, string(data))
		if !ok {
			unmarked++
			continue
		}
		label := "gofastr generate " + m.Generator
		if m.Version != "" {
			label += " " + m.Version
		}
		made[label]++
		if codegen.Sum(body) != m.Sum {
			edited++
		}
	}
	if len(made) == 0 && unmarked == 0 {
		return ""
	}
	var parts []string
	for _, label := range slices.Sorted(maps.Keys(made)) {
		parts = append(parts, fmt.Sprintf("%d written by `%s`", made[label], label))
	}
	if unmarked > 0 {
		parts = append(parts, fmt.Sprintf("%d with no gofastr marker", unmarked))
	}
	s := "Already there: " + strings.Join(parts, ", ")
	if edited > 0 {
		s += fmt.Sprintf("; %d of the marked files were edited since", edited)
	}
	return s + "."
}

// existingNames returns the names of files that already exist under root.
func existingNames(files []generatedFile, root string) []string {
	var names []string
	for _, f := range files {
		if fileExistsUnder(root, f.name) {
			names = append(names, f.name)
		}
	}
	return names
}

// warnDerivedEdit warns when a derived file about to be rebuilt was edited
// since it was generated. Derived output says DO NOT EDIT, so the rebuild
// goes ahead; the warning names where the change belongs instead.
func warnDerivedEdit(generator, display, existing, source string) {
	if edited, ok := codegen.Edited(display, existing); ok && edited {
		warn("%s was edited since `gofastr generate %s` wrote it; this run rebuilds it from %s and the edit is lost. Make the change in %s instead.", display, generator, source, source)
	}
}

// warnEditedOutputs warns about codegen-config output edited since the
// last run, read from the sums in the output root's manifest. Only files
// this run replaces count: a clean run replaces every manifest file, an
// unclean one only those it writes again.
func warnEditedOutputs(cfg codegen.CodegenConfig, files []codegen.GeneratedFile) {
	edited, err := codegen.EditedOutputs(cfg.Output)
	if err != nil {
		warn("could not check %s for edited files: %v", cfg.Output, err)
		return
	}
	pending := map[string]bool{}
	for _, f := range files {
		pending[f.Path] = true
	}
	clean := codegen.CleanEnabled(cfg)
	for _, rel := range edited {
		if clean || pending[rel] {
			warn("%s was edited since the last `gofastr generate`; this run rebuilds it from the codegen config and the edit is lost. Make the change in the generator's source instead.", filepath.Join(cfg.Output, filepath.FromSlash(rel)))
		}
	}
}

// provenanceMaxBytes bounds the files the provenance scan reads: a
// generated source file is far smaller, and a large asset is not one.
const provenanceMaxBytes = 4 << 20

// provenanceEntry is one marked file the scan found.
type provenanceEntry struct {
	Path   string
	Marker codegen.Marker
	Edited bool
}

// scanProvenance walks root for files carrying a provenance marker,
// skipping the directories the go tool skips (vendor, testdata,
// node_modules, and names starting with "." or "_").
func scanProvenance(root string) ([]provenanceEntry, error) {
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
// scaffold files are counted, not flagged: the user owns them.
func reportProvenance(root string, note, noteWarn func(string, ...any)) {
	entries, err := scanProvenance(root)
	if err != nil {
		noteWarn("could not scan for generated files: %v", err)
		return
	}
	if len(entries) == 0 {
		return
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
}
