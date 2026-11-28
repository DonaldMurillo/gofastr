package appgen

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core/settings"
)

func stampFile(generator, name, content string) (string, error) {
	content = FormatGenerated(name, content)
	if _, _, ok := codegen.ReadMarker(name, content); ok {
		return content, nil
	}
	g, ok := LookupGenerator(generator)
	if !ok {
		return "", fmt.Errorf("stamp %s: %q is not a registered generator", name, generator)
	}
	m := codegen.Marker{Generator: generator, Kind: codegen.KindDerived}
	if g.Kind == ScaffoldKind {
		m.Kind = codegen.KindScaffold
		m.Version = MarkerVersion(project.CliVersion())
	}
	return codegen.Stamp(name, content, m)
}

// MarkerVersion is the version a scaffold marker records: a release tag, or "dev" for any other build.
func MarkerVersion(v string) string {
	if project.IsReleaseTag(v) {
		return v
	}
	return "dev"
}

func fileEdited(name, existing, replacement string) bool {
	if edited, ok := codegen.Edited(name, existing); ok {
		return edited
	}
	return existing != replacement
}

// StampGeneratedFiles stamps every rendered file but the settings file with its provenance marker.
func StampGeneratedFiles(generator string, files []GeneratedFile) ([]GeneratedFile, error) {
	out := make([]GeneratedFile, len(files))
	for i, f := range files {
		if f.Name == settings.FileName {
			out[i] = f
			continue
		}
		content, err := stampFile(generator, f.Name, f.Content)
		if err != nil {
			return nil, err
		}
		f.Content = content
		out[i] = f
	}
	return out, nil
}

func warnForceOverwrite(edited []string) {
	if len(edited) == 0 {
		return
	}
	cli.Warn("--force is overwriting %d file(s) edited since they were generated: the edits are being discarded:", len(edited))
	for _, name := range edited {
		cli.Warn("    %s", name)
	}
	cli.Warn("  Edits you never committed are gone. If this code is maintained by hand, restore the committed version with `git restore <file>` and do not regenerate it.")
}

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

func existingNames(files []GeneratedFile, root string) []string {
	var names []string
	for _, f := range files {
		if fileExistsUnder(root, f.Name) {
			names = append(names, f.Name)
		}
	}
	return names
}

func warnDerivedEdit(generator, display, existing, source string) {
	if edited, ok := codegen.Edited(display, existing); ok && edited {
		cli.Warn("%s was edited since `gofastr generate %s` wrote it; this run rebuilds it from %s and the edit is lost. Make the change in %s instead.", display, generator, source, source)
	}
}

func warnEditedOutputs(cfg codegen.CodegenConfig, files []codegen.GeneratedFile) {
	edited, err := codegen.EditedOutputs(cfg.Output)
	if err != nil {
		cli.Warn("could not check %s for edited files: %v", cfg.Output, err)
		return
	}
	pending := map[string]bool{}
	for _, f := range files {
		pending[f.Path] = true
	}
	clean := codegen.CleanEnabled(cfg)
	for _, rel := range edited {
		if clean || pending[rel] {
			cli.Warn("%s was edited since the last `gofastr generate`; this run rebuilds it from the codegen config and the edit is lost. Make the change in the generator's source instead.", filepath.Join(cfg.Output, filepath.FromSlash(rel)))
		}
	}
}
